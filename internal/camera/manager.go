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

// PanicHandler is called when the UVC event loop panics and recovers.
// The handler receives the panic value for logging/alerting purposes.
type PanicHandler func(panicValue interface{})

// Manager coordinates camera passthrough from browser to UVC.
//
// Thread-safety:
//   - enabled, uvcStreamingFast, uvcMjpegFast: atomic, lock-free access for hot paths
//   - streamer: atomic pointer, lock-free load for frame dispatch
//   - streamerMu: protects streamer creation/destruction and streaming state changes
//   - eventLoopRun: atomic, controls event loop lifecycle
//   - formatChanMu: protects formatChangeChan and lastNotifiedFormat; held during non-blocking sends
//   - onPanic: set once during configuration, read-only during operation
type Manager struct {
	gadget       GadgetController
	uvcLog       *zerolog.Logger
	camLog       *zerolog.Logger
	streamer     atomic.Pointer[usbgadget.UVCStreamer]
	streamerMu   sync.Mutex
	eventLoopRun atomic.Bool
	enabled      atomic.Bool

	uvcStreamingFast atomic.Bool
	uvcMjpegFast     atomic.Bool

	formatChangeChan   chan FormatInfo
	formatChanMu       sync.RWMutex
	lastNotifiedFormat FormatInfo

	uvcFrameErrors atomic.Uint32

	// Frame drop counters for observability (atomic for lock-free hot path access)
	droppedStateFrames atomic.Uint64 // Frames dropped due to state mismatch (not streaming, disabled, wrong codec)
	droppedWriteFrames atomic.Uint64 // Frames dropped due to V4L2 write failures

	onPanic PanicHandler
}

// VideoCodec represents a video codec type with validation.
type VideoCodec string

// Valid video codec constants.
const (
	CodecH264  VideoCodec = "h264"
	CodecMJPEG VideoCodec = "mjpeg"
	CodecStop  VideoCodec = "stop"
)

// Wire protocol codec bytes for binary frame headers.
// Used by camera_ws.go (via import) and must match values in ui/src/lib/cameraTransport.ts.
const (
	CodecByteH264  byte = 0x01
	CodecByteMJPEG byte = 0x02
)

// MaxFrameRate is the maximum supported frame rate (240fps).
// This limit balances hardware capabilities with practical UVC bandwidth constraints.
const MaxFrameRate = 240

// IsValid returns true if the codec is a recognized value.
func (c VideoCodec) IsValid() bool {
	switch c {
	case CodecH264, CodecMJPEG, CodecStop:
		return true
	default:
		return false
	}
}
func (c VideoCodec) String() string {
	return string(c)
}

// ToByte returns the wire protocol byte for this codec.
// Returns 0 for CodecStop since it has no wire representation.
// Panics on invalid codec values to fail fast during development.
func (c VideoCodec) ToByte() byte {
	switch c {
	case CodecH264:
		return CodecByteH264
	case CodecMJPEG:
		return CodecByteMJPEG
	case CodecStop:
		return 0
	default:
		panic("camera: ToByte called on invalid VideoCodec: " + string(c))
	}
}

// FormatInfo describes the video format requested by the UVC host.
type FormatInfo struct {
	Codec     VideoCodec `json:"codec"`
	Width     int        `json:"width"`
	Height    int        `json:"height"`
	FrameRate int        `json:"frameRate"`
}

// StopFormat returns a FormatInfo signaling that streaming has stopped.
// This is the canonical way to create a stop notification.
func StopFormat() FormatInfo {
	return FormatInfo{Codec: CodecStop}
}

// FormatInfo validation errors.
var (
	ErrInvalidCodec     = errors.New("camera: invalid video codec")
	ErrInvalidWidth     = errors.New("camera: width must be positive")
	ErrInvalidHeight    = errors.New("camera: height must be positive")
	ErrInvalidFrameRate = errors.New("camera: frame rate must be 1-240") // See MaxFrameRate
)

// Validate checks that FormatInfo contains valid values.
// Stop formats are always valid. For streaming formats, validates:
// - Codec is h264 or mjpeg (not stop or invalid)
// - Width and Height are positive
// - FrameRate is between 1 and 240
func (f FormatInfo) Validate() error {
	// Stop format is always valid (no dimension/rate requirements)
	if f.Codec == CodecStop {
		return nil
	}

	// For streaming formats, codec must be valid (h264 or mjpeg)
	if !f.Codec.IsValid() {
		return ErrInvalidCodec
	}
	if f.Width <= 0 {
		return ErrInvalidWidth
	}
	if f.Height <= 0 {
		return ErrInvalidHeight
	}
	if f.FrameRate < 1 || f.FrameRate > MaxFrameRate {
		return ErrInvalidFrameRate
	}
	return nil
}

// NewFormatInfo creates a validated FormatInfo for streaming.
// Use StopFormat() for stop notifications instead of this constructor.
// Returns error if any parameter is invalid.
func NewFormatInfo(codec VideoCodec, width, height, frameRate int) (FormatInfo, error) {
	// CodecStop is not valid for streaming formats - use StopFormat() instead
	if codec == CodecStop {
		return FormatInfo{}, ErrInvalidCodec
	}

	f := FormatInfo{
		Codec:     codec,
		Width:     width,
		Height:    height,
		FrameRate: frameRate,
	}

	// Reuse Validate() to avoid duplicating validation logic
	if err := f.Validate(); err != nil {
		return FormatInfo{}, err
	}

	return f, nil
}

// Config holds configuration for creating a Manager.
type Config struct {
	UVCLogger    *zerolog.Logger
	CameraLogger *zerolog.Logger
	Gadget       GadgetController
	OnPanic      PanicHandler // Optional: called when event loop panics and recovers
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
		gadget:  cfg.Gadget,
		uvcLog:  cfg.UVCLogger,
		camLog:  cfg.CameraLogger,
		onPanic: cfg.OnPanic,
	}, nil
}

// SetEnabled enables or disables camera passthrough.
func (m *Manager) SetEnabled(enabled bool) {
	m.enabled.Store(enabled)
	if m.camLog != nil {
		m.camLog.Info().Bool("enabled", enabled).Msg("Camera passthrough state changed")
	}
}
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

	// Use constructor to ensure format validity (should always succeed with UVC values)
	info, err := NewFormatInfo(codec, int(width), int(height), frameRate)
	if err != nil {
		// UVC-negotiated values should always be valid; log if they aren't
		if m.camLog != nil {
			m.camLog.Error().Err(err).
				Str("codec", string(codec)).
				Int("width", int(width)).
				Int("height", int(height)).
				Int("frameRate", frameRate).
				Msg("GetCurrentFormat: unexpected invalid format")
		}
		return nil
	}
	return &info
}

// SubscribeFormatChanges returns a channel for format notifications.
// Only one subscriber is supported; calling again replaces the previous subscription.
//
// IMPORTANT: If an existing subscription is active, this will close the old channel
// before creating a new one. Callers must ensure the old subscriber has stopped
// reading before calling SubscribeFormatChanges again (typically via UnsubscribeFormatChanges).
//
// Buffer size 4 allows bursty format changes (e.g., rapid USB reconnects) without
// blocking the UVC event loop while the WebSocket goroutine processes them.
func (m *Manager) SubscribeFormatChanges() <-chan FormatInfo {
	m.formatChanMu.Lock()
	defer m.formatChanMu.Unlock()

	// Close old channel if exists - caller should have called UnsubscribeFormatChanges first
	if m.formatChangeChan != nil {
		if m.camLog != nil {
			m.camLog.Warn().Msg("SubscribeFormatChanges: replacing existing subscription (old channel closed)")
		}
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
		case m.formatChangeChan <- StopFormat():
		default:
			if m.camLog != nil {
				m.camLog.Warn().Msg("Stop notification dropped - channel full")
			}
		}
	}
}

// FrameStats provides frame processing statistics for monitoring.
type FrameStats struct {
	DroppedStateFrames uint64 // Frames dropped due to state mismatch
	DroppedWriteFrames uint64 // Frames dropped due to V4L2 write failures
	WriteErrors        uint32 // Total V4L2 write errors (throttled in logs)
}

// GetFrameStats returns current frame processing statistics.
// Use this for monitoring dashboards and debugging frame drop issues.
func (m *Manager) GetFrameStats() FrameStats {
	return FrameStats{
		DroppedStateFrames: m.droppedStateFrames.Load(),
		DroppedWriteFrames: m.droppedWriteFrames.Load(),
		WriteErrors:        m.uvcFrameErrors.Load(),
	}
}

// ResetFrameStats resets all frame statistics counters.
// Typically called when starting a new streaming session.
func (m *Manager) ResetFrameStats() {
	m.droppedStateFrames.Store(0)
	m.droppedWriteFrames.Store(0)
	m.uvcFrameErrors.Store(0)
}

// ResendCurrentFormat resends the current format to the browser.
func (m *Manager) ResendCurrentFormat() {
	format := m.GetCurrentFormat()
	if format == nil {
		return
	}

	if m.camLog != nil {
		m.camLog.Info().
			Str("codec", format.Codec.String()).
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
