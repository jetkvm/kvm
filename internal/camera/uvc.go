package camera

import (
	"time"

	"github.com/jetkvm/kvm/internal/usbgadget"
	"github.com/rs/zerolog"
)

const uvcBufferCount = 3

// zerologAdapter wraps zerolog for usbgadget.Logger.
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
	m.streamer.Store(usbgadget.NewUVCStreamer(devicePath, &zerologAdapter{logger: m.uvcLog}))

	m.stopChan = make(chan struct{})
	m.eventLoopRun.Store(true)
	go m.eventLoop()
}

func (m *Manager) StopUVC() {
	m.streamerMu.Lock()
	defer m.streamerMu.Unlock()

	if m.stopChan != nil {
		m.eventLoopRun.Store(false)
		close(m.stopChan)
		m.stopChan = nil
	}

	if streamer := m.streamer.Load(); streamer != nil {
		if err := streamer.StopStreaming(); err != nil && m.uvcLog != nil {
			m.uvcLog.Debug().Err(err).Msg("StopStreaming error during cleanup")
		}
		if err := streamer.Close(); err != nil && m.uvcLog != nil {
			m.uvcLog.Debug().Err(err).Msg("Close error during cleanup")
		}
		m.streamer.Store(nil)
	}
}

func (m *Manager) ReinitUVC(uvcEnabled bool) {
	m.streamerMu.Lock()
	defer m.streamerMu.Unlock()

	if !uvcEnabled {
		return
	}
	if streamer := m.streamer.Load(); streamer != nil && streamer.IsOpen() && streamer.IsValid() {
		return
	}

	devicePath, err := m.gadget.GetUVCVideoDevice()
	if err != nil {
		if m.uvcLog != nil {
			m.uvcLog.Warn().Err(err).Msg("UVC device not found during reinit")
		}
		return
	}

	if streamer := m.streamer.Load(); streamer != nil {
		if err := streamer.StopStreaming(); err != nil && m.uvcLog != nil {
			m.uvcLog.Debug().Err(err).Msg("StopStreaming error during reinit")
		}
		if err := streamer.Close(); err != nil && m.uvcLog != nil {
			m.uvcLog.Debug().Err(err).Msg("Close error during reinit")
		}
	}

	if m.uvcLog != nil {
		m.uvcLog.Info().Str("device", devicePath).Msg("UVC reinitialized (H.264 mode)")
	}
	m.streamer.Store(usbgadget.NewUVCStreamer(devicePath, &zerologAdapter{logger: m.uvcLog}))

	if !m.eventLoopRun.Load() && m.stopChan == nil {
		m.stopChan = make(chan struct{})
		m.eventLoopRun.Store(true)
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

	for m.eventLoopRun.Load() {
		streamer := m.streamer.Load()

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
				if m.uvcLog != nil {
					m.uvcLog.Warn().Err(err).Msg("Failed to open UVC device, retrying")
				}
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
			if m.uvcLog != nil {
				m.uvcLog.Warn().Err(err).Msg("UVC PollEventsWithData failed")
			}
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

	streamer := m.streamer.Load()
	if streamer == nil || streamer.IsStreaming() {
		return
	}

	width, height := streamer.GetCommittedResolution()
	isMjpeg := !streamer.IsH264Format()

	if err := streamer.SetFormatWithCodec(width, height, isMjpeg); err != nil {
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

	streamer := m.streamer.Load()
	if streamer == nil {
		return
	}

	if !streamer.IsStreaming() {
		if err := streamer.RequestBuffers(uvcBufferCount); err != nil {
			if m.uvcLog != nil {
				m.uvcLog.Warn().Err(err).Msg("Failed to request UVC buffers")
			}
			return
		}
		if err := streamer.StartStreaming(); err != nil {
			if m.uvcLog != nil {
				m.uvcLog.Warn().Err(err).Msg("Failed to start UVC streaming")
			}
			return
		}
		// Inject SPS/PPS on first frame so UVC clients can decode
		m.uvcNeedParamInject.Store(true)
		m.uvcParamInjectCount.Store(0)
	}

	formatIndex := streamer.GetCommittedFormatIndex()
	isH264 := streamer.IsH264Format()
	m.uvcMjpegSelected = !isH264
	isHDMI := m.source.IsHDMI()
	width, height := streamer.GetCommittedResolution()
	frameRate := streamer.GetCommittedFrameRate()

	if m.uvcLog != nil {
		m.uvcLog.Info().
			Uint32("width", width).
			Uint32("height", height).
			Int("fps", frameRate).
			Uint8("format_index", formatIndex).
			Bool("isH264", isH264).
			Msg("UVC host committed format")
	}

	m.configureEncoderForStreaming(formatIndex, isHDMI)
	m.uvcStreamingFast.Store(true)
	m.uvcMjpegFast.Store(m.uvcMjpegSelected)

	codec := CodecH264
	if m.uvcMjpegSelected {
		codec = CodecMJPEG
	}
	m.notifyFormatChange(FormatInfo{
		Codec:     codec,
		Width:     int(width),
		Height:    int(height),
		FrameRate: frameRate,
	})
}

func (m *Manager) configureEncoderForStreaming(formatIndex uint8, isHDMI bool) {
	if m.native == nil {
		if m.uvcLog != nil && m.uvcMjpegSelected {
			m.uvcLog.Warn().
				Uint8("format_index", formatIndex).
				Str("format", "MJPEG").
				Msg("UVC: Host selected MJPEG but native controller not available")
		}
		return
	}

	source := m.source.Get().String()
	format := "H.264"
	if m.uvcMjpegSelected {
		format = "MJPEG"
	}

	switch {
	case m.uvcMjpegSelected && isHDMI:
		m.native.MjpegSetEnabled(true)
		if m.uvcLog != nil {
			m.uvcLog.Info().
				Uint8("format_index", formatIndex).
				Str("format", format).
				Str("source", source).
				Msg("UVC MJPEG streaming started (hardware encoder enabled)")
		}

	case m.uvcMjpegSelected && !isHDMI:
		m.native.MjpegSetEnabled(false)
		if m.uvcLog != nil {
			m.uvcLog.Warn().
				Uint8("format_index", formatIndex).
				Str("format", format).
				Str("source", source).
				Msg("UVC: Camera source with MJPEG format - H.264 transcoding not available on RV1106")
		}

	default:
		m.native.MjpegSetEnabled(false)
		if m.uvcLog != nil {
			m.uvcLog.Info().
				Uint8("format_index", formatIndex).
				Str("format", format).
				Str("source", source).
				Msg("UVC H.264 streaming started")
		}
	}
}

func (m *Manager) stopStreaming() {
	m.uvcStreamingFast.Store(false)
	m.uvcMjpegFast.Store(false)

	m.streamerMu.Lock()
	defer m.streamerMu.Unlock()

	streamer := m.streamer.Load()
	if streamer == nil {
		return
	}

	if m.uvcMjpegSelected && m.native != nil {
		m.native.MjpegSetEnabled(false)
	}

	wasMjpeg := m.uvcMjpegSelected
	m.uvcMjpegSelected = false

	m.notifyStreamingStopped()

	if err := streamer.StopStreaming(); err != nil {
		if m.uvcLog != nil {
			m.uvcLog.Debug().Err(err).Msg("StopStreaming error")
		}
	}

	m.h264Cache.Clear()
	m.uvcNeedParamInject.Store(false)
	m.uvcParamInjectCount.Store(0)

	if m.uvcLog != nil {
		if wasMjpeg {
			m.uvcLog.Info().Msg("UVC MJPEG streaming stopped")
		} else {
			m.uvcLog.Info().Msg("UVC H.264 streaming stopped")
		}
	}
}

// HandleH264Frame processes H.264 frames from the native encoder (HDMI source).
func (m *Manager) HandleH264Frame(frame []byte) {
	if !m.uvcStreamingFast.Load() || !m.source.IsHDMI() {
		return
	}
	m.writeFrameToUVC(frame)
}

// HandleCameraH264Frame processes H.264 frames from the browser camera.
func (m *Manager) HandleCameraH264Frame(frame []byte) {
	if !m.uvcStreamingFast.Load() {
		return
	}
	if !m.source.IsCamera() || !m.enabled.Load() || m.uvcMjpegFast.Load() {
		return
	}
	if streamer := m.streamer.Load(); streamer != nil {
		if err := streamer.WriteFrame(frame); err != nil {
			errCount := m.uvcFrameErrors.Add(1)
			// Log periodically: first error and every 128th (power of 2 for fast bitwise AND)
			if errCount == 1 || errCount&127 == 0 {
				if m.uvcLog != nil {
					m.uvcLog.Warn().Uint32("total_errors", errCount).Err(err).Msg("Camera H.264 WriteFrame failed")
				}
			}
		}
	}
}

const spsInjectInterval = 256 // Re-inject SPS/PPS every ~4-8 seconds (power of 2 for fast check)

// writeFrameToUVC writes H.264 frames to UVC (HDMI path only).
func (m *Manager) writeFrameToUVC(frame []byte) {
	if m.uvcMjpegFast.Load() {
		return
	}

	streamer := m.streamer.Load()
	if streamer == nil {
		return
	}

	// Fast path: P-frames (95%+ of frames) need no SPS/PPS processing
	firstNALType := GetFirstNALType(frame)
	isIDR := firstNALType == NALTypeIDR
	isSPS := firstNALType == NALTypeSPS

	if !isIDR && !isSPS {
		if err := streamer.WriteFrame(frame); err != nil {
			errCount := m.uvcFrameErrors.Add(1)
			if errCount == 1 || errCount&127 == 0 {
				if m.uvcLog != nil {
					m.uvcLog.Warn().Uint32("total_errors", errCount).Err(err).Msg("HDMI H.264 WriteFrame failed")
				}
			}
		}
		return
	}

	// IDR/SPS frames: may need parameter injection
	needParamInject := m.uvcNeedParamInject.Load()
	frameToSend := frame

	if isSPS {
		frameInfo := m.h264Cache.AnalyzeAndUpdate(frame)
		if needParamInject && frameInfo.HasIDR {
			m.uvcNeedParamInject.Store(false)
		}
	} else if isIDR {
		paramInjectCount := m.uvcParamInjectCount.Load()
		if needParamInject || paramInjectCount >= spsInjectInterval {
			quickInfo := QuickFrameInfo(frame)
			if !quickInfo.HasSPS && m.h264Cache.HasParameters() {
				frameToSend = m.h264Cache.PrependParametersWithInfo(frame, quickInfo)
			}
			if needParamInject {
				m.uvcNeedParamInject.Store(false)
			}
			m.uvcParamInjectCount.Store(0)
		}
	}

	m.uvcParamInjectCount.Add(1)

	if err := streamer.WriteFrame(frameToSend); err != nil {
		errCount := m.uvcFrameErrors.Add(1)
		if errCount == 1 || errCount&127 == 0 {
			if m.uvcLog != nil {
				m.uvcLog.Warn().Uint32("total_errors", errCount).Err(err).Msg("HDMI H.264 (IDR) WriteFrame failed")
			}
		}
	}
}

func (m *Manager) IsStreaming() bool {
	m.streamerMu.Lock()
	defer m.streamerMu.Unlock()
	streamer := m.streamer.Load()
	return streamer != nil && streamer.IsStreaming()
}

// RestoreMjpegState re-enables MJPEG encoder after native process restart.
func (m *Manager) RestoreMjpegState() {
	m.streamerMu.Lock()
	streamer := m.streamer.Load()
	isStreaming := streamer != nil && streamer.IsStreaming()
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

// HandleMjpegFrame processes MJPEG frames from the native encoder (HDMI source).
func (m *Manager) HandleMjpegFrame(frame []byte) {
	if !m.uvcStreamingFast.Load() || !m.uvcMjpegFast.Load() || !m.source.IsHDMI() {
		return
	}
	if streamer := m.streamer.Load(); streamer != nil {
		if err := streamer.WriteFrame(frame); err != nil {
			errCount := m.uvcFrameErrors.Add(1)
			if errCount == 1 || errCount&127 == 0 {
				if m.uvcLog != nil {
					m.uvcLog.Warn().Uint32("total_errors", errCount).Err(err).Msg("HDMI MJPEG WriteFrame failed")
				}
			}
		}
	}
}

// HandleCameraMjpegFrame processes MJPEG frames from the browser camera.
func (m *Manager) HandleCameraMjpegFrame(frame []byte) {
	if !m.uvcStreamingFast.Load() {
		return
	}
	if !m.source.IsCamera() || !m.enabled.Load() || !m.uvcMjpegFast.Load() {
		return
	}
	if streamer := m.streamer.Load(); streamer != nil {
		if err := streamer.WriteFrame(frame); err != nil {
			errCount := m.uvcFrameErrors.Add(1)
			if errCount == 1 || errCount&127 == 0 {
				if m.uvcLog != nil {
					m.uvcLog.Warn().Uint32("total_errors", errCount).Err(err).Msg("Camera MJPEG WriteFrame failed")
				}
			}
		}
	}
}
