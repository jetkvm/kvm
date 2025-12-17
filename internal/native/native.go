package native

import (
	"os"
	"sync"
	"time"

	"github.com/Masterminds/semver/v3"
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
	onMjpegFrameReceived func(frame []byte) // MJPEG frames for UVC streaming
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
	OnMjpegFrameReceived func(frame []byte) // MJPEG frames for UVC streaming
	OnIndevEvent         func(event string)
	OnRpcEvent           func(event string)
	OnNativeRestart      func()
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

	// MJPEG callback is optional - nil means UVC streaming disabled
	onMjpegFrameReceived := opts.OnMjpegFrameReceived

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
		onMjpegFrameReceived: onMjpegFrameReceived,
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
	go n.handleMjpegFrameChan()
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

// MjpegSetEnabled enables or disables MJPEG encoding for UVC streaming
func (n *Native) MjpegSetEnabled(enabled bool) {
	n.videoLock.Lock()
	defer n.videoLock.Unlock()
	mjpegSetEnabled(enabled)
}

// MjpegGetEnabled returns whether MJPEG encoding is enabled
func (n *Native) MjpegGetEnabled() bool {
	n.videoLock.Lock()
	defer n.videoLock.Unlock()
	return mjpegGetEnabled()
}

// MjpegSetFrameDivisor sets the frame rate divisor for MJPEG encoding
// divisor=1 means full fps, divisor=2 means half fps (30fps from 60fps input), etc.
func (n *Native) MjpegSetFrameDivisor(divisor int) {
	n.videoLock.Lock()
	defer n.videoLock.Unlock()
	mjpegSetFrameDivisor(divisor)
}

// MjpegGetFrameDivisor returns the current frame rate divisor
func (n *Native) MjpegGetFrameDivisor() int {
	n.videoLock.Lock()
	defer n.videoLock.Unlock()
	return mjpegGetFrameDivisor()
}

// MjpegSetQuality sets the MJPEG encoding quality (0.1 to 1.0)
// Higher values mean better quality but larger file sizes
func (n *Native) MjpegSetQuality(quality float32) {
	n.videoLock.Lock()
	defer n.videoLock.Unlock()
	mjpegSetQuality(quality)
}

// MjpegGetQuality returns the current MJPEG encoding quality
func (n *Native) MjpegGetQuality() float32 {
	n.videoLock.Lock()
	defer n.videoLock.Unlock()
	return mjpegGetQuality()
}

// SetMjpegFrameCallback sets the callback for MJPEG frames (for UVC streaming)
func (n *Native) SetMjpegFrameCallback(callback func(frame []byte)) {
	n.extraLock.Lock()
	defer n.extraLock.Unlock()
	n.onMjpegFrameReceived = callback
}
