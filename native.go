package kvm

import (
	"os"
	"sync"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/jetkvm/kvm/internal/diagnostics"
	"github.com/jetkvm/kvm/internal/native"
	"github.com/jetkvm/kvm/internal/rdp"
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
			requestDisplayUpdate(true, "video_state_changed")

			// Update VNC server with video resolution
			if state.Width > 0 && state.Height > 0 {
				GetVNCServer().UpdateVideoState(uint16(state.Width), uint16(state.Height))
				// Update RDP server with video resolution
				UpdateRDPVideoState(uint16(state.Width), uint16(state.Height))
			}
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
			// Send to WebRTC session
			if currentSession != nil {
				err := currentSession.VideoTrack.WriteSample(media.Sample{Data: frame, Duration: duration})
				if err != nil {
					nativeLogger.Warn().Err(err).Msg("error writing sample")
				}
			}
			// Send H.264 frames to RDP clients
			BroadcastRDPFrame(frame)
		},
		OnJpegFrameReceived: func(frame []byte) {
			// Hot potato: send directly to VNC clients synchronously
			// Frame buffer is only valid during this call
			GetVNCServer().BroadcastJPEGFrame(frame)

			// Also send to RDP bitmap mode subscribers
			BroadcastRDPJPEGFrame(frame)
		},
		OnRGBFrameReceived: func(frame native.RGBFrame) {
			// Send frames to RDP bitmap mode subscribers
			// Convert format from native to rdp package format
			format := rdp.RGBFrameFormatYUV422
			if frame.Format == native.RGBFrameFormatBGRX {
				format = rdp.RGBFrameFormatBGRX
			}
			BroadcastRDPRGBFrame(frame.Data, frame.Width, frame.Height, format)
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
		if err := nativeInstance.VideoSetEDID(config.EdidString); err != nil {
			nativeLogger.Warn().Err(err).Msg("error setting EDID")
		}
	}()

	if os.Getenv("JETKVM_CRASH_TESTING") == "1" {
		nativeInstance.DoNotUseThisIsForCrashTestingOnly()
	}

	// Initialize UVC streaming if enabled
	initUVC()
}
