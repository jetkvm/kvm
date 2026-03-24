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
				err := rpcResetConfig()
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
		OnVideoFrameReceived: func(frame []byte, duration time.Duration) {
			// 1. WebRTC: fan out to ALL active sessions
			forEachSession(func(s *Session) {
				if err := s.VideoTrack.WriteSample(media.Sample{Data: frame, Duration: duration}); err != nil {
					nativeLogger.Warn().Err(err).Msg("error writing sample to session")
				}
			})

			// 2. RTSP server (always, if enabled and running)
			if rtspServer != nil {
				rtspServer.WriteNALU(frame, duration)
			}

		},
		GetSessionInfo: func() diagnostics.SessionInfo {
			info := diagnostics.SessionInfo{
				ActiveSessions:    getActiveSessions(),
				HasCurrentSession: anySession(),
			}
			if s := getFirstSession(); s != nil {
				sessionInfo := s.GetDiagnosticsInfo()
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
		if err := nativeInstance.VideoSetEDID(config.EdidString); err != nil {
			nativeLogger.Warn().Err(err).Msg("error setting EDID")
		}
	}()

	if os.Getenv("JETKVM_CRASH_TESTING") == "1" {
		nativeInstance.DoNotUseThisIsForCrashTestingOnly()
	}
}
