// Package camera implements UVC camera passthrough for JetKVM.
//
// # Architecture Overview
//
// The camera package enables real-time video streaming from a browser camera to a USB
// host via the UVC (USB Video Class) gadget. The data flow is:
//
//	Browser Camera → WebCodecs/Canvas → WebSocket → Go Backend → V4L2 → UVC Gadget → USB Host
//
// # Components
//
// Manager: Coordinates camera passthrough state and UVC device lifecycle. Handles:
//   - UVC device initialization and event loop management
//   - Format negotiation between USB host and browser
//   - Frame routing from WebSocket to V4L2 device
//   - Frame drop tracking for observability
//
// FormatInfo: Describes video format (codec, resolution, frame rate) negotiated with USB host.
// Use NewFormatInfo() for validated creation or StopFormat() for stop notifications.
//
// VideoCodec: Type-safe video codec identifier (h264, mjpeg, stop).
// Provides IsValid(), String(), and ToByte() for wire protocol encoding.
//
// # Threading Model
//
// The Manager uses a carefully designed concurrency model optimized for real-time video:
//
//   - Hot path fields (uvcStreamingFast, uvcMjpegFast, enabled) use atomics for lock-free access
//   - Frame handlers (HandleCameraH264Frame, HandleCameraMjpegFrame) are lock-free for minimal latency
//   - UVC event loop runs in a dedicated goroutine, processing USB events
//   - Format notification uses a buffered channel to decouple event loop from WebSocket handling
//   - Mutex (streamerMu) only protects streamer creation/destruction, not frame dispatch
//
// # Wire Protocol
//
// Binary frame format over WebSocket:
//
//	[codec_byte: 1 byte][frame_data: N bytes]
//
// Codec bytes (must match cameraTransport.ts):
//   - 0x01: H.264 (Annex B format with SPS/PPS in keyframes)
//   - 0x02: MJPEG (complete JPEG frames)
//
// # Frame Drop Observability
//
// Frames may be silently dropped in hot paths for performance. Use GetFrameStats() to
// monitor drop rates:
//   - DroppedStateFrames: Drops due to state mismatch (not streaming, disabled, wrong codec)
//   - DroppedWriteFrames: Drops due to V4L2 write failures
//   - WriteErrors: Total V4L2 errors (throttled in logs to prevent spam)
//
// Call ResetFrameStats() when starting a new streaming session.
//
// # Error Recovery
//
// The event loop includes panic recovery that:
//   - Logs the panic for debugging
//   - Notifies the browser via format channel (so it stops encoding)
//   - Calls the configured OnPanic handler for alerting
//
// Streaming failures also notify the browser to prevent orphaned encoding.
//
// # Usage Example
//
//	cfg := camera.Config{
//	    Gadget:       gadgetController,
//	    UVCLogger:    &uvcLogger,
//	    CameraLogger: &camLogger,
//	    OnPanic:      func(v interface{}) { alert(v) },
//	}
//	mgr, err := camera.NewManager(cfg)
//	if err != nil {
//	    return err
//	}
//
//	// Subscribe to format changes for WebSocket notification
//	formatCh := mgr.SubscribeFormatChanges()
//	go func() {
//	    for format := range formatCh {
//	        sendToWebSocket(format)
//	    }
//	}()
//
//	// Initialize UVC device
//	if err := mgr.InitUVC(true); err != nil {
//	    return err
//	}
//	mgr.SetEnabled(true)
//
//	// Handle incoming frames from WebSocket
//	mgr.HandleCameraH264Frame(frameData)
package camera
