package camera

import (
	"time"

	"github.com/jetkvm/kvm/internal/usbgadget"
	"github.com/rs/zerolog"
)

const (
	uvcBufferCount = 3   // 3 buffers for low-latency streaming (double-buffer + 1 for jitter)
	uvcLogInterval = 240 // Log every N frames (~10 seconds at 24fps)
)

// zerologAdapter wraps zerolog to implement usbgadget.Logger interface.
type zerologAdapter struct {
	logger *zerolog.Logger
}

func (l *zerologAdapter) Info() usbgadget.LogEvent {
	return &zerologEventAdapter{event: l.logger.Info()}
}

func (l *zerologAdapter) Warn() usbgadget.LogEvent {
	return &zerologEventAdapter{event: l.logger.Warn()}
}

func (l *zerologAdapter) Error() usbgadget.LogEvent {
	return &zerologEventAdapter{event: l.logger.Error()}
}

func (l *zerologAdapter) Debug() usbgadget.LogEvent {
	return &zerologEventAdapter{event: l.logger.Debug()}
}

type zerologEventAdapter struct {
	event *zerolog.Event
}

func (e *zerologEventAdapter) Str(key, val string) usbgadget.LogEvent {
	e.event = e.event.Str(key, val)
	return e
}

func (e *zerologEventAdapter) Int(key string, val int) usbgadget.LogEvent {
	e.event = e.event.Int(key, val)
	return e
}

func (e *zerologEventAdapter) Uint32(key string, val uint32) usbgadget.LogEvent {
	e.event = e.event.Uint32(key, val)
	return e
}

func (e *zerologEventAdapter) Bool(key string, val bool) usbgadget.LogEvent {
	e.event = e.event.Bool(key, val)
	return e
}

func (e *zerologEventAdapter) Err(err error) usbgadget.LogEvent {
	e.event = e.event.Err(err)
	return e
}

func (e *zerologEventAdapter) Msg(msg string) {
	e.event.Msg(msg)
}

// InitUVC initializes UVC streaming if enabled.
func (m *Manager) InitUVC(uvcEnabled bool) {
	m.streamerMu.Lock()
	defer m.streamerMu.Unlock()

	if !uvcEnabled {
		return
	}

	devicePath, err := m.gadget.GetUVCVideoDevice()
	if err != nil {
		if m.uvcLog != nil {
			m.uvcLog.Warn().Err(err).Msg("UVC device not found")
		}
		return
	}

	if m.uvcLog != nil {
		m.uvcLog.Info().Str("device", devicePath).Msg("UVC initialized (H.264 mode)")
	}
	m.streamer = usbgadget.NewUVCStreamer(devicePath, &zerologAdapter{logger: m.uvcLog})

	m.stopChan = make(chan struct{})
	m.eventLoopRun = true
	go m.eventLoop()
}

// StopUVC stops UVC streaming and cleanup.
func (m *Manager) StopUVC() {
	m.streamerMu.Lock()
	defer m.streamerMu.Unlock()

	if m.stopChan != nil {
		m.eventLoopRun = false
		close(m.stopChan)
		m.stopChan = nil
	}

	if m.streamer != nil {
		m.streamer.StopStreaming()
		m.streamer.Close()
		m.streamer = nil
	}
}

// ReinitUVC reinitializes UVC if needed.
func (m *Manager) ReinitUVC(uvcEnabled bool) {
	m.streamerMu.Lock()
	defer m.streamerMu.Unlock()

	if !uvcEnabled {
		return
	}

	// Don't disrupt existing valid connection
	if m.streamer != nil && m.streamer.IsOpen() && m.streamer.IsValid() {
		return
	}

	devicePath, err := m.gadget.GetUVCVideoDevice()
	if err != nil {
		if m.uvcLog != nil {
			m.uvcLog.Warn().Err(err).Msg("UVC device not found during reinit")
		}
		return
	}

	if m.streamer != nil {
		m.streamer.StopStreaming()
		m.streamer.Close()
	}

	if m.uvcLog != nil {
		m.uvcLog.Info().Str("device", devicePath).Msg("UVC reinitialized (H.264 mode)")
	}
	m.streamer = usbgadget.NewUVCStreamer(devicePath, &zerologAdapter{logger: m.uvcLog})

	if !m.eventLoopRun && m.stopChan == nil {
		m.stopChan = make(chan struct{})
		m.eventLoopRun = true
		go m.eventLoop()
	}
}

func (m *Manager) eventLoop() {
	const (
		pollInterval    = 20 * time.Millisecond
		retryInterval   = time.Second
		recoveryDelay   = 500 * time.Millisecond
		errorRetryDelay = 100 * time.Millisecond
	)

	for m.eventLoopRun {
		// HOTPATH: Lock-free streamer access - pointer only changes during init/shutdown
		streamer := m.streamer

		if streamer == nil {
			time.Sleep(retryInterval)
			continue
		}

		if streamer.IsOpen() && !streamer.IsValid() {
			if m.uvcLog != nil {
				m.uvcLog.Warn().Msg("UVC device invalid, recovering")
			}
			streamer.Close()
			time.Sleep(recoveryDelay)
			continue
		}

		if !streamer.IsOpen() {
			if err := streamer.Open(); err != nil {
				time.Sleep(retryInterval)
				continue
			}
			if err := streamer.SubscribeEvents(); err != nil {
				if m.uvcLog != nil {
					m.uvcLog.Warn().Err(err).Msg("Failed to subscribe to UVC events")
				}
			}
			if m.uvcLog != nil {
				m.uvcLog.Info().Msg("UVC device ready (H.264)")
			}
		}

		eventType, eventData, err := streamer.PollEventsWithData()
		if err != nil {
			time.Sleep(errorRetryDelay)
			continue
		}

		if eventType != 0 {
			m.handleEvent(streamer, eventType, eventData)
		}

		time.Sleep(pollInterval)
	}
}

func (m *Manager) handleEvent(streamer *usbgadget.UVCStreamer, eventType uint32, eventData []byte) {
	switch eventType {
	case usbgadget.UVC_EVENT_CONNECT:
		if m.uvcLog != nil {
			m.uvcLog.Debug().Msg("UVC connected")
		}

	case usbgadget.UVC_EVENT_DISCONNECT:
		if m.uvcLog != nil {
			m.uvcLog.Debug().Msg("UVC disconnected")
		}
		m.stopStreaming()

	case usbgadget.UVC_EVENT_STREAMON:
		m.startStreaming()

	case usbgadget.UVC_EVENT_STREAMOFF:
		m.stopStreaming()

	case usbgadget.UVC_EVENT_SETUP:
		if err := streamer.HandleSetupEvent(eventData); err != nil {
			if m.uvcLog != nil {
				m.uvcLog.Warn().Err(err).Msg("UVC SETUP failed")
			}
		}

	case usbgadget.UVC_EVENT_DATA:
		wasCommit, err := streamer.HandleDataEvent(eventData)
		if err != nil {
			if m.uvcLog != nil {
				m.uvcLog.Warn().Err(err).Msg("UVC DATA failed")
			}
		}
		if wasCommit {
			m.prepareStreaming()
		}
	}
}

func (m *Manager) prepareStreaming() {
	m.streamerMu.Lock()
	defer m.streamerMu.Unlock()

	if m.streamer == nil || m.streamer.IsStreaming() {
		return
	}

	width, height := m.streamer.GetCommittedResolution()
	isMjpeg := !m.streamer.IsH264Format()

	if err := m.streamer.SetFormatWithCodec(width, height, isMjpeg); err != nil {
		if m.uvcLog != nil {
			m.uvcLog.Debug().Err(err).Msg("SetFormatWithCodec failed (may not be supported)")
		}
	} else if m.uvcLog != nil {
		codec := "H.264"
		if isMjpeg {
			codec = "MJPEG"
		}
		m.uvcLog.Info().
			Uint32("width", width).
			Uint32("height", height).
			Str("codec", codec).
			Msg("V4L2 format set for UVC output")
	}
}

func (m *Manager) startStreaming() {
	m.streamerMu.Lock()
	defer m.streamerMu.Unlock()

	if m.streamer == nil {
		return
	}

	if !m.streamer.IsStreaming() {
		if err := m.streamer.RequestBuffers(uvcBufferCount); err != nil {
			if m.uvcLog != nil {
				m.uvcLog.Warn().Err(err).Msg("Failed to request UVC buffers")
			}
			return
		}
		if err := m.streamer.StartStreaming(); err != nil {
			if m.uvcLog != nil {
				m.uvcLog.Warn().Err(err).Msg("Failed to start UVC streaming")
			}
			return
		}

		// Request SPS/PPS injection on first frame after stream start
		// This ensures UVC clients receive the parameters they need to decode
		m.uvcNeedParamInject = true
		m.uvcParamInjectCount = 0
	}

	// Check the format selected by the host
	formatIndex := m.streamer.GetCommittedFormatIndex()
	isH264 := m.streamer.IsH264Format()
	m.uvcMjpegSelected = !isH264
	isHDMI := m.source.IsHDMI()

	// Log committed resolution for debugging
	committedW, committedH := m.streamer.GetCommittedResolution()
	committedFPS := m.streamer.GetCommittedFrameRate()
	if m.uvcLog != nil {
		m.uvcLog.Info().
			Uint32("width", committedW).
			Uint32("height", committedH).
			Int("fps", committedFPS).
			Uint8("format_index", formatIndex).
			Bool("isH264", isH264).
			Msg("UVC host committed format")
	}

	// Enable/disable MJPEG encoder based on format selection
	// Note: MJPEG encoder only works for HDMI source (hardware encodes capture buffer)
	// For camera source, we send H.264 frames regardless of format selection
	if m.native != nil {
		if m.uvcMjpegSelected && isHDMI {
			// HDMI source with MJPEG format: enable hardware MJPEG encoder
			m.native.MjpegSetEnabled(true)
			if m.uvcLog != nil {
				m.uvcLog.Info().
					Uint8("format_index", formatIndex).
					Str("format", "MJPEG").
					Str("source", m.source.Get().String()).
					Msg("UVC MJPEG streaming started (hardware encoder enabled)")
			}
		} else if m.uvcMjpegSelected && !isHDMI {
			// Camera source with MJPEG format requested by host
			// Unfortunately, hardware transcoding is not available on RV1106 (no VDEC)
			// Fall back to sending H.264 frames - this may not work on all hosts
			// but is the best we can do without hardware H.264 decoder
			m.native.MjpegSetEnabled(false)
			if m.uvcLog != nil {
				m.uvcLog.Warn().
					Uint8("format_index", formatIndex).
					Str("format", "MJPEG").
					Str("source", m.source.Get().String()).
					Msg("UVC: Camera source with MJPEG format - H.264 transcoding not available on RV1106 (no hardware VDEC)")
			}
		} else {
			m.native.MjpegSetEnabled(false)
			if m.uvcLog != nil {
				m.uvcLog.Info().
					Uint8("format_index", formatIndex).
					Str("format", "H.264").
					Str("source", m.source.Get().String()).
					Msg("UVC H.264 streaming started")
			}
		}
	} else if m.uvcLog != nil {
		if isH264 {
			m.uvcLog.Info().
				Uint8("format_index", formatIndex).
				Str("format", "H.264").
				Str("source", m.source.Get().String()).
				Msg("UVC H.264 streaming started")
		} else {
			m.uvcLog.Warn().
				Uint8("format_index", formatIndex).
				Str("format", "MJPEG").
				Msg("UVC: Host selected MJPEG but native controller not available")
		}
	}

	// Update atomic fast-path flags (for MJPEG hotpath)
	m.uvcStreamingFast.Store(true)
	m.uvcMjpegFast.Store(m.uvcMjpegSelected)

	// Notify WebSocket clients about format change
	width, height := m.streamer.GetCommittedResolution()
	frameRate := m.streamer.GetCommittedFrameRate()
	codec := "h264"
	if m.uvcMjpegSelected {
		codec = "mjpeg"
	}
	m.notifyFormatChange(FormatInfo{
		Codec:     codec,
		Width:     int(width),
		Height:    int(height),
		FrameRate: frameRate,
	})
}

func (m *Manager) stopStreaming() {
	// Clear atomic fast-path flags first (for MJPEG hotpath)
	m.uvcStreamingFast.Store(false)
	m.uvcMjpegFast.Store(false)

	m.streamerMu.Lock()
	defer m.streamerMu.Unlock()

	if m.streamer == nil {
		return
	}

	// Disable MJPEG encoder if it was enabled
	if m.uvcMjpegSelected && m.native != nil {
		m.native.MjpegSetEnabled(false)
	}

	wasMjpeg := m.uvcMjpegSelected
	m.uvcMjpegSelected = false

	// Notify WebSocket clients that streaming stopped (so browser can pause encoding)
	m.notifyStreamingStopped()

	if err := m.streamer.StopStreaming(); err != nil {
		if m.uvcLog != nil {
			m.uvcLog.Debug().Err(err).Msg("StopStreaming error")
		}
	}

	// Clear H.264 parameter cache on stream stop
	// Fresh parameters will be cached when streaming resumes
	m.h264Cache.Clear()
	m.uvcNeedParamInject = false
	m.uvcParamInjectCount = 0

	if m.uvcLog != nil {
		if wasMjpeg {
			m.uvcLog.Info().Msg("UVC MJPEG streaming stopped")
		} else {
			m.uvcLog.Info().Msg("UVC H.264 streaming stopped")
		}
	}
}

// HandleH264Frame handles an H.264 frame from the native encoder (HDMI loopback).
// This is called for each H.264 NAL unit from the RV1106 hardware encoder.
// HOTPATH: Optimized with atomic fast-path check to avoid mutex on every frame.
func (m *Manager) HandleH264Frame(frame []byte) {
	// HOTPATH: Atomic fast-path check - skip frames early if not streaming
	if !m.uvcStreamingFast.Load() {
		return
	}

	// Only process HDMI frames if UVC source is HDMI
	if !m.source.IsHDMI() {
		return
	}

	m.writeFrameToUVC(frame)
}

// HandleCameraH264Frame handles an H.264 frame from the browser camera.
// This is called when the browser sends H.264 encoded video over WebSocket.
// Browser sends H.264 when UVC host negotiates H.264 format.
// HOTPATH: Fully optimized - bypasses NAL scanning since browser sends complete H.264.
func (m *Manager) HandleCameraH264Frame(frame []byte) {
	// HOTPATH: Cache atomic loads once at function entry to avoid redundant loads
	enabled := m.enabled.Load()
	isCamera := m.source.IsCamera()
	isMjpeg := m.uvcMjpegFast.Load()

	// Debug logging for first few frames to trace issues
	frameNum := m.cameraFrameCount.Add(1)
	if frameNum <= 5 && m.camLog != nil {
		m.camLog.Info().
			Int32("frame", frameNum).
			Int("size", len(frame)).
			Bool("enabled", enabled).
			Bool("isCamera", isCamera).
			Bool("isMjpeg", isMjpeg).
			Msg("HandleCameraH264Frame called")
	}

	// Only process camera frames if camera passthrough is enabled and source is camera
	// Use cached values from function entry
	if !enabled || !isCamera {
		return
	}

	// Skip H.264 frames when MJPEG format is selected
	// (browser should send MJPEG in that case, not H.264)
	// Use cached value from function entry
	if isMjpeg {
		return
	}

	// HOTPATH: Direct write - bypass NAL scanning for camera passthrough
	// Browser sends properly formatted H.264 with SPS/PPS, no processing needed
	streamer := m.streamer
	if streamer == nil {
		return
	}

	// Check if UVC is actually streaming before writing
	if !streamer.IsStreaming() {
		// Log first few dropped frames for debugging
		if frameNum <= 3 && m.camLog != nil {
			m.camLog.Warn().Int32("frame", frameNum).Int("size", len(frame)).Msg("Camera frame dropped - UVC not streaming yet")
		}
		return
	}

	// Write frame directly to UVC (no NAL analysis overhead)
	m.uvcFrameCount++
	if err := streamer.WriteFrame(frame); err != nil {
		m.uvcFrameErrors++
		if m.uvcFrameCount <= 5 || m.uvcFrameCount%30 == 1 {
			if m.camLog != nil {
				m.camLog.Warn().Err(err).Int("frame", m.uvcFrameCount).Int("size", len(frame)).Msg("Camera H264 WriteFrame failed")
			}
		}
	} else if m.uvcFrameCount <= 5 || m.uvcFrameCount%60 == 1 {
		if m.camLog != nil {
			m.camLog.Info().Int("frame", m.uvcFrameCount).Int("size", len(frame)).Msg("Camera H264 frame written to UVC successfully")
		}
	}
}

// Periodic SPS/PPS injection interval (every N frames, ~10 seconds at 24fps)
const spsInjectInterval = 240

// writeFrameToUVC writes an H.264 frame to the UVC gadget (HDMI loopback path).
// Handles SPS/PPS caching and injection for proper H.264 stream initialization.
// HOTPATH: Optimized with quick NAL check - avoids full frame scan for P-frames.
// NOTE: Caller (HandleH264Frame) guarantees source is HDMI - no need to recheck.
func (m *Manager) writeFrameToUVC(frame []byte) {
	// Skip H.264 frames when MJPEG format is selected
	// (HDMI MJPEG frames are handled by HandleMjpegFrame)
	// NOTE: IsHDMI check removed - caller HandleH264Frame already verified this
	if m.uvcMjpegFast.Load() {
		return
	}

	// HOTPATH: Lock-free streamer access
	streamer := m.streamer
	if streamer == nil {
		return
	}

	// HOTPATH: Quick NAL type check - O(1) vs O(n) full scan
	// Most frames are P-frames that don't need SPS/PPS processing
	firstNALType := GetFirstNALType(frame)
	isIDR := firstNALType == NALTypeIDR
	isSPS := firstNALType == NALTypeSPS

	// Fast path: P-frames (non-IDR, non-SPS) - just send directly
	if !isIDR && !isSPS {
		m.uvcFrameCount++
		if err := streamer.WriteFrame(frame); err != nil {
			m.uvcFrameErrors++
		}
		return
	}

	// Slow path: IDR or SPS frame - may need parameter handling
	// Only do full scan when we actually need to process parameters
	needParamInject := m.uvcNeedParamInject
	frameToSend := frame

	if isSPS {
		// Frame starts with SPS - cache it (full scan needed for complete analysis)
		frameInfo := m.h264Cache.AnalyzeAndUpdate(frame)
		if needParamInject && frameInfo.HasIDR {
			m.uvcNeedParamInject = false
		}
	} else if isIDR {
		// IDR frame - check if we need to inject SPS/PPS
		if needParamInject || m.uvcParamInjectCount >= spsInjectInterval {
			// Need to inject parameters - do quick check for existing SPS
			quickInfo := QuickFrameInfo(frame)
			if !quickInfo.HasSPS && m.h264Cache.HasParameters() {
				frameToSend = m.h264Cache.PrependParametersWithInfo(frame, quickInfo)
			}
			if needParamInject {
				m.uvcNeedParamInject = false
			}
			m.uvcParamInjectCount = 0
		}
	}

	// Periodic counter for SPS/PPS injection
	m.uvcParamInjectCount++

	// Write the frame to UVC
	m.uvcFrameCount++
	if err := streamer.WriteFrame(frameToSend); err != nil {
		m.uvcFrameErrors++
	}

	// Log stats periodically (debug level to reduce overhead)
	if m.uvcFrameCount-m.uvcLastLogFrame >= uvcLogInterval {
		if m.uvcLog != nil {
			m.uvcLog.Debug().
				Int("frames", m.uvcFrameCount).
				Int("errors", m.uvcFrameErrors).
				Msg("UVC H.264 stats")
		}
		m.uvcLastLogFrame = m.uvcFrameCount
	}
}

// IsStreaming returns true if UVC is currently streaming.
func (m *Manager) IsStreaming() bool {
	m.streamerMu.Lock()
	defer m.streamerMu.Unlock()
	return m.streamer != nil && m.streamer.IsStreaming()
}

// RestoreMjpegState re-enables MJPEG encoder if UVC is streaming with MJPEG format.
// This should be called after native process restarts to restore encoder state.
func (m *Manager) RestoreMjpegState() {
	m.streamerMu.Lock()
	isStreaming := m.streamer != nil && m.streamer.IsStreaming()
	isMjpeg := m.uvcMjpegSelected
	isHDMI := m.source.IsHDMI()
	m.streamerMu.Unlock()

	if isStreaming && isMjpeg && isHDMI && m.native != nil {
		if m.uvcLog != nil {
			m.uvcLog.Info().Msg("Restoring MJPEG encoder state after native restart")
		}
		m.native.MjpegSetEnabled(true)
	}
}

// HandleMjpegFrame handles an MJPEG frame from the native encoder.
// This is called when the host selected MJPEG format and MJPEG encoder is enabled.
// Only works for HDMI source (hardware encodes from capture buffer).
// HOTPATH: Optimized with atomic fast-path checks to avoid mutex on every frame.
func (m *Manager) HandleMjpegFrame(frame []byte) {
	// HOTPATH: Atomic fast-path check - skip frames early if not streaming MJPEG
	if !m.uvcStreamingFast.Load() || !m.uvcMjpegFast.Load() {
		return
	}

	// Only process HDMI frames if UVC source is HDMI
	// (camera passthrough cannot use MJPEG - no hardware VDEC on RV1106)
	if !m.source.IsHDMI() {
		return
	}

	// HOTPATH: Lock-free streamer access - pointer only changes during init/shutdown
	streamer := m.streamer
	if streamer == nil {
		return
	}

	// Write MJPEG frame directly to UVC
	m.uvcFrameCount++

	// Log first few frames and then periodically
	if m.uvcFrameCount <= 5 || m.uvcFrameCount%60 == 1 {
		if m.uvcLog != nil {
			m.uvcLog.Info().
				Int("frame", m.uvcFrameCount).
				Int("size", len(frame)).
				Msg("HDMI MJPEG frame to UVC")
		}
	}

	if err := streamer.WriteFrame(frame); err != nil {
		m.uvcFrameErrors++
		if m.uvcFrameCount <= 5 || m.uvcFrameErrors%30 == 1 {
			if m.uvcLog != nil {
				m.uvcLog.Warn().Err(err).Int("frame", m.uvcFrameCount).Msg("HDMI MJPEG WriteFrame failed")
			}
		}
	}
}

// HandleCameraMjpegFrame handles an MJPEG frame from the browser camera.
// This is called when the browser sends MJPEG encoded video over WebSocket.
// Browser sends MJPEG when UVC host negotiates MJPEG format.
// HOTPATH: Fully lock-free using atomic flags - no mutex in frame path.
func (m *Manager) HandleCameraMjpegFrame(frame []byte) {
	// HOTPATH: Cache atomic loads once at function entry to avoid redundant loads
	streaming := m.uvcStreamingFast.Load()
	mjpeg := m.uvcMjpegFast.Load()
	enabled := m.enabled.Load()
	isCamera := m.source.IsCamera()

	// Debug: log first few frames to diagnose issues
	count := m.cameraFrameCount.Add(1)
	if count <= 5 && m.camLog != nil {
		// Check JPEG validity (SOI=0xFFD8, EOI=0xFFD9)
		validJpeg := len(frame) >= 4 &&
			frame[0] == 0xFF && frame[1] == 0xD8 &&
			frame[len(frame)-2] == 0xFF && frame[len(frame)-1] == 0xD9

		m.camLog.Info().
			Int32("frame", count).
			Bool("streaming", streaming).
			Bool("mjpeg", mjpeg).
			Bool("enabled", enabled).
			Bool("isCamera", isCamera).
			Int("size", len(frame)).
			Bool("validJpeg", validJpeg).
			Hex("first4", frame[:min(4, len(frame))]).
			Hex("last4", frame[max(0, len(frame)-4):]).
			Msg("HandleCameraMjpegFrame: checking conditions")
	}

	// HOTPATH: Use cached values for all checks
	if !streaming || !mjpeg {
		return
	}

	// Only process camera frames if camera passthrough is enabled and source is camera
	if !enabled || !isCamera {
		return
	}

	// HOTPATH: Direct streamer access - streamer pointer only changes during
	// init/shutdown which doesn't happen during active streaming
	// The atomic flags above guarantee streaming is active
	streamer := m.streamer
	if streamer == nil {
		return
	}

	// Write MJPEG frame directly to UVC
	if err := streamer.WriteFrame(frame); err != nil {
		// Log first 5 errors and then occasional (every 30 frames)
		if count <= 5 || count%30 == 1 {
			if m.camLog != nil {
				m.camLog.Warn().Err(err).Int32("frame", count).Int("size", len(frame)).Msg("Camera MJPEG WriteFrame failed")
			}
		}
		return
	}

	// Log first 5 successes and then occasional (every 60 frames = ~2.5s at 24fps)
	if count <= 5 || count%60 == 1 {
		if m.camLog != nil {
			m.camLog.Info().Int32("frame", count).Int("size", len(frame)).Msg("Camera MJPEG frame written to UVC successfully")
		}
	}
}
