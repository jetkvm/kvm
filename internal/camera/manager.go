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

type Manager struct {
	gadget           GadgetController
	native           NativeController
	uvcLog           *zerolog.Logger
	camLog           *zerolog.Logger
	streamer         *usbgadget.UVCStreamer
	streamerMu       sync.Mutex
	stopChan         chan struct{}
	eventLoopRun     bool
	uvcMjpegSelected bool
	h264Cache        *H264ParamCache
	enabled          atomic.Bool
	source           *sourceStore

	cameraFrameCount    atomic.Int32
	cameraLastLogFrame  atomic.Int32
	uvcFrameErrors      atomic.Uint32
	uvcNeedParamInject  atomic.Bool
	uvcParamInjectCount atomic.Uint32
	uvcStreamingFast    atomic.Bool
	uvcMjpegFast        atomic.Bool

	formatChangeChan   chan FormatInfo
	formatChanMu       sync.RWMutex
	lastNotifiedFormat FormatInfo
}

type FormatInfo struct {
	Codec     string `json:"codec"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	FrameRate int    `json:"frameRate"`
}

type Config struct {
	UVCLogger    *zerolog.Logger
	CameraLogger *zerolog.Logger
	Gadget       GadgetController
	Native       NativeController
}

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

func (m *Manager) SetEnabled(enabled bool) {
	m.enabled.Store(enabled)
	if m.camLog != nil {
		m.camLog.Info().Bool("enabled", enabled).Msg("Camera passthrough state changed")
	}
}

func (m *Manager) IsEnabled() bool { return m.enabled.Load() }

func (m *Manager) SetSource(source Source) {
	oldSource := m.source.Get()
	if oldSource == source {
		return
	}
	m.source.Set(source)
	if m.camLog != nil {
		m.camLog.Debug().
			Str("old_source", oldSource.String()).
			Str("new_source", source.String()).
			Msg("UVC source changed")
	}
	m.updateMjpegEncoderForSource(source)
	m.notifySourceChange(source)
}

func (m *Manager) updateMjpegEncoderForSource(source Source) {
	m.streamerMu.Lock()
	isStreaming := m.streamer != nil && m.streamer.IsStreaming()
	isMjpeg := m.uvcMjpegSelected
	m.streamerMu.Unlock()

	if m.native == nil {
		return
	}

	if source == SourceHDMI && isStreaming && isMjpeg {
		m.native.MjpegSetEnabled(true)
	} else {
		m.native.MjpegSetEnabled(false)
	}
}

func (m *Manager) GetSource() Source    { return m.source.Get() }
func (m *Manager) IsSourceCamera() bool { return m.source.IsCamera() }
func (m *Manager) IsSourceHDMI() bool   { return m.source.IsHDMI() }

func (m *Manager) SetNativeController(native NativeController) {
	m.streamerMu.Lock()
	defer m.streamerMu.Unlock()
	m.native = native
}

func (m *Manager) GetCurrentFormat() *FormatInfo {
	return m.getStreamingFormat()
}

func (m *Manager) SubscribeFormatChanges() <-chan FormatInfo {
	m.formatChanMu.Lock()
	defer m.formatChanMu.Unlock()
	m.formatChangeChan = make(chan FormatInfo, 4)
	return m.formatChangeChan
}

func (m *Manager) UnsubscribeFormatChanges() {
	m.formatChanMu.Lock()
	defer m.formatChanMu.Unlock()
	if m.formatChangeChan != nil {
		close(m.formatChangeChan)
		m.formatChangeChan = nil
	}
}

func (m *Manager) notifyFormatChange(info FormatInfo) {
	m.formatChanMu.Lock()
	if m.lastNotifiedFormat.Codec == info.Codec &&
		m.lastNotifiedFormat.Width == info.Width &&
		m.lastNotifiedFormat.Height == info.Height &&
		m.lastNotifiedFormat.FrameRate == info.FrameRate {
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
		}
	}
}

func (m *Manager) notifyStreamingStopped() {
	m.formatChanMu.Lock()
	m.lastNotifiedFormat = FormatInfo{}
	ch := m.formatChangeChan
	m.formatChanMu.Unlock()

	if ch != nil {
		select {
		case ch <- FormatInfo{Codec: "stop"}:
		default:
		}
	}
}

// getStreamingFormat returns the current streaming format info, or nil if not streaming.
// Caller must not hold streamerMu.
func (m *Manager) getStreamingFormat() *FormatInfo {
	m.streamerMu.Lock()
	defer m.streamerMu.Unlock()

	if m.streamer == nil || !m.streamer.IsStreaming() {
		return nil
	}

	width, height := m.streamer.GetCommittedResolution()
	frameRate := m.streamer.GetCommittedFrameRate()
	codec := "h264"
	if m.uvcMjpegSelected {
		codec = "mjpeg"
	}

	return &FormatInfo{
		Codec:     codec,
		Width:     int(width),
		Height:    int(height),
		FrameRate: frameRate,
	}
}

func (m *Manager) ResendCurrentFormat() {
	format := m.getStreamingFormat()
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

func (m *Manager) notifySourceChange(source Source) {
	format := m.getStreamingFormat()
	if format == nil {
		return
	}

	switch source {
	case SourceHDMI:
		if m.camLog != nil {
			m.camLog.Info().Msg("Notifying browser to stop camera encoding")
		}
		m.notifyStreamingStopped()
	case SourceCamera:
		if m.camLog != nil {
			m.camLog.Info().
				Str("codec", format.Codec).
				Int("width", format.Width).
				Int("height", format.Height).
				Int("frameRate", format.FrameRate).
				Msg("Notifying browser to start camera encoding (switched to Camera)")
		}
		m.notifyFormatChange(*format)
	}
}
