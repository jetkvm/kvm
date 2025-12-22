package camera

import (
	"time"

	"github.com/jetkvm/kvm/internal/usbgadget"
	"github.com/rs/zerolog"
)

const (
	uvcBufferCount = 4   // 4 buffers for 60fps pipelining (allows USB transfer overlap with frame writes)
	uvcLogInterval = 600 // Log every N frames (~10 seconds at 60fps)
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
		m.streamerMu.Lock()
		streamer := m.streamer
		m.streamerMu.Unlock()

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
	if err := m.streamer.SetFormat(width, height); err != nil {
		if m.uvcLog != nil {
			m.uvcLog.Debug().Err(err).Msg("SetFormat failed (may not be supported)")
		}
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

	// Notify WebSocket clients about format change
	width, height := m.streamer.GetCommittedResolution()
	codec := "h264"
	if m.uvcMjpegSelected {
		codec = "mjpeg"
	}
	m.notifyFormatChange(FormatInfo{
		Codec:  codec,
		Width:  int(width),
		Height: int(height),
	})
}

func (m *Manager) stopStreaming() {
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
func (m *Manager) HandleH264Frame(frame []byte) {
	// Only process HDMI frames if UVC source is HDMI
	if !m.source.IsHDMI() {
		return
	}

	m.writeFrameToUVC(frame)
}

// HandleCameraH264Frame handles an H.264 frame from the browser camera.
// This is called when the browser sends H.264 encoded video over DataChannel.
// Note: Camera passthrough sends H.264 regardless of format negotiation.
// If host selected MJPEG, this will likely not work (Linux uvcvideo prefers MJPEG).
func (m *Manager) HandleCameraH264Frame(frame []byte) {
	// Only process camera frames if camera passthrough is enabled and source is camera
	if !m.enabled.Load() || !m.source.IsCamera() {
		return
	}

	m.writeFrameToUVC(frame)
}

// Periodic SPS/PPS injection interval (every N frames, ~10 seconds at 60fps)
const spsInjectInterval = 600

// writeFrameToUVC writes an H.264 frame to the UVC gadget.
// Handles SPS/PPS caching and injection for proper H.264 stream initialization.
func (m *Manager) writeFrameToUVC(frame []byte) {
	m.streamerMu.Lock()
	streamer := m.streamer
	isStreaming := streamer != nil && streamer.IsStreaming()
	needParamInject := m.uvcNeedParamInject
	isMjpeg := m.uvcMjpegSelected
	m.streamerMu.Unlock()

	if !isStreaming {
		return
	}

	// Skip H.264 frames when MJPEG format is selected AND source is HDMI
	// (HDMI MJPEG frames are handled by HandleMjpegFrame)
	// For camera source, we send H.264 frames regardless of format selection
	// since we can't transcode camera to MJPEG
	if isMjpeg && m.source.IsHDMI() {
		return
	}

	// Always cache SPS/PPS from incoming frames
	m.h264Cache.UpdateFromFrame(frame)

	// Prepare the frame to send
	frameToSend := frame

	// Handle SPS/PPS injection scenarios:
	// 1. Stream just started - inject on first IDR frame
	// 2. Periodic injection - every N frames to help clients that connect mid-stream
	// 3. IDR frames without SPS - prepend cached parameters
	if needParamInject && IsIDRFrame(frame) {
		// Stream start: inject SPS/PPS before first IDR
		if m.h264Cache.HasParameters() {
			frameToSend = m.h264Cache.PrependParameters(frame)
			m.streamerMu.Lock()
			m.uvcNeedParamInject = false
			m.streamerMu.Unlock()
		}
	} else if IsIDRFrame(frame) && !ContainsSPS(frame) {
		// IDR without SPS: prepend cached parameters
		frameToSend = m.h264Cache.PrependParameters(frame)
	}

	// Periodic SPS/PPS injection for clients joining mid-stream
	m.uvcParamInjectCount++
	if m.uvcParamInjectCount >= spsInjectInterval {
		m.uvcParamInjectCount = 0
		// If this is an IDR frame, ensure it has SPS/PPS
		if IsIDRFrame(frame) && m.h264Cache.HasParameters() && !ContainsSPS(frameToSend) {
			frameToSend = m.h264Cache.PrependParameters(frame)
		}
	}

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

// HandleMjpegFrame handles an MJPEG frame from the native encoder.
// This is called when the host selected MJPEG format and MJPEG encoder is enabled.
// Only works for HDMI source (hardware encodes from capture buffer).
func (m *Manager) HandleMjpegFrame(frame []byte) {
	// Only process MJPEG frames when MJPEG format is selected
	m.streamerMu.Lock()
	streamer := m.streamer
	isStreaming := streamer != nil && streamer.IsStreaming()
	isMjpeg := m.uvcMjpegSelected
	m.streamerMu.Unlock()

	if !isStreaming || !isMjpeg {
		return
	}

	// Only process HDMI frames if UVC source is HDMI
	// (camera passthrough cannot use MJPEG - no hardware VDEC on RV1106)
	if !m.source.IsHDMI() {
		return
	}

	// Write MJPEG frame directly to UVC
	m.uvcFrameCount++
	if err := streamer.WriteFrame(frame); err != nil {
		m.uvcFrameErrors++
	}

	// Log stats periodically (debug level)
	if m.uvcFrameCount-m.uvcLastLogFrame >= uvcLogInterval {
		if m.uvcLog != nil {
			m.uvcLog.Debug().
				Int("frames", m.uvcFrameCount).
				Int("errors", m.uvcFrameErrors).
				Msg("UVC MJPEG stats")
		}
		m.uvcLastLogFrame = m.uvcFrameCount
	}
}

// HandleCameraMjpegFrame handles an MJPEG frame from the browser camera.
// This is called when the browser sends MJPEG encoded video over WebSocket
// and the UVC host selected MJPEG format.
func (m *Manager) HandleCameraMjpegFrame(frame []byte) {
	// Only process camera frames if camera passthrough is enabled and source is camera
	if !m.enabled.Load() || !m.source.IsCamera() {
		return
	}

	m.streamerMu.Lock()
	streamer := m.streamer
	isStreaming := streamer != nil && streamer.IsStreaming()
	isMjpeg := m.uvcMjpegSelected
	m.streamerMu.Unlock()

	if !isStreaming || !isMjpeg {
		return
	}

	// Write MJPEG frame directly to UVC
	m.uvcFrameCount++
	if err := streamer.WriteFrame(frame); err != nil {
		m.uvcFrameErrors++
	}

	// Log stats periodically (debug level)
	if m.uvcFrameCount-m.uvcLastLogFrame >= uvcLogInterval {
		if m.uvcLog != nil {
			m.uvcLog.Debug().
				Int("frames", m.uvcFrameCount).
				Int("errors", m.uvcFrameErrors).
				Msg("UVC camera MJPEG stats")
		}
		m.uvcLastLogFrame = m.uvcFrameCount
	}
}
