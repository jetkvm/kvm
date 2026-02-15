package camera

import (
	"fmt"
	"runtime/debug"
	"time"

	"github.com/jetkvm/kvm/internal/usbgadget"
)

// uvcBufferCount is the number of V4L2 buffers to allocate.
// 3 buffers provides triple-buffering (one being filled, one ready, one in-flight).
const uvcBufferCount = 3

// errorLogInterval is a bitmask for throttling frame error logs.
// Logs at error count 1, then at multiples of 128 (128, 256, 384...).
// Condition: errCount == 1 || errCount & errorLogInterval == 0.
const errorLogInterval = 127

// codecName returns the display name for a codec.
func codecName(isMjpeg bool) string {
	if isMjpeg {
		return "MJPEG"
	}
	return "H.264"
}

// InitUVC initializes the UVC subsystem and starts the event loop.
// If uvcEnabled is false, this is a no-op and returns nil.
// Returns an error if the UVC video device cannot be found.
func (m *Manager) InitUVC(uvcEnabled bool) error {
	m.streamerMu.Lock()
	defer m.streamerMu.Unlock()

	if !uvcEnabled {
		return nil
	}

	devicePath, err := m.gadget.GetUVCVideoDevice()
	if err != nil {
		if m.uvcLog != nil {
			m.uvcLog.Warn().Err(err).Msg("UVC device not found")
		}
		return err
	}

	if m.uvcLog != nil {
		m.uvcLog.Info().Str("device", devicePath).Msg("UVC initialized")
	}
	m.streamer.Store(usbgadget.NewUVCStreamer(devicePath, m.uvcLog))

	m.eventLoopRun.Store(true)
	go m.eventLoop()
	return nil
}

// StopUVC stops UVC streaming and releases all resources.
// Safe to call if UVC was never initialized.
func (m *Manager) StopUVC() {
	m.streamerMu.Lock()
	defer m.streamerMu.Unlock()

	// Signal event loop to stop (it checks this atomic at each iteration)
	m.eventLoopRun.Store(false)

	if streamer := m.streamer.Load(); streamer != nil {
		if err := streamer.StopStreaming(); err != nil && m.uvcLog != nil {
			m.uvcLog.Warn().Err(err).Msg("StopStreaming error during cleanup")
		}
		if err := streamer.Close(); err != nil && m.uvcLog != nil {
			m.uvcLog.Warn().Err(err).Msg("Close error during cleanup")
		}
		m.streamer.Store(nil)
	}
}

// ReinitUVC reinitializes UVC if the device became invalid (e.g., USB disconnect/reconnect).
// Unlike InitUVC, this checks if reinitialization is actually needed before proceeding.
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
		if streamer := m.streamer.Load(); streamer != nil {
			if closeErr := streamer.Close(); closeErr != nil && m.uvcLog != nil {
				m.uvcLog.Warn().Err(closeErr).Msg("Close error during reinit cleanup")
			}
			m.streamer.Store(nil)
		}
		m.uvcStreamingFast.Store(false)
		m.uvcMjpegFast.Store(false)
		m.notifyStreamingStopped()
		return
	}

	if streamer := m.streamer.Load(); streamer != nil {
		if err := streamer.StopStreaming(); err != nil && m.uvcLog != nil {
			m.uvcLog.Warn().Err(err).Msg("StopStreaming error during reinit")
		}
		if err := streamer.Close(); err != nil && m.uvcLog != nil {
			m.uvcLog.Warn().Err(err).Msg("Close error during reinit")
		}
	}

	if m.uvcLog != nil {
		m.uvcLog.Info().Str("device", devicePath).Msg("UVC reinitialized")
	}
	m.streamer.Store(usbgadget.NewUVCStreamer(devicePath, m.uvcLog))

	// Start event loop if not already running
	if !m.eventLoopRun.Load() {
		m.eventLoopRun.Store(true)
		go m.eventLoop()
	}
}

// eventLoop runs the UVC event processing loop in a goroutine.
// It handles device open/close, event subscription, and streaming state transitions.
// The loop runs until eventLoopRun is set to false via StopUVC.
// Note: PollEventsWithData is only called from this goroutine, ensuring thread-safe
// access to the file descriptor without requiring the mutex during polling.
func (m *Manager) eventLoop() {
	defer func() {
		if r := recover(); r != nil {
			if m.uvcLog != nil {
				m.uvcLog.Error().
					Interface("panic", r).
					Str("stack", string(debug.Stack())).
					Msg("UVC event loop panic - recovering")
			}
			m.eventLoopRun.Store(false)
			m.uvcStreamingFast.Store(false)
			m.uvcMjpegFast.Store(false)
			// Notify browser that streaming stopped unexpectedly so it can stop encoding
			m.notifyStreamingStopped()
			// Notify higher layers about the panic for alerting/recovery
			if m.onPanic != nil {
				m.onPanic(r)
			}
		}
	}()

	const (
		pollInterval       = 20 * time.Millisecond  // Balance latency vs CPU usage (50 polls/sec)
		retryInterval      = time.Second            // Backoff on open/subscribe failures
		recoveryDelay      = 500 * time.Millisecond // Allow USB stack to stabilize after errors
		settlingDelay      = 50 * time.Millisecond  // Debounce rapid USB connect/disconnect events
		maxEventsPerPoll   = 16                     // Drain event queue to find final state
		maxOpenFailures    = 5                      // Rediscover device path after this many failures
		rediscoveryBackoff = 2 * time.Second        // Wait before rediscovering device path
	)

	type pendingEvent struct {
		eventType uint32
		eventData []byte
	}

	openFailureCount := 0

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
			if err := streamer.Close(); err != nil && m.uvcLog != nil {
				m.uvcLog.Warn().Err(err).Msg("Close failed during recovery")
			}
			openFailureCount = 0 // Reset on intentional close
			time.Sleep(recoveryDelay)
			continue
		}

		if !streamer.IsOpen() {
			if err := streamer.Open(); err != nil {
				openFailureCount++
				if m.uvcLog != nil {
					m.uvcLog.Warn().Err(err).Int("failures", openFailureCount).Msg("Failed to open UVC device, retrying")
				}

				// After too many failures, the device path may have changed (USB reconfiguration)
				// Rediscover the device path by creating a new streamer
				if openFailureCount >= maxOpenFailures {
					if m.uvcLog != nil {
						m.uvcLog.Info().Msg("Too many open failures, rediscovering UVC device path")
					}
					openFailureCount = 0

					// Close old streamer and rediscover device
					if err := streamer.Close(); err != nil && m.uvcLog != nil {
						m.uvcLog.Warn().Err(err).Msg("Close failed during rediscovery")
					}

					devicePath, err := m.gadget.GetUVCVideoDevice()
					if err != nil {
						if m.uvcLog != nil {
							m.uvcLog.Warn().Err(err).Msg("UVC device not found during rediscovery")
						}
						m.streamer.Store(nil)
						time.Sleep(rediscoveryBackoff)
						continue
					}

					if m.uvcLog != nil {
						m.uvcLog.Info().Str("device", devicePath).Msg("UVC device path rediscovered")
					}
					m.streamer.Store(usbgadget.NewUVCStreamer(devicePath, m.uvcLog))
					time.Sleep(recoveryDelay)
					continue
				}

				time.Sleep(retryInterval)
				continue
			}
			openFailureCount = 0 // Reset on successful open
			if err := streamer.SubscribeEvents(); err != nil {
				if m.uvcLog != nil {
					m.uvcLog.Error().Err(err).Msg("Failed to subscribe to UVC events")
				}
				if closeErr := streamer.Close(); closeErr != nil && m.uvcLog != nil {
					m.uvcLog.Warn().Err(closeErr).Msg("Close failed after SubscribeEvents error")
				}
				time.Sleep(retryInterval)
				continue
			}
			if m.uvcLog != nil {
				m.uvcLog.Info().Msg("UVC device ready")
			}
		}

		var events []pendingEvent
		for i := 0; i < maxEventsPerPoll; i++ {
			eventType, eventData, err := streamer.PollEventsWithData()
			if err != nil {
				if m.uvcLog != nil {
					m.uvcLog.Warn().Err(err).Msg("UVC PollEventsWithData failed")
				}
				break
			}
			if eventType == 0 {
				break
			}
			dataCopy := make([]byte, len(eventData))
			copy(dataCopy, eventData)
			events = append(events, pendingEvent{eventType: eventType, eventData: dataCopy})
		}

		if len(events) == 0 {
			time.Sleep(pollInterval)
			continue
		}

		wantStreaming := false
		sawStreamingEvent := false
		for _, ev := range events {
			switch ev.eventType {
			case usbgadget.UVC_EVENT_STREAMON:
				wantStreaming = true
				sawStreamingEvent = true
			case usbgadget.UVC_EVENT_STREAMOFF, usbgadget.UVC_EVENT_DISCONNECT:
				wantStreaming = false
				sawStreamingEvent = true
			default:
				m.handleEvent(streamer, ev.eventType, ev.eventData)
			}
		}

		if sawStreamingEvent {
			if wantStreaming {
				time.Sleep(settlingDelay)

				for i := 0; i < maxEventsPerPoll; i++ {
					eventType, _, err := streamer.PollEventsWithData()
					if err != nil || eventType == 0 {
						break
					}
					if eventType == usbgadget.UVC_EVENT_STREAMOFF || eventType == usbgadget.UVC_EVENT_DISCONNECT {
						wantStreaming = false
						break
					}
				}
				if wantStreaming {
					if err := m.startStreaming(); err != nil {
						if m.uvcLog != nil {
							m.uvcLog.Error().Err(err).Msg("Failed to start streaming")
						}
						// Notify browser so it stops encoding (it received format during prepare)
						m.notifyStreamingStopped()
					}
				}
			} else {
				m.stopStreaming()
			}
		}

		time.Sleep(pollInterval)
	}
}

// handleEvent processes a single UVC event from the kernel driver.
// CONNECT events are logged; SETUP and DATA events are forwarded to the streamer.
// Streaming events (STREAMON/STREAMOFF/DISCONNECT) are handled separately in eventLoop.
func (m *Manager) handleEvent(streamer *usbgadget.UVCStreamer, eventType uint32, eventData []byte) {
	switch eventType {
	case usbgadget.UVC_EVENT_CONNECT:
		if m.uvcLog != nil {
			m.uvcLog.Debug().Msg("UVC connected")
		}

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

// prepareStreaming configures the V4L2 output format after the host commits a format.
// Called when UVC_VS_COMMIT_CONTROL is received. Sets the pixel format (MJPEG or H.264)
// to match what the host negotiated. Errors are logged but not fatal since the gadget
// driver will use the negotiated format regardless.
func (m *Manager) prepareStreaming() {
	m.streamerMu.Lock()
	defer m.streamerMu.Unlock()

	streamer := m.streamer.Load()
	if streamer == nil {
		return
	}
	if streamer.IsStreaming() {
		return
	}

	width, height := streamer.GetCommittedResolution()
	isMjpeg := !streamer.IsH264Format()

	codec := codecName(isMjpeg)
	if err := streamer.SetFormatWithCodec(width, height, isMjpeg); err != nil {
		if m.uvcLog != nil {
			// errno=25 (ENOTTY) is expected for UVC gadgets - the USB host negotiates
			// the format, so VIDIOC_S_FMT is not supported. This is normal and harmless.
			m.uvcLog.Debug().
				Err(err).
				Uint32("width", width).
				Uint32("height", height).
				Str("codec", codec).
				Msg("UVC format set by host (S_FMT not supported)")
		}
	} else if m.uvcLog != nil {
		m.uvcLog.Info().
			Uint32("width", width).
			Uint32("height", height).
			Str("codec", codec).
			Msg("V4L2 format set for UVC output")
	}
}

// startStreaming activates V4L2 streaming and notifies the browser of the format.
// Called when UVC_EVENT_STREAMON is received and confirmed stable. Allocates V4L2
// buffers, starts the streaming pipeline, and sends a format notification to the
// browser so it can configure its encoder to match.
func (m *Manager) startStreaming() error {
	m.streamerMu.Lock()
	defer m.streamerMu.Unlock()

	streamer := m.streamer.Load()
	if streamer == nil {
		return fmt.Errorf("no UVC streamer available")
	}

	if !streamer.IsStreaming() {
		// Reset frame stats for the new session
		m.ResetFrameStats()
		if err := streamer.RequestBuffers(uvcBufferCount); err != nil {
			return fmt.Errorf("failed to request UVC buffers: %w", err)
		}
		if err := streamer.StartStreaming(); err != nil {
			return fmt.Errorf("failed to start UVC streaming: %w", err)
		}
	}

	formatIndex := streamer.GetCommittedFormatIndex()
	isH264 := streamer.IsH264Format()
	isMjpeg := !isH264
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

	m.uvcStreamingFast.Store(true)
	m.uvcMjpegFast.Store(isMjpeg)

	codec := CodecH264
	if isMjpeg {
		codec = CodecMJPEG
	}
	formatInfo, err := NewFormatInfo(codec, int(width), int(height), frameRate)
	if err != nil {
		// Should never happen with UVC-negotiated values
		if m.uvcLog != nil {
			m.uvcLog.Error().Err(err).
				Str("codec", string(codec)).
				Int("width", int(width)).
				Int("height", int(height)).
				Int("frameRate", frameRate).
				Msg("UVC format validation failed - not notifying browser")
		}
		return fmt.Errorf("invalid UVC format: %w", err)
	}
	m.notifyFormatChange(formatInfo)
	return nil
}

// stopStreaming deactivates V4L2 streaming and notifies the browser.
// Called when UVC_EVENT_STREAMOFF or UVC_EVENT_DISCONNECT is received.
// Resets all streaming state flags and error counters for the next session.
func (m *Manager) stopStreaming() {
	m.uvcStreamingFast.Store(false)
	m.uvcMjpegFast.Store(false)
	m.uvcFrameErrors.Store(0) // Reset error counter for next streaming session

	m.streamerMu.Lock()
	defer m.streamerMu.Unlock()

	streamer := m.streamer.Load()
	if streamer == nil {
		return
	}

	m.notifyStreamingStopped()
	if err := streamer.StopStreaming(); err != nil && m.uvcLog != nil {
		m.uvcLog.Warn().Err(err).Msg("StopStreaming error")
	}
}

// logFrameError logs frame write errors with throttling to avoid log spam.
func (m *Manager) logFrameError(err error, codec string) {
	errCount := m.uvcFrameErrors.Add(1)
	if errCount == 1 || errCount&errorLogInterval == 0 {
		if m.uvcLog != nil {
			m.uvcLog.Warn().Uint32("total_errors", errCount).Err(err).Msgf("Camera %s WriteFrame failed", codec)
		}
	}
}

// HandleCameraH264Frame processes H.264 frames from the browser camera.
// HOTPATH: Called for every frame at the negotiated frame rate.
// Frames are silently dropped (with counter increment) when:
// - UVC is not streaming (host hasn't started capture)
// - Camera passthrough is disabled
// - Host requested MJPEG codec instead of H.264
// Use GetFrameStats() to monitor drop rates for debugging.
func (m *Manager) HandleCameraH264Frame(frame []byte) {
	if !m.uvcStreamingFast.Load() || !m.enabled.Load() || m.uvcMjpegFast.Load() {
		m.droppedStateFrames.Add(1) // Track for observability
		return
	}
	if streamer := m.streamer.Load(); streamer != nil {
		if err := streamer.WriteFrame(frame); err != nil {
			// ErrNotStreaming is a state issue (race), not a write failure
			if err == usbgadget.ErrNotStreaming {
				m.droppedStateFrames.Add(1)
			} else {
				m.droppedWriteFrames.Add(1)
				m.logFrameError(err, "H.264")
			}
		}
	} else {
		m.droppedStateFrames.Add(1)
	}
}

// HandleCameraMjpegFrame processes MJPEG frames from the browser camera.
// HOTPATH: Called for every frame at the negotiated frame rate.
// Frames are silently dropped (with counter increment) when:
// - UVC is not streaming (host hasn't started capture)
// - Camera passthrough is disabled
// - Host requested H.264 codec instead of MJPEG
// Use GetFrameStats() to monitor drop rates for debugging.
func (m *Manager) HandleCameraMjpegFrame(frame []byte) {
	if !m.uvcStreamingFast.Load() || !m.enabled.Load() || !m.uvcMjpegFast.Load() {
		m.droppedStateFrames.Add(1) // Track for observability
		return
	}
	if streamer := m.streamer.Load(); streamer != nil {
		if err := streamer.WriteFrame(frame); err != nil {
			// ErrNotStreaming is a state issue (race), not a write failure
			if err == usbgadget.ErrNotStreaming {
				m.droppedStateFrames.Add(1)
			} else {
				m.droppedWriteFrames.Add(1)
				m.logFrameError(err, "MJPEG")
			}
		}
	} else {
		m.droppedStateFrames.Add(1)
	}
}

// IsStreaming returns true if the UVC device is actively streaming frames.
func (m *Manager) IsStreaming() bool {
	streamer := m.streamer.Load()
	return streamer != nil && streamer.IsStreaming()
}
