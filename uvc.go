package kvm

import (
	"sync"
	"time"

	"github.com/jetkvm/kvm/internal/usbgadget"
	"github.com/rs/zerolog"
)

// UVC streaming manager
var (
	uvcStreamer     *usbgadget.UVCStreamer
	uvcMutex        sync.Mutex
	uvcEventLoopRun bool
	uvcStopChan     chan struct{}
)

// zerologAdapter wraps zerolog to implement usbgadget.Logger interface
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

// initUVC initializes UVC streaming if enabled
func initUVC() {
	uvcMutex.Lock()
	defer uvcMutex.Unlock()

	if !config.UsbDevices.UVC {
		uvcLog.Info().Msg("UVC not enabled in config, skipping UVC init")
		return
	}

	// Find the UVC gadget device
	devicePath, err := gadget.GetUVCVideoDevice()
	if err != nil {
		uvcLog.Warn().Err(err).Msg("Failed to find UVC video device")
		return
	}

	uvcLog.Info().Str("device", devicePath).Msg("Found UVC video device")

	// Create the UVC streamer
	uvcStreamer = usbgadget.NewUVCStreamer(devicePath, &zerologAdapter{logger: uvcLog})

	// Start the UVC event loop
	uvcStopChan = make(chan struct{})
	uvcEventLoopRun = true
	go uvcEventLoop()
}

// stopUVC stops UVC streaming
func stopUVC() {
	uvcMutex.Lock()
	defer uvcMutex.Unlock()

	if uvcStopChan != nil {
		uvcEventLoopRun = false
		close(uvcStopChan)
		uvcStopChan = nil
	}

	if uvcStreamer != nil {
		uvcStreamer.StopStreaming()
		uvcStreamer.Close()
		uvcStreamer = nil
	}
}

// reinitUVC reinitializes UVC after USB reconfiguration
// This finds the new UVC device path and creates a new streamer
// IMPORTANT: Don't disrupt an existing working connection - the host may be enumerating
func reinitUVC() {
	uvcMutex.Lock()
	defer uvcMutex.Unlock()

	if !config.UsbDevices.UVC {
		return
	}

	// If we have an existing streamer that is open and valid, don't reinit
	// USB re-enumeration can trigger this, and we don't want to disrupt a working session
	if uvcStreamer != nil && uvcStreamer.IsOpen() && uvcStreamer.IsValid() {
		uvcLog.Debug().Msg("UVC reinit skipped - existing streamer is still valid")
		return
	}

	// Find the new UVC gadget device (path may have changed)
	devicePath, err := gadget.GetUVCVideoDevice()
	if err != nil {
		uvcLog.Warn().Err(err).Msg("Failed to find UVC video device during reinit")
		return
	}

	// If we have an existing streamer, close it before creating new one
	if uvcStreamer != nil {
		uvcLog.Info().Msg("Closing existing UVC streamer for reinit")
		uvcStreamer.StopStreaming()
		uvcStreamer.Close()
	}

	uvcLog.Info().Str("device", devicePath).Msg("Reinitializing UVC with device")

	// Create new UVC streamer with the new path
	uvcStreamer = usbgadget.NewUVCStreamer(devicePath, &zerologAdapter{logger: uvcLog})

	// Start event loop if not running
	if !uvcEventLoopRun && uvcStopChan == nil {
		uvcStopChan = make(chan struct{})
		uvcEventLoopRun = true
		go uvcEventLoop()
	}
}

// uvcEventLoop monitors UVC events and manages streaming state
func uvcEventLoop() {
	for uvcEventLoopRun {
		uvcMutex.Lock()
		streamer := uvcStreamer
		uvcMutex.Unlock()

		if streamer == nil {
			time.Sleep(time.Second)
			continue
		}

		// Check if device is open and still valid
		if streamer.IsOpen() && !streamer.IsValid() {
			uvcLog.Warn().Msg("UVC device became invalid, closing for recovery")
			streamer.Close()
			time.Sleep(500 * time.Millisecond) // Wait for device to potentially reappear
		}

		// Try to open the device if not open
		if !streamer.IsOpen() {
			if err := streamer.Open(); err != nil {
				uvcLog.Debug().Err(err).Msg("UVC device not ready")
				time.Sleep(time.Second)
				continue
			}

			// Subscribe to UVC events
			if err := streamer.SubscribeEvents(); err != nil {
				uvcLog.Warn().Err(err).Msg("Failed to subscribe to UVC events")
			}
			uvcLog.Info().Msg("UVC device opened and events subscribed")
		}

		// Poll for UVC events (non-blocking) with sleep to prevent CPU spin
		eventType, eventData, err := streamer.PollEventsWithData()
		if err != nil {
			uvcLog.Debug().Err(err).Msg("Error polling UVC event")
			time.Sleep(100 * time.Millisecond)
			continue
		}

		if eventType != 0 {
			uvcLog.Info().Uint32("eventType", eventType).Msg("UVC event received")
			handleUVCEvent(streamer, eventType, eventData)
		}

		// Sleep to prevent CPU spin (20ms = 50 polls/sec, enough for USB control)
		time.Sleep(20 * time.Millisecond)
	}
}

// handleUVCEvent handles UVC events from the host
// For SETUP and DATA events, this sends proper responses via UVCIOC_SEND_RESPONSE
// which is critical for USB enumeration - the host will reset the device without responses
func handleUVCEvent(streamer *usbgadget.UVCStreamer, eventType uint32, eventData []byte) {
	switch eventType {
	case usbgadget.UVC_EVENT_CONNECT:
		uvcLog.Info().Msg("UVC: Host connected")

	case usbgadget.UVC_EVENT_DISCONNECT:
		uvcLog.Info().Msg("UVC: Host disconnected")
		stopUVCStreaming()

	case usbgadget.UVC_EVENT_STREAMON:
		uvcLog.Info().Msg("UVC: Host requested streaming start")
		startUVCStreaming()

	case usbgadget.UVC_EVENT_STREAMOFF:
		uvcLog.Info().Msg("UVC: Host requested streaming stop")
		stopUVCStreaming()

	case usbgadget.UVC_EVENT_SETUP:
		// Critical: Must respond to SETUP events or host will reset device
		if err := streamer.HandleSetupEvent(eventData); err != nil {
			uvcLog.Warn().Err(err).Msg("Failed to handle UVC SETUP event")
		}

	case usbgadget.UVC_EVENT_DATA:
		// Handle data received from SET_CUR requests
		wasCommit, err := streamer.HandleDataEvent(eventData)
		if err != nil {
			uvcLog.Warn().Err(err).Msg("Failed to handle UVC DATA event")
		}
		// If this was a COMMIT, prepare V4L2 streaming NOW so it's ready
		// when the host sends SET_INTERFACE (which triggers STREAMON)
		if wasCommit {
			prepareUVCStreaming()
		}
	}
}

// prepareUVCStreaming prepares V4L2 streaming after COMMIT
// This must be called BEFORE the host sends SET_INTERFACE
func prepareUVCStreaming() {
	uvcMutex.Lock()
	defer uvcMutex.Unlock()

	if uvcStreamer == nil {
		return
	}

	// If already streaming, don't re-prepare
	if uvcStreamer.IsStreaming() {
		uvcLog.Debug().Msg("UVC streaming already active, skipping prepare")
		return
	}

	// Get the committed resolution and set the V4L2 format
	width, height := uvcStreamer.GetCommittedResolution()
	uvcLog.Info().Uint32("width", width).Uint32("height", height).Msg("Setting V4L2 format for committed resolution")
	if err := uvcStreamer.SetFormat(width, height); err != nil {
		uvcLog.Warn().Err(err).Msg("Failed to set UVC format")
		// Continue anyway - format setting may not be supported
	}

	// Request buffers with correct size based on negotiated format
	if err := uvcStreamer.RequestBuffers(2); err != nil {
		uvcLog.Warn().Err(err).Msg("Failed to request UVC buffers")
		return
	}

	// Start V4L2 streaming so device is ready when host sends SET_INTERFACE
	if err := uvcStreamer.StartStreaming(); err != nil {
		uvcLog.Warn().Err(err).Msg("Failed to start UVC streaming")
		return
	}

	uvcLog.Info().Msg("UVC streaming prepared (V4L2 STREAMON sent)")
}

// startUVCStreaming enables MJPEG encoding when host requests streaming
// V4L2 streaming should already be prepared from COMMIT
func startUVCStreaming() {
	uvcMutex.Lock()
	defer uvcMutex.Unlock()

	if uvcStreamer == nil {
		return
	}

	// If not already streaming, prepare now (fallback)
	if !uvcStreamer.IsStreaming() {
		uvcLog.Warn().Msg("V4L2 streaming not prepared, doing it now")
		if err := uvcStreamer.RequestBuffers(2); err != nil {
			uvcLog.Warn().Err(err).Msg("Failed to request UVC buffers")
			return
		}
		if err := uvcStreamer.StartStreaming(); err != nil {
			uvcLog.Warn().Err(err).Msg("Failed to start UVC streaming")
			return
		}
	}

	uvcLog.Info().Msg("UVC streaming started")

	// Enable MJPEG encoding in native layer
	if nativeInstance != nil {
		uvcLog.Info().Msg("Enabling MJPEG encoding for UVC")
		nativeInstance.MjpegSetEnabled(true)
	} else {
		uvcLog.Warn().Msg("nativeInstance is nil, cannot enable MJPEG")
	}
}

// stopUVCStreaming stops UVC streaming and disables MJPEG encoding
func stopUVCStreaming() {
	uvcMutex.Lock()
	defer uvcMutex.Unlock()

	if uvcStreamer == nil {
		return
	}

	// Disable MJPEG encoding
	if nativeInstance != nil {
		uvcLog.Info().Msg("Disabling MJPEG encoding for UVC")
		nativeInstance.MjpegSetEnabled(false)
	}

	// Stop V4L2 streaming
	if err := uvcStreamer.StopStreaming(); err != nil {
		uvcLog.Warn().Err(err).Msg("Failed to stop UVC streaming")
	}

	uvcLog.Info().Msg("UVC streaming stopped")
}

// handleMjpegFrame handles MJPEG frames from the native layer
// This is called from the native MJPEG callback
func handleMjpegFrame(frame []byte) {
	uvcMutex.Lock()
	streamer := uvcStreamer
	uvcMutex.Unlock()

	if streamer == nil || !streamer.IsStreaming() {
		return
	}

	if err := streamer.WriteFrame(frame); err != nil {
		uvcLog.Warn().Err(err).Int("frameSize", len(frame)).Msg("Failed to write MJPEG frame to UVC")
	}
}
