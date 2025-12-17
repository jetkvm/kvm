package camera

import (
	"sync"
	"sync/atomic"

	"github.com/jetkvm/kvm/internal/usbgadget"
	"github.com/rs/zerolog"
)

// NativeController interface for controlling native MJPEG encoder.
type NativeController interface {
	MjpegSetEnabled(enabled bool)
}

// GadgetController interface for USB gadget operations.
type GadgetController interface {
	GetUVCVideoDevice() (string, error)
}

// Manager handles all camera and UVC functionality.
type Manager struct {
	// Dependencies
	gadget  GadgetController
	native  NativeController
	uvcLog  *zerolog.Logger
	camLog  *zerolog.Logger

	// UVC streaming state
	streamer        *usbgadget.UVCStreamer
	streamerMu      sync.Mutex
	eventLoopRun    bool
	stopChan        chan struct{}

	// Camera passthrough state
	enabled         atomic.Bool
	source          *sourceStore
	frameCount      int
	lastLogFrame    int
	frameMu         sync.Mutex

	// UVC frame stats
	uvcFrameCount   int
	uvcFrameErrors  int
	uvcLastLogFrame int
}

// Config holds configuration for the Manager.
type Config struct {
	UVCLogger       *zerolog.Logger
	CameraLogger    *zerolog.Logger
	Gadget          GadgetController
	Native          NativeController
}

// NewManager creates a new camera Manager.
func NewManager(cfg Config) *Manager {
	m := &Manager{
		gadget:  cfg.Gadget,
		native:  cfg.Native,
		uvcLog:  cfg.UVCLogger,
		camLog:  cfg.CameraLogger,
		source:  newSourceStore(),
	}
	return m
}

// SetNativeController updates the native controller (may be set after init).
func (m *Manager) SetNativeController(native NativeController) {
	m.native = native
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

	// Enable/disable native MJPEG encoder based on source
	if m.native != nil {
		if source == SourceHDMI {
			m.native.MjpegSetEnabled(true)
		} else {
			m.native.MjpegSetEnabled(false)
		}
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
