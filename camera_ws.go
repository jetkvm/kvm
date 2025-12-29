package kvm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/jetkvm/kvm/internal/camera"
)

// Frame header size: 1-byte codec flag (timestamp removed for efficiency)
const frameHeaderSize = 1

// Ring buffer configuration for zero-allocation WebSocket reads
const (
	ringBufferCount = 4           // 4 buffers in ring (safe with 8 V4L2 buffers)
	ringBufferSize  = 2048 * 1024 // 2MB per buffer (handles 1080p MJPEG at any quality)
)

// Codec flags for binary frame protocol
const (
	codecH264  = 0x01
	codecMjpeg = 0x02
)

// handleCameraWs handles the low-latency WebSocket endpoint for camera frames.
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
	defer func() {
		if err := ws.CloseNow(); err != nil {
			cameraLog.Debug().Err(err).Msg("Error closing camera WebSocket")
		}
	}()

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

	// HOTPATH: Pre-allocate ring buffer to eliminate per-frame GC pressure
	// Ring of 4 buffers rotates; safe because WriteFrame copies to V4L2 buffer
	// before we advance, and V4L2 has 3 buffers for low-latency streaming
	var ringBuf [ringBufferCount][]byte
	for i := range ringBuf {
		ringBuf[i] = make([]byte, ringBufferSize)
	}
	ringIdx := 0

	// Log when WebSocket connects and is ready to receive frames
	cameraLog.Info().Msg("Camera WebSocket ready to receive frames")

	// Track frame count for debug logging
	var frameCount int

	// HOTPATH: Main loop - zero-allocation frame handling
	// Uses ws.Reader() with ring buffer to avoid per-frame allocations
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

		// HOTPATH: Skip non-binary messages (control frames)
		if msgType != websocket.MessageBinary {
			// Drain the reader to complete the message
			if _, err := io.Copy(io.Discard, reader); err != nil {
				cameraLog.Debug().Err(err).Msg("Error draining non-binary WebSocket message")
			}
			continue
		}

		// HOTPATH: Read entire message into current ring buffer slot
		buf := ringBuf[ringIdx]
		n := 0
		for {
			nr, readErr := reader.Read(buf[n:])
			n += nr
			if readErr == io.EOF {
				break
			}
			if readErr != nil {
				n = 0 // Mark as invalid
				break
			}
			if n >= len(buf) {
				// Frame too large, drain remainder and skip
				cameraLog.Warn().Int("bufferSize", len(buf)).Msg("Camera frame too large for buffer")
				if _, drainErr := io.Copy(io.Discard, reader); drainErr != nil {
					cameraLog.Debug().Err(drainErr).Msg("Error draining oversized frame")
				}
				n = 0
				break
			}
		}

		// HOTPATH: Validate minimum frame size (1 byte header + 1 byte data)
		if n < frameHeaderSize+1 {
			continue
		}

		// HOTPATH: Parse codec byte and route directly
		codec := buf[0]
		frameData := buf[frameHeaderSize:n]

		// HOTPATH: Direct dispatch to handlers
		// WriteFrame copies data to V4L2 buffer before returning,
		// so it's safe to advance ring index after dispatch
		frameCount++
		if frameCount <= 5 {
			cameraLog.Info().
				Int("frame", frameCount).
				Int("size", len(frameData)).
				Uint8("codec", codec).
				Msg("Received camera frame")
		}
		switch codec {
		case codecH264:
			cameraManager.HandleCameraH264Frame(frameData)
		case codecMjpeg:
			cameraManager.HandleCameraMjpegFrame(frameData)
		}

		// Advance ring buffer index for next frame
		ringIdx = (ringIdx + 1) % ringBufferCount
	}
}

// sendFormatMessage sends a format negotiation message to the WebSocket client.
// Includes encoder settings (bitrate/quality) from config so the browser can configure its encoder.
func sendFormatMessage(ctx context.Context, ws *websocket.Conn, format *camera.FormatInfo) {
	ensureConfigLoaded()

	// Debug: log actual format values being sent to browser
	cameraLog.Info().
		Str("codec", format.Codec).
		Int("width", format.Width).
		Int("height", format.Height).
		Int("frameRate", format.FrameRate).
		Int("h264Bitrate_mbps", config.CameraH264Bitrate).
		Int("mjpegQuality_pct", config.CameraMjpegQuality).
		Msg("sendFormatMessage: sending to browser")

	msg := map[string]interface{}{
		"type":         "format",
		"codec":        format.Codec,
		"width":        format.Width,
		"height":       format.Height,
		"frameRate":    format.FrameRate,                           // UVC-negotiated rate from host
		"frameRateCap": config.CameraFrameRate,                     // User's configured cap (browser uses min of both)
		"h264Bitrate":  config.CameraH264Bitrate * 1_000_000,       // Convert Mbps to bps for browser
		"mjpegQuality": float64(config.CameraMjpegQuality) / 100.0, // Convert 0-100% to 0.0-1.0
	}

	data, err := json.Marshal(msg)
	if err != nil {
		cameraLog.Error().Err(err).Msg("Failed to marshal format message")
		return
	}

	// Use a short timeout for control messages
	writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := ws.Write(writeCtx, websocket.MessageText, data); err != nil {
		cameraLog.Warn().Err(err).Msg("Failed to send format message to browser")
	}
}
