package native

import (
	"sync"
	"time"

	"github.com/Masterminds/semver/v3"
)

type Native struct {
	ready                chan struct{}
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
	n := &Native{
		ready:                make(chan struct{}),
		systemVersion:        opts.SystemVersion,
		appVersion:           opts.AppVersion,
		displayRotation:      opts.DisplayRotation,
		defaultQualityFactor: opts.DefaultQualityFactor,
		onVideoStateChange:   opts.OnVideoStateChange,
		onVideoFrameReceived: opts.OnVideoFrameReceived,
		onIndevEvent:         opts.OnIndevEvent,
		onRpcEvent:           opts.OnRpcEvent,
		sleepModeSupported:   isSleepModeSupported(),
		videoLock:            sync.Mutex{},
	}

	if n.onVideoStateChange == nil {
		n.onVideoStateChange = func(state VideoState) {
			GetDisplayLogger().Info().Interface("state", state).Msg("video state changed")
		}
	}

	if n.onVideoFrameReceived == nil {
		n.onVideoFrameReceived = func(frame []byte, duration time.Duration) {
			GetDisplayLogger().Trace().Int("frame_size", len(frame)).Dur("duration", duration).Msg("video frame received")
		}
	}

	if n.onIndevEvent == nil {
		n.onIndevEvent = func(event string) {
			GetDisplayLogger().Info().Str("event", event).Msg("indev event")
		}
	}

	if n.onRpcEvent == nil {
		n.onRpcEvent = func(event string) {
			GetNativeLogger().Info().Str("event", event).Msg("rpc event")
		}
	}

	if n.defaultQualityFactor <= 0 || n.defaultQualityFactor > 1 {
		n.defaultQualityFactor = 1.0
	}

	return n
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
		GetDisplayLogger().Error().Err(err).Msg("failed to initialize video")
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
			GetNativeLogger().Error().Interface("recovered", r).Msg("recovered from crash")
		}
	}()

	crash()
}

// GetLVGLVersion returns the LVGL version
func GetLVGLVersion() string {
	return uiGetLVGLVersion()
}
