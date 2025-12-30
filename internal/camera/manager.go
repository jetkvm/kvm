package camera

import (
	"sync"
	"sync/atomic"

	"github.com/jetkvm/kvm/internal/usbgadget"
	"github.com/rs/zerolog"
)

// GadgetController provides access to the USB gadget UVC device.
type GadgetController interface {
	GetUVCVideoDevice() (string, error)
}

// Manager coordinates camera passthrough from browser to UVC.
type Manager struct {
	gadget       GadgetController
	uvcLog       *zerolog.Logger
	camLog       *zerolog.Logger
	streamer     atomic.Pointer[usbgadget.UVCStreamer]
	streamerMu   sync.Mutex
	stopChan     chan struct{}
	eventLoopRun atomic.Bool
	enabled      atomic.Bool

	// Fast-path flags: Cached streaming state to avoid mutex/pointer loads on every frame.
	// uvcStreamingFast mirrors streamer.IsStreaming() for lock-free hot path rejection.
	// uvcMjpegFast is true when streaming MJPEG (codec selection for incoming frames).
	uvcStreamingFast atomic.Bool
	uvcMjpegFast     atomic.Bool

	// Format negotiation
	formatChangeChan   chan FormatInfo
	formatChanMu       sync.RWMutex
	lastNotifiedFormat FormatInfo

	// Stats (only used for periodic logging)
	uvcFrameErrors atomic.Uint32
}

// Codec constants for FormatInfo
const (
	CodecH264  = "h264"
	CodecMJPEG = "mjpeg"
	CodecStop  = "stop"
)

// FormatInfo describes the video format requested by the UVC host.
type FormatInfo struct {
	Codec     string `json:"codec"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	FrameRate int    `json:"frameRate"`
}

// Config holds configuration for creating a Manager.
type Config struct {
	UVCLogger    *zerolog.Logger
	CameraLogger *zerolog.Logger
	Gadget       GadgetController
}

// NewManager creates a new camera manager.
func NewManager(cfg Config) *Manager {
	return &Manager{
		gadget: cfg.Gadget,
		uvcLog: cfg.UVCLogger,
		camLog: cfg.CameraLogger,
	}
}

// SetEnabled enables or disables camera passthrough.
func (m *Manager) SetEnabled(enabled bool) {
	m.enabled.Store(enabled)
	if m.camLog != nil {
		m.camLog.Info().Bool("enabled", enabled).Msg("Camera passthrough state changed")
	}
}

// IsEnabled returns true if camera passthrough is enabled.
func (m *Manager) IsEnabled() bool { return m.enabled.Load() }

// GetCurrentFormat returns the current streaming format, or nil if not streaming.
func (m *Manager) GetCurrentFormat() *FormatInfo {
	streamer := m.streamer.Load()
	if streamer == nil || !streamer.IsStreaming() {
		return nil
	}

	width, height := streamer.GetCommittedResolution()
	frameRate := streamer.GetCommittedFrameRate()
	codec := CodecH264
	if m.uvcMjpegFast.Load() {
		codec = CodecMJPEG
	}

	return &FormatInfo{
		Codec:     codec,
		Width:     int(width),
		Height:    int(height),
		FrameRate: frameRate,
	}
}

// SubscribeFormatChanges returns a channel for format notifications.
func (m *Manager) SubscribeFormatChanges() <-chan FormatInfo {
	m.formatChanMu.Lock()
	defer m.formatChanMu.Unlock()
	if m.formatChangeChan != nil {
		close(m.formatChangeChan)
	}
	m.formatChangeChan = make(chan FormatInfo, 4)
	return m.formatChangeChan
}

// UnsubscribeFormatChanges closes the format notification channel.
func (m *Manager) UnsubscribeFormatChanges() {
	m.formatChanMu.Lock()
	defer m.formatChanMu.Unlock()
	if m.formatChangeChan != nil {
		close(m.formatChangeChan)
		m.formatChangeChan = nil
	}
}

// notifyFormatChange sends a format change notification if it differs from last.
func (m *Manager) notifyFormatChange(info FormatInfo) {
	m.formatChanMu.Lock()
	if m.lastNotifiedFormat == info {
		m.formatChanMu.Unlock()
		return
	}
	m.lastNotifiedFormat = info
	ch := m.formatChangeChan
	m.formatChanMu.Unlock()

	if ch != nil {
		select {
		case ch <- info:
		default:
			if m.camLog != nil {
				m.camLog.Warn().Msg("Format notification dropped - channel full")
			}
		}
	}
}

// notifyStreamingStopped notifies that streaming has stopped.
func (m *Manager) notifyStreamingStopped() {
	m.formatChanMu.Lock()
	m.lastNotifiedFormat = FormatInfo{}
	ch := m.formatChangeChan
	m.formatChanMu.Unlock()

	if ch != nil {
		select {
		case ch <- FormatInfo{Codec: CodecStop}:
		default:
			if m.camLog != nil {
				m.camLog.Warn().Msg("Stop notification dropped - channel full")
			}
		}
	}
}

// ResendCurrentFormat resends the current format to the browser.
func (m *Manager) ResendCurrentFormat() {
	format := m.GetCurrentFormat()
	if format == nil {
		return
	}

	if m.camLog != nil {
		m.camLog.Info().
			Str("codec", format.Codec).
			Int("width", format.Width).
			Int("height", format.Height).
			Int("frameRate", format.FrameRate).
			Msg("Resending format notification")
	}

	m.formatChanMu.Lock()
	m.lastNotifiedFormat = FormatInfo{}
	m.formatChanMu.Unlock()

	m.notifyFormatChange(*format)
}
