package native

import (
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
	onVideoStateChange   func(state VideoState)
	onVideoFrameReceived func(frame []byte, duration time.Duration)
}

type NativeOptions struct {
	SystemVersion        *semver.Version
	AppVersion           *semver.Version
	OnVideoStateChange   func(state VideoState)
	OnVideoFrameReceived func(frame []byte, duration time.Duration)
}

func NewNative(opts NativeOptions) *Native {
	onVideoStateChange := opts.OnVideoStateChange
	if onVideoStateChange == nil {
		onVideoStateChange = func(state VideoState) {
			nativeLogger.Info().Msg("video state changed")
		}
	}

	onVideoFrameReceived := opts.OnVideoFrameReceived
	if onVideoFrameReceived == nil {
		onVideoFrameReceived = func(frame []byte, duration time.Duration) {
			nativeLogger.Info().Msg("video frame received")
		}
	}

	return &Native{
		ready:                make(chan struct{}),
		l:                    nativeLogger,
		lD:                   displayLogger,
		systemVersion:        opts.SystemVersion,
		appVersion:           opts.AppVersion,
		onVideoStateChange:   opts.OnVideoStateChange,
		onVideoFrameReceived: opts.OnVideoFrameReceived,
	}
}

func (n *Native) Start() {
	// set up singleton
	setInstance(n)
	setUpNativeHandlers()

	// start the native video
	go n.handleLogChan()
	go n.handleVideoStateChan()
	go n.handleVideoFrameChan()

	n.initUI()
	go n.tickUI()

	close(n.ready)
}
