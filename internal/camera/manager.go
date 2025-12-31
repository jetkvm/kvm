package camera

import (
	"errors"
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
//
// Thread-safety:
//   - enabled, uvcStreamingFast, uvcMjpegFast: atomic, lock-free access for hot paths
//   - streamer: atomic pointer, lock-free load for frame dispatch
//   - streamerMu: protects streamer creation/destruction and streaming state changes
//   - formatChanMu: protects format notification channel and lastNotifiedFormat
type Manager struct {
	gadget       GadgetController
	uvcLog       *zerolog.Logger
	camLog       *zerolog.Logger
	streamer     atomic.Pointer[usbgadget.UVCStreamer]
	streamerMu   sync.Mutex
	stopChan     chan struct{}
	eventLoopRun atomic.Bool
	enabled      atomic.Bool

	uvcStreamingFast atomic.Bool
	uvcMjpegFast     atomic.Bool

	formatChangeChan   chan FormatInfo
	formatChanMu       sync.RWMutex
	lastNotifiedFormat FormatInfo

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

// ErrNilGadget is returned when Config.Gadget is nil.
var ErrNilGadget = errors.New("camera: gadget controller is required")

// NewManager creates a new camera manager.
// Returns error if cfg.Gadget is nil.
func NewManager(cfg Config) (*Manager, error) {
	if cfg.Gadget == nil {
		return nil, ErrNilGadget
	}
	return &Manager{
		gadget: cfg.Gadget,
		uvcLog: cfg.UVCLogger,
		camLog: cfg.CameraLogger,
	}, nil
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
// Only one subscriber is supported; calling again replaces the previous subscription.
// The old channel is not closed to avoid races with concurrent sends.
func (m *Manager) SubscribeFormatChanges() <-chan FormatInfo {
	m.formatChanMu.Lock()
	defer m.formatChanMu.Unlock()
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
// The lock is held during send (non-blocking) to prevent send-to-closed-channel panic.
func (m *Manager) notifyFormatChange(info FormatInfo) {
	m.formatChanMu.Lock()
	defer m.formatChanMu.Unlock()

	if m.lastNotifiedFormat == info {
		return
	}
	m.lastNotifiedFormat = info

	if m.formatChangeChan != nil {
		select {
		case m.formatChangeChan <- info:
		default:
			if m.camLog != nil {
				m.camLog.Warn().Msg("Format notification dropped - channel full")
			}
		}
	}
}

// notifyStreamingStopped notifies that streaming has stopped.
// The lock is held during send (non-blocking) to prevent send-to-closed-channel panic.
func (m *Manager) notifyStreamingStopped() {
	m.formatChanMu.Lock()
	defer m.formatChanMu.Unlock()

	m.lastNotifiedFormat = FormatInfo{}

	if m.formatChangeChan != nil {
		select {
		case m.formatChangeChan <- FormatInfo{Codec: CodecStop}:
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
