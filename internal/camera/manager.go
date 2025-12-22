package camera

import (
	"sync"
	"sync/atomic"

	"github.com/jetkvm/kvm/internal/usbgadget"
	"github.com/rs/zerolog"
)

// GadgetController interface for USB gadget operations.
type GadgetController interface {
	GetUVCVideoDevice() (string, error)
}

// NativeController interface for native operations (MJPEG encoding and transcoding).
type NativeController interface {
	MjpegSetEnabled(enabled bool)

	// Transcoder methods for H.264 to MJPEG conversion (camera passthrough)
	TranscodeInit(width, height int) error
	TranscodeStart() error
	TranscodeStop()
	TranscodeShutdown()
	TranscodeSendH264(frame []byte) error
	TranscodeIsRunning() bool
}

// Manager handles all camera and UVC functionality.
// Now uses H.264 directly for both HDMI loopback and camera passthrough.
type Manager struct {
	// Dependencies
	gadget GadgetController
	native NativeController
	uvcLog *zerolog.Logger
	camLog *zerolog.Logger

	// UVC streaming state
	streamer     *usbgadget.UVCStreamer
	streamerMu   sync.Mutex
	eventLoopRun bool
	stopChan     chan struct{}

	// H.264 parameter cache for SPS/PPS injection
	h264Cache *H264ParamCache

	// Camera passthrough state
	enabled      atomic.Bool
	source       *sourceStore
	frameCount   int
	lastLogFrame int
	frameMu      sync.Mutex

	// UVC frame stats
	uvcFrameCount       int
	uvcFrameErrors      int
	uvcLastLogFrame     int
	uvcNeedParamInject  bool // Flag to inject SPS/PPS on next frame
	uvcParamInjectCount int  // Counter for periodic SPS/PPS injection

	// MJPEG format tracking
	uvcMjpegSelected bool // True when host selected MJPEG format

	// Format change notification channel for WebSocket clients
	formatChangeChan   chan FormatInfo
	formatChanMu       sync.RWMutex
	lastNotifiedFormat FormatInfo // Track last notified format to avoid duplicates
}

// FormatInfo contains the current UVC format information.
type FormatInfo struct {
	Codec  string `json:"codec"` // "h264" or "mjpeg"
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// Config holds configuration for the Manager.
type Config struct {
	UVCLogger    *zerolog.Logger
	CameraLogger *zerolog.Logger
	Gadget       GadgetController
	Native       NativeController
}

// NewManager creates a new camera Manager.
func NewManager(cfg Config) *Manager {
	m := &Manager{
		gadget:    cfg.Gadget,
		native:    cfg.Native,
		uvcLog:    cfg.UVCLogger,
		camLog:    cfg.CameraLogger,
		source:    newSourceStore(),
		h264Cache: NewH264ParamCache(),
	}
	return m
}

// SetEnabled enables or disables camera passthrough.
func (m *Manager) SetEnabled(enabled bool) {
	m.enabled.Store(enabled)
	if m.camLog != nil {
		m.camLog.Info().Bool("enabled", enabled).Msg("Camera passthrough state changed")
	}
}

// IsEnabled returns whether camera passthrough is enabled.
func (m *Manager) IsEnabled() bool {
	return m.enabled.Load()
}

// SetSource sets the video source for UVC output.
func (m *Manager) SetSource(source Source) {
	oldSource := m.source.Get()
	m.source.Set(source)

	if m.camLog != nil {
		m.camLog.Info().
			Str("old_source", oldSource.String()).
			Str("new_source", source.String()).
			Msg("UVC source changed")
	}
}

// GetSource returns the current UVC video source.
func (m *Manager) GetSource() Source {
	return m.source.Get()
}

// IsSourceCamera returns true if UVC source is browser camera.
func (m *Manager) IsSourceCamera() bool {
	return m.source.IsCamera()
}

// IsSourceHDMI returns true if UVC source is HDMI.
func (m *Manager) IsSourceHDMI() bool {
	return m.source.IsHDMI()
}

// SetNativeController sets the native controller for MJPEG encoding control.
// This should be called after native is initialized.
func (m *Manager) SetNativeController(native NativeController) {
	m.streamerMu.Lock()
	defer m.streamerMu.Unlock()
	m.native = native
}

// GetCurrentFormat returns the current UVC format info.
// Returns nil if UVC is not streaming.
func (m *Manager) GetCurrentFormat() *FormatInfo {
	m.streamerMu.Lock()
	defer m.streamerMu.Unlock()

	if m.streamer == nil || !m.streamer.IsStreaming() {
		return nil
	}

	width, height := m.streamer.GetCommittedResolution()
	codec := "h264"
	if m.uvcMjpegSelected {
		codec = "mjpeg"
	}

	return &FormatInfo{
		Codec:  codec,
		Width:  int(width),
		Height: int(height),
	}
}

// SubscribeFormatChanges returns a channel that receives format change notifications.
// Call UnsubscribeFormatChanges when done.
func (m *Manager) SubscribeFormatChanges() <-chan FormatInfo {
	m.formatChanMu.Lock()
	defer m.formatChanMu.Unlock()

	// Create buffered channel to avoid blocking
	m.formatChangeChan = make(chan FormatInfo, 4)
	return m.formatChangeChan
}

// UnsubscribeFormatChanges closes the format change subscription.
func (m *Manager) UnsubscribeFormatChanges() {
	m.formatChanMu.Lock()
	defer m.formatChanMu.Unlock()

	if m.formatChangeChan != nil {
		close(m.formatChangeChan)
		m.formatChangeChan = nil
	}
}

// notifyFormatChange sends format info to subscribed WebSocket clients.
// Only sends if format actually changed from last notification.
func (m *Manager) notifyFormatChange(info FormatInfo) {
	m.formatChanMu.Lock()
	// Check if format changed
	if m.lastNotifiedFormat.Codec == info.Codec &&
		m.lastNotifiedFormat.Width == info.Width &&
		m.lastNotifiedFormat.Height == info.Height {
		m.formatChanMu.Unlock()
		return // No change, skip notification
	}
	m.lastNotifiedFormat = info
	ch := m.formatChangeChan
	m.formatChanMu.Unlock()

	if ch != nil {
		select {
		case ch <- info:
		default:
			// Channel full, skip notification
		}
	}
}

// notifyStreamingStopped sends a stop notification to WebSocket clients.
// This tells the browser to pause camera encoding when UVC host disconnects.
func (m *Manager) notifyStreamingStopped() {
	m.formatChanMu.Lock()
	// Reset last notified format so next start will send format again
	m.lastNotifiedFormat = FormatInfo{}
	ch := m.formatChangeChan
	m.formatChanMu.Unlock()

	// Send empty format to signal stop
	if ch != nil {
		select {
		case ch <- FormatInfo{Codec: "stop"}:
		default:
			// Channel full, skip notification
		}
	}
}
