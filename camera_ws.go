package kvm

import (
	"context"
	"encoding/json"
	"fmt"
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
	ringBufferCount      = 4           // 4 buffers in ring (exceeds 3 V4L2 buffers for safety)
	ringBufferSize       = 2048 * 1024 // 2MB per buffer (handles typical 1080p MJPEG; oversized frames are drained and skipped)
	initialFrameLogCount = 5           // Number of initial frames to log for debugging
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
	mgr := cameraManagerPtr.Load()
	if mgr == nil {
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
	formatChan := mgr.SubscribeFormatChanges()
	defer mgr.UnsubscribeFormatChanges()

	// Send initial format if UVC is already streaming
	if format := mgr.GetCurrentFormat(); format != nil {
		if err := sendFormatMessage(ctx, ws, format); err != nil {
			cameraLog.Warn().Err(err).Msg("Failed to send initial format message")
		}
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
				if err := sendFormatMessage(ctx, ws, &format); err != nil {
					cameraLog.Warn().Err(err).Msg("Failed to send format change message")
				}
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
				cameraLog.Warn().Err(readErr).Msg("Error reading WebSocket frame")
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
			cameraLog.Debug().Int("size", n).Msg("Camera frame too small, skipping")
			continue
		}

		// HOTPATH: Parse codec byte and route directly
		codecByte := buf[0]
		frameData := buf[frameHeaderSize:n]

		// HOTPATH: Direct dispatch to handlers
		// WriteFrame copies data to V4L2 buffer before returning,
		// so it's safe to advance ring index after dispatch
		frameCount++
		if frameCount <= initialFrameLogCount {
			cameraLog.Debug().
				Int("frame", frameCount).
				Int("size", len(frameData)).
				Uint8("codec", codecByte).
				Msg("Received camera frame")
		}

		// Use centralized codec byte constants from camera package
		switch codecByte {
		case camera.CodecByteH264:
			mgr.HandleCameraH264Frame(frameData)
		case camera.CodecByteMJPEG:
			mgr.HandleCameraMjpegFrame(frameData)
		default:
			cameraLog.Warn().Uint8("codec", codecByte).Msg("Unknown camera codec, dropping frame")
		}

		// Advance ring buffer index for next frame
		ringIdx = (ringIdx + 1) % ringBufferCount
	}
}

// sendFormatMessage sends a format negotiation message to the WebSocket client.
// Includes encoder settings (bitrate/quality) from config so the browser can configure its encoder.
// Returns an error if the message could not be sent.
func sendFormatMessage(ctx context.Context, ws *websocket.Conn, format *camera.FormatInfo) error {
	ensureConfigLoaded()

	cameraLog.Info().
		Str("codec", format.Codec.String()).
		Int("width", format.Width).
		Int("height", format.Height).
		Int("frameRate", format.FrameRate).
		Int("h264Bitrate_mbps", config.CameraH264Bitrate).
		Int("mjpegQuality_pct", config.CameraMjpegQuality).
		Msg("sendFormatMessage: sending to browser")

	msg := map[string]interface{}{
		"type":         "format",
		"codec":        format.Codec.String(),
		"width":        format.Width,
		"height":       format.Height,
		"frameRate":    format.FrameRate,                           // UVC-negotiated rate from host
		"frameRateCap": config.CameraFrameRate,                     // User's configured cap (browser uses min of both)
		"h264Bitrate":  config.CameraH264Bitrate * 1_000_000,       // Convert Mbps to bps for browser
		"mjpegQuality": float64(config.CameraMjpegQuality) / 100.0, // Convert 0-100% to 0.0-1.0
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal format message: %w", err)
	}

	// Use a short timeout for control messages
	writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := ws.Write(writeCtx, websocket.MessageText, data); err != nil {
		return fmt.Errorf("failed to send format message: %w", err)
	}
	return nil
}
