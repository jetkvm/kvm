package kvm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/jetkvm/kvm/internal/camera"
)

// frameBufferPool reduces GC pressure by reusing frame buffers.
// Each buffer is 256KB which should handle most MJPEG frames.
var frameBufferPool = sync.Pool{
	New: func() interface{} {
		buf := make([]byte, 256*1024) // 256KB initial capacity
		return &buf
	},
}

// Frame header size: 1-byte codec flag (timestamp removed for efficiency)
const frameHeaderSize = 1

// Codec flags for binary frame protocol
const (
	codecH264  = 0x01
	codecMjpeg = 0x02
)

// handleCameraWs handles the zero-overhead WebSocket endpoint for camera frames.
// Protocol:
//   - Server -> Client: JSON messages for format negotiation
//   - Client -> Server: Binary frames with 1-byte header (codec)
//
// Binary frame format:
//
//	[0]    uint8 codec (0x01=H.264, 0x02=MJPEG)
//	[1+]   raw frame data
func handleCameraWs(c *gin.Context) {
	if cameraManager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Camera manager not initialized"})
		return
	}

	// Accept WebSocket connection with minimal options for lowest latency
	wsOpts := &websocket.AcceptOptions{
		InsecureSkipVerify: true,
		CompressionMode:    websocket.CompressionDisabled, // No compression for video frames
	}

	ws, err := websocket.Accept(c.Writer, c.Request, wsOpts)
	if err != nil {
		cameraLog.Warn().Err(err).Msg("Failed to accept camera WebSocket")
		return
	}
	defer ws.CloseNow()

	// Set generous read limit for video frames (16MB should handle any resolution)
	ws.SetReadLimit(16 * 1024 * 1024)

	ctx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()

	cameraLog.Debug().Msg("Camera WebSocket connected")

	// Subscribe to format changes
	formatChan := cameraManager.SubscribeFormatChanges()
	defer cameraManager.UnsubscribeFormatChanges()

	// Send initial format if UVC is already streaming
	if format := cameraManager.GetCurrentFormat(); format != nil {
		sendFormatMessage(ctx, ws, format)
	}

	// Start goroutine to forward format changes to WebSocket
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case format, ok := <-formatChan:
				if !ok {
					return
				}
				sendFormatMessage(ctx, ws, &format)
			}
		}
	}()

	// Main loop: read binary frames from client using pooled buffers
	for {
		msgType, reader, err := ws.Reader(ctx)
		if err != nil {
			if websocket.CloseStatus(err) == websocket.StatusNormalClosure {
				cameraLog.Debug().Msg("Camera WebSocket closed normally")
			} else {
				cameraLog.Warn().Err(err).Msg("Camera WebSocket read error")
			}
			return
		}

		// Only process binary messages (frames)
		if msgType != websocket.MessageBinary {
			// Drain the reader for non-binary messages
			_, _ = io.Copy(io.Discard, reader)
			continue
		}

		// Get a buffer from the pool
		bufPtr := frameBufferPool.Get().(*[]byte)
		buf := *bufPtr

		// Read frame data into pooled buffer
		n, err := io.ReadFull(reader, buf)
		if err == io.ErrUnexpectedEOF || err == io.EOF {
			// Frame smaller than buffer - this is normal
			// n contains the actual bytes read
		} else if err != nil {
			frameBufferPool.Put(bufPtr)
			continue
		}

		// If frame is larger than our buffer, we need to read the rest
		// This handles frames > 256KB (rare but possible)
		if n == len(buf) {
			// Check if there's more data
			extra, _ := io.ReadAll(reader)
			if len(extra) > 0 {
				// Frame was larger than buffer, allocate new buffer
				fullData := make([]byte, n+len(extra))
				copy(fullData, buf[:n])
				copy(fullData[n:], extra)
				frameBufferPool.Put(bufPtr)
				processFrame(fullData)
				continue
			}
		}

		// Process the frame from pooled buffer
		data := buf[:n]

		// Validate minimum frame size
		if len(data) < frameHeaderSize+1 {
			frameBufferPool.Put(bufPtr)
			continue
		}

		// Parse header: codec (1 byte)
		codec := data[0]
		frameData := data[frameHeaderSize:n]

		// Route frame to appropriate handler based on codec
		switch codec {
		case codecH264:
			cameraManager.HandleCameraH264Frame(frameData)
		case codecMjpeg:
			cameraManager.HandleCameraMjpegFrame(frameData)
		default:
			cameraLog.Warn().Uint8("codec", codec).Msg("Unknown codec in camera frame")
		}

		// Return buffer to pool
		frameBufferPool.Put(bufPtr)
	}
}

// processFrame handles a frame that was too large for the pooled buffer.
func processFrame(data []byte) {
	if len(data) < frameHeaderSize+1 {
		return
	}

	codec := data[0]
	frameData := data[frameHeaderSize:]

	switch codec {
	case codecH264:
		cameraManager.HandleCameraH264Frame(frameData)
	case codecMjpeg:
		cameraManager.HandleCameraMjpegFrame(frameData)
	default:
		cameraLog.Warn().Uint8("codec", codec).Msg("Unknown codec in camera frame")
	}
}

// sendFormatMessage sends a format negotiation message to the WebSocket client.
func sendFormatMessage(ctx context.Context, ws *websocket.Conn, format *camera.FormatInfo) {
	msg := map[string]interface{}{
		"type":   "format",
		"codec":  format.Codec,
		"width":  format.Width,
		"height": format.Height,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		cameraLog.Warn().Err(err).Msg("Failed to marshal format message")
		return
	}

	// Use a short timeout for control messages
	writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := ws.Write(writeCtx, websocket.MessageText, data); err != nil {
		cameraLog.Warn().Err(err).Msg("Failed to send format message")
	}
}

// handleCameraFrame is called from WebRTC track handler.
// Kept for backward compatibility with WebRTC camera track.
func handleCameraFrame(frame []byte) {
	if cameraManager != nil {
		cameraManager.HandleCameraH264Frame(frame)
	}
}
