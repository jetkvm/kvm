package camera

import (
	"time"

	"github.com/jetkvm/kvm/internal/usbgadget"
	"github.com/rs/zerolog"
)

const (
	uvcBufferCount = 4
	uvcLogInterval = 30 // Log every N frames (~1 second at 30fps)
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
		m.uvcLog.Info().Str("device", devicePath).Msg("UVC initialized")
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
		m.uvcLog.Info().Str("device", devicePath).Msg("UVC reinitialized")
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
				m.uvcLog.Info().Msg("UVC device ready")
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
	}

	// Only enable native MJPEG encoder if UVC source is HDMI
	// If source is camera, the browser will send frames via DataChannel
	if m.native != nil && m.source.IsHDMI() {
		m.native.MjpegSetEnabled(true)
	}
	if m.uvcLog != nil {
		m.uvcLog.Info().Str("source", m.source.Get().String()).Msg("UVC streaming started")
	}
}

func (m *Manager) stopStreaming() {
	m.streamerMu.Lock()
	defer m.streamerMu.Unlock()

	if m.streamer == nil {
		return
	}

	if m.native != nil {
		m.native.MjpegSetEnabled(false)
	}

	if err := m.streamer.StopStreaming(); err != nil {
		if m.uvcLog != nil {
			m.uvcLog.Debug().Err(err).Msg("StopStreaming error")
		}
	}
	if m.uvcLog != nil {
		m.uvcLog.Info().Msg("UVC streaming stopped")
	}
}

// HandleMjpegFrame handles an MJPEG frame from either HDMI or camera source.
func (m *Manager) HandleMjpegFrame(frame []byte) {
	m.streamerMu.Lock()
	streamer := m.streamer
	m.streamerMu.Unlock()

	if streamer == nil || !streamer.IsStreaming() {
		return
	}

	m.uvcFrameCount++
	if err := streamer.WriteFrame(frame); err != nil {
		m.uvcFrameErrors++
	}

	if m.uvcFrameCount-m.uvcLastLogFrame >= uvcLogInterval {
		if m.uvcLog != nil {
			m.uvcLog.Info().
				Int("frames", m.uvcFrameCount).
				Int("errors", m.uvcFrameErrors).
				Int("size", len(frame)).
				Msg("UVC stats")
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
