package native

import (
	"os"
	"sync"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/jetkvm/kvm/internal/diagnostics"
	"github.com/rs/zerolog"
)

type Native struct {
	ready                chan struct{}
	l                    *zerolog.Logger
	lD                   *zerolog.Logger
	systemVersion        *semver.Version
	appVersion           *semver.Version
	displayRotation      uint16
	defaultQualityFactor float64
	onVideoStateChange   func(state VideoState)
	onVideoFrameReceived func(frame []byte, duration time.Duration)
	onIndevEvent         func(event string)
	onRpcEvent           func(event string)
	sleepModeSupported   bool
	videoLock            sync.Mutex
	screenLock           sync.Mutex
	extraLock            sync.Mutex
}

type NativeOptions struct {
	SystemVersion        *semver.Version
	AppVersion           *semver.Version
	DisplayRotation      uint16
	DefaultQualityFactor float64
	MaxRestartAttempts   uint
	OnVideoStateChange   func(state VideoState)
	OnVideoFrameReceived func(frame []byte, duration time.Duration)
	OnJpegFrameReceived  func(frame []byte)
	OnIndevEvent         func(event string)
	OnRpcEvent           func(event string)
	OnNativeRestart      func()
	// GetSessionInfo returns session diagnostics for crash logging.
	GetSessionInfo func() diagnostics.SessionInfo
}

type VideoStreamingStatus uint8

const (
	VideoStreamingStatusActive   VideoStreamingStatus = 1
	VideoStreamingStatusStopping VideoStreamingStatus = 2 // video is stopping, but not yet stopped
	VideoStreamingStatusInactive VideoStreamingStatus = 0
)

func (s VideoStreamingStatus) String() string {
	switch s {
	case VideoStreamingStatusActive:
		return "active"
	case VideoStreamingStatusStopping:
		return "stopping"
	case VideoStreamingStatusInactive:
		return "inactive"
	}
	return "unknown"
}

func NewNative(opts NativeOptions) *Native {
	pid := os.Getpid()
	nativeSubLogger := nativeLogger.With().Int("pid", pid).Str("scope", "native").Logger()
	displaySubLogger := displayLogger.With().Int("pid", pid).Str("scope", "native").Logger()

	onVideoStateChange := opts.OnVideoStateChange
	if onVideoStateChange == nil {
		onVideoStateChange = func(state VideoState) {
			nativeLogger.Info().Interface("state", state).Msg("video state changed")
		}
	}

	onVideoFrameReceived := opts.OnVideoFrameReceived
	if onVideoFrameReceived == nil {
		onVideoFrameReceived = func(frame []byte, duration time.Duration) {
			nativeLogger.Trace().Interface("frame", frame).Dur("duration", duration).Msg("video frame received")
		}
	}

	onIndevEvent := opts.OnIndevEvent
	if onIndevEvent == nil {
		onIndevEvent = func(event string) {
			nativeLogger.Info().Str("event", event).Msg("indev event")
		}
	}

	onRpcEvent := opts.OnRpcEvent
	if onRpcEvent == nil {
		onRpcEvent = func(event string) {
			nativeLogger.Info().Str("event", event).Msg("rpc event")
		}
	}

	sleepModeSupported := isSleepModeSupported()

	defaultQualityFactor := opts.DefaultQualityFactor
	if defaultQualityFactor <= 0 || defaultQualityFactor > 1 {
		defaultQualityFactor = 1.0
	}

	return &Native{
		ready:                make(chan struct{}),
		l:                    &nativeSubLogger,
		lD:                   &displaySubLogger,
		systemVersion:        opts.SystemVersion,
		appVersion:           opts.AppVersion,
		displayRotation:      opts.DisplayRotation,
		defaultQualityFactor: defaultQualityFactor,
		onVideoStateChange:   onVideoStateChange,
		onVideoFrameReceived: onVideoFrameReceived,
		onIndevEvent:         onIndevEvent,
		onRpcEvent:           onRpcEvent,
		sleepModeSupported:   sleepModeSupported,
		videoLock:            sync.Mutex{},
		screenLock:           sync.Mutex{},
	}
}

func (n *Native) Start() error {
	// set up singleton
	setInstance(n)
	setUpNativeHandlers()

	// start the native video
	go n.handleLogChan()
	go n.handleVideoStateChan()
	go n.handleVideoFrameChan()
	go n.handleIndevEventChan()
	go n.handleRpcEventChan()

	n.initUI()
	go n.tickUI()

	if err := videoInit(n.defaultQualityFactor); err != nil {
		n.l.Error().Err(err).Msg("failed to initialize video")
		return err
	}

	close(n.ready)
	return nil
}

// DoNotUseThisIsForCrashTestingOnly
// will crash the program in cgo code
func (n *Native) DoNotUseThisIsForCrashTestingOnly() {
	defer func() {
		if r := recover(); r != nil {
			n.l.Error().Msg("recovered from crash")
		}
	}()

	crash()
}

// GetLVGLVersion returns the LVGL version
func GetLVGLVersion() string {
	return uiGetLVGLVersion()
}

// JPEG encoder public API

// StartJPEGEncoder starts the hardware JPEG encoder with the given quality (1-99)
func StartJPEGEncoder(quality int) error {
	return jpegStart(quality)
}

// StopJPEGEncoder stops the hardware JPEG encoder
func StopJPEGEncoder() {
	jpegStop()
}

// SetJPEGQuality sets the JPEG encoder quality (1-99)
func SetJPEGQuality(quality int) error {
	return jpegSetQuality(quality)
}

// GetJPEGQuality returns the current JPEG encoder quality
func GetJPEGQuality() int {
	return jpegGetQuality()
}

// IsJPEGEncoderRunning returns true if the JPEG encoder is running
func IsJPEGEncoderRunning() bool {
	return jpegIsRunning()
}

// GetJPEGFrameChannel returns the channel for receiving JPEG frames
func GetJPEGFrameChannel() <-chan []byte {
	return jpegFrameChan
}

// Native instance methods that implement NativeInterface

// JpegStart starts the hardware JPEG encoder with the given quality (1-99)
func (n *Native) JpegStart(quality int) error {
	return jpegStart(quality)
}

// JpegStop stops the hardware JPEG encoder
func (n *Native) JpegStop() error {
	jpegStop()
	return nil
}

// JpegSetQuality sets the JPEG encoder quality (1-99)
func (n *Native) JpegSetQuality(quality int) error {
	return jpegSetQuality(quality)
}

// JpegGetQuality returns the current JPEG encoder quality
func (n *Native) JpegGetQuality() (int, error) {
	return jpegGetQuality(), nil
}

// JpegIsRunning returns true if the JPEG encoder is running
func (n *Native) JpegIsRunning() (bool, error) {
	return jpegIsRunning(), nil
}

// VideoRequestKeyframe requests an IDR (keyframe) from the H.264 encoder
func (n *Native) VideoRequestKeyframe() error {
	return videoRequestKeyframe()
}
