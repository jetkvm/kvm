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
	startTime := time.Now()
	n.l.Info().Str("phase", "native-start").Msg("[NATIVE-START] beginning Native.Start() initialization")

	// set up singleton
	n.l.Info().Str("phase", "native-start").Msg("[NATIVE-START] calling setInstance")
	setInstance(n)
	n.l.Info().Str("phase", "native-start").Msg("[NATIVE-START] setInstance completed")

	n.l.Info().Str("phase", "native-start").Msg("[NATIVE-START] calling setUpNativeHandlers (CGO callback setup)")
	handlersStart := time.Now()
	setUpNativeHandlers()
	n.l.Info().Str("phase", "native-start").Dur("elapsed", time.Since(handlersStart)).Msg("[NATIVE-START] setUpNativeHandlers completed")

	// start the native video
	n.l.Info().Str("phase", "native-start").Msg("[NATIVE-START] launching channel handler goroutines")
	go n.handleLogChan()
	go n.handleVideoStateChan()
	go n.handleVideoFrameChan()
	go n.handleIndevEventChan()
	go n.handleRpcEventChan()
	n.l.Info().Str("phase", "native-start").Msg("[NATIVE-START] channel handler goroutines launched")

	n.l.Info().Str("phase", "native-start").Uint16("rotation", n.displayRotation).Msg("[NATIVE-START] calling initUI (CGO uiInit)")
	uiStart := time.Now()
	n.initUI()
	n.l.Info().Str("phase", "native-start").Dur("elapsed", time.Since(uiStart)).Msg("[NATIVE-START] initUI completed")

	n.l.Info().Str("phase", "native-start").Msg("[NATIVE-START] launching tickUI goroutine")
	go n.tickUI()

	n.l.Info().Str("phase", "native-start").Float64("qualityFactor", n.defaultQualityFactor).Msg("[NATIVE-START] calling videoInit (CGO) - THIS IS A SUSPECTED BLOCKING POINT")
	videoStart := time.Now()
	if err := videoInit(n.defaultQualityFactor); err != nil {
		n.l.Error().Err(err).Dur("elapsed", time.Since(videoStart)).Msg("[NATIVE-START] videoInit failed")
		return err
	}
	n.l.Info().Str("phase", "native-start").Dur("elapsed", time.Since(videoStart)).Msg("[NATIVE-START] videoInit completed successfully")

	close(n.ready)
	n.l.Info().Str("phase", "native-start").Dur("totalElapsed", time.Since(startTime)).Msg("[NATIVE-START] Native.Start() completed successfully, ready channel closed")
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
