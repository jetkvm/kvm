package kvm

import (
	"os"
	"time"

	"github.com/jetkvm/kvm/internal/diagnostics"
	"github.com/jetkvm/kvm/internal/native"
	"github.com/jetkvm/kvm/internal/sync"

	"github.com/Masterminds/semver/v3"
	"github.com/pion/webrtc/v4/pkg/media"
)

// lastVideoFrameDuration is the pacing of the last normal frame, which the
// first frame after a stall takes over. Frame callback only.
var lastVideoFrameDuration = 16 * time.Millisecond

// videoFrameTiming turns the time since the previous frame into how far the
// RTP clock skips before this frame and the duration the frame itself gets.
func videoFrameTiming(sinceLastFrame time.Duration) (skip, duration time.Duration) {
	if skip = rtpClockSkip(sinceLastFrame, lastVideoFrameDuration); skip > 0 {
		return skip, lastVideoFrameDuration
	}
	lastVideoFrameDuration = sinceLastFrame
	return 0, sinceLastFrame
}

var (
	nativeInstance native.NativeInterface
	nativeCmdLock  = sync.Mutex{}
)

func initNative(systemVersion *semver.Version, appVersion *semver.Version) {
	if failsafeModeActive {
		nativeInstance = &native.EmptyNativeInterface{}
		nativeLogger.Warn().Msg("failsafe mode active, using empty native interface")
		return
	}

	nativeLogger.Info().Msg("initializing native proxy")
	var err error
	nativeInstance, err = native.NewNativeProxy(native.NativeOptions{
		SystemVersion:        systemVersion,
		AppVersion:           appVersion,
		DisplayRotation:      config.GetDisplayRotation(),
		DefaultQualityFactor: config.VideoQualityFactor,
		MaxRestartAttempts:   config.NativeMaxRestart,
		OnNativeRestart: func() {
			configureDisplayOnNativeRestart()
			_ = reapplyHostDisplayAdvertisement("native_restarted")
		},
		OnVideoStateChange: func(state native.VideoState) {
			lastVideoState = state
			triggerVideoStateUpdate()
			requestDisplayUpdate(false, "video_state_changed")
		},
		OnIndevEvent: func(event string) {
			nativeLogger.Trace().Str("event", event).Msg("indev event received")
			wakeDisplay(false, "indev_event")
		},
		OnRpcEvent: func(event string) {
			nativeCmdLock.Lock()
			defer nativeCmdLock.Unlock()

			nativeLogger.Trace().Str("event", event).Msg("rpc event received")
			switch event {
			case "resetConfig":
				nativeLogger.Info().Msg("Reset configuration request via native rpc event")
				err := resetConfig()
				if err != nil {
					nativeLogger.Warn().Err(err).Msg("error resetting config")
				}
				_ = rpcReboot(true)
			case "reboot":
				nativeLogger.Info().Msg("Reboot request via native rpc event")
				_ = rpcReboot(true)
			case "toggleDHCPClient":
				nativeLogger.Info().Msg("Toggle DHCP request via native rpc event")
				_ = rpcToggleDHCPClient()
			default:
				nativeLogger.Warn().Str("event", event).Msg("unknown rpc event received")
			}
		},
		OnVideoFrameReceived: func(frame []byte, sinceLastFrame time.Duration) {
			session := currentSession
			if session == nil {
				return
			}
			skip, duration := videoFrameTiming(sinceLastFrame)
			if skip > 0 {
				// An empty sample only moves the clock.
				_ = session.VideoTrack.WriteSample(media.Sample{Duration: skip})
			}
			if err := session.VideoTrack.WriteSample(media.Sample{Data: frame, Duration: duration}); err != nil {
				nativeLogger.Warn().Err(err).Msg("error writing sample")
			}
		},
		GetSessionInfo: func() diagnostics.SessionInfo {
			info := diagnostics.SessionInfo{
				ActiveSessions:    getActiveSessions(),
				HasCurrentSession: currentSession != nil,
			}
			if currentSession != nil {
				sessionInfo := currentSession.GetDiagnosticsInfo()
				info.ICEConnectionState = sessionInfo.ICEConnectionState
				info.SignalingState = sessionInfo.SignalingState
				info.ConnectionState = sessionInfo.ConnectionState
				info.DataChannels = sessionInfo.DataChannels
			}
			return info
		},
	})
	if err != nil {
		nativeLogger.Fatal().Err(err).Msg("failed to create native proxy")
	}

	if err := nativeInstance.Start(); err != nil {
		nativeLogger.Fatal().Err(err).Msg("failed to start native proxy")
	}
	go func() {
		_ = reapplyHostDisplayAdvertisement("native_started")
	}()

	if os.Getenv("JETKVM_CRASH_TESTING") == "1" {
		nativeInstance.DoNotUseThisIsForCrashTestingOnly()
	}
}
