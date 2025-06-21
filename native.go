package kvm

import (
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/jetkvm/kvm/internal/native"
	"github.com/pion/webrtc/v4/pkg/media"
)

var nativeInstance *native.Native

func initNative(systemVersion *semver.Version, appVersion *semver.Version) {
	nativeInstance = native.NewNative(native.NativeOptions{
		SystemVersion: systemVersion,
		AppVersion:    appVersion,
		OnVideoStateChange: func(state native.VideoState) {
			lastVideoState = state
			triggerVideoStateUpdate()
			requestDisplayUpdate(true)
		},
		OnVideoFrameReceived: func(frame []byte, duration time.Duration) {
			if currentSession != nil {
				err := currentSession.VideoTrack.WriteSample(media.Sample{Data: frame, Duration: duration})
				if err != nil {
					nativeLogger.Warn().Err(err).Msg("error writing sample")
				}
			}
		},
	})
	nativeInstance.Start()
}
