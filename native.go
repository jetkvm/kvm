package kvm

import (
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/jetkvm/kvm/internal/diagnostics"
	"github.com/jetkvm/kvm/internal/native"
	"github.com/jetkvm/kvm/internal/rdp"
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

	// Check config for native mode: "direct" for CGO mode, "subprocess" (default) for crash isolation
	cfg := loadCfg()
	nativeMode := cfg.NativeMode
	if nativeMode == "" {
		nativeMode = "subprocess" // default to subprocess for crash isolation
	}

	opts := native.NativeOptions{
		SystemVersion:        systemVersion,
		AppVersion:           appVersion,
		DisplayRotation:      cfg.GetDisplayRotation(),
		DefaultQualityFactor: cfg.VideoQualityFactor,
		MaxRestartAttempts:   cfg.NativeMaxRestart,
		OnNativeRestart: func() {
			configureDisplayOnNativeRestart()
		},
		OnVideoStateChange: func(state native.VideoState) {
			lastVideoState.Store(&state)
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
			// Send to WebRTC session (allocation-free path)
			if s := currentSession.Load(); s != nil {
				if err := s.WriteVideoFrame(frame, duration); err != nil {
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
			// Pass the release callback to return the buffer to the native pool
			BroadcastRDPRGBFrame(frame.Data, frame.Width, frame.Height, format, frame.Release)
		},
		GetSessionInfo: func() diagnostics.SessionInfo {
			s := currentSession.Load()
			info := diagnostics.SessionInfo{
				ActiveSessions:    getActiveSessions(),
				HasCurrentSession: s != nil,
			}
			if s != nil {
				sessionInfo := s.GetDiagnosticsInfo()
				info.ICEConnectionState = sessionInfo.ICEConnectionState
				info.SignalingState = sessionInfo.SignalingState
				info.ConnectionState = sessionInfo.ConnectionState
				info.DataChannels = sessionInfo.DataChannels
			}
			return info
		},
	}

	// Initialize native based on mode
	var err error
	if nativeMode == "direct" {
		nativeLogger.Info().Msg("initializing native in DIRECT mode (CGO, no subprocess)")
		nativeInstance = native.NewNative(opts)
	} else {
		nativeLogger.Info().Msg("initializing native in SUBPROCESS mode (crash-isolated)")
		nativeInstance, err = native.NewNativeProxy(opts)
		if err != nil {
			nativeLogger.Fatal().Err(err).Msg("failed to create native proxy")
		}
	}

	if err := nativeInstance.Start(); err != nil {
		nativeLogger.Fatal().Err(err).Msg("failed to start native instance")
	}
	go func() {
		if err := nativeInstance.VideoSetEDID(loadCfg().EdidString); err != nil {
			nativeLogger.Warn().Err(err).Msg("error setting EDID")
		}
	}()

	if os.Getenv("JETKVM_CRASH_TESTING") == "1" {
		nativeInstance.DoNotUseThisIsForCrashTestingOnly()
	}

	// Initialize UVC streaming if enabled
	initUVC()
}

// NativeModeOption describes a selectable native execution mode.
type NativeModeOption struct {
	Value       string `json:"value"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

// NativeModeState is the response for getNativeMode.
type NativeModeState struct {
	Mode           string             `json:"mode"`
	AvailableModes []NativeModeOption `json:"availableModes"`
	RequiresReboot bool               `json:"requiresReboot"`
}

var nativeModeOptions = []NativeModeOption{
	{Value: "subprocess", Label: "Subprocess", Description: "Crash-isolated mode — native code runs in a separate process (default)"},
	{Value: "direct", Label: "Direct", Description: "In-process CGO mode — more efficient but native crashes bring down the app"},
}

func rpcGetNativeMode() (NativeModeState, error) {
	mode := loadCfg().NativeMode
	if mode == "" {
		mode = "subprocess"
	}
	return NativeModeState{
		Mode:           mode,
		AvailableModes: nativeModeOptions,
		RequiresReboot: true,
	}, nil
}

func rpcSetNativeMode(mode string) error {
	if mode != "subprocess" && mode != "direct" {
		return fmt.Errorf("invalid native mode: %s (must be subprocess or direct)", mode)
	}

	if err := updateAndSaveConfig(func(cfg *Config) {
		cfg.NativeMode = mode
	}); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	nativeLogger.Info().Str("mode", mode).Msg("native execution mode updated (reboot required)")
	return nil
}
