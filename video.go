package kvm

import (
	"context"
	"fmt"
	"time"

	"github.com/jetkvm/kvm/internal/native"
)

var (
	lastVideoState       native.VideoState
	videoSleepModeCtx    context.Context
	videoSleepModeCancel context.CancelFunc
)

const (
	defaultVideoSleepModeDuration = 1 * time.Minute
)

func triggerVideoStateUpdate() {
	go func() {
		writeJSONRPCEvent("videoInputState", lastVideoState, currentSession)
	}()

	// Publish video state to MQTT
	if mqttManager != nil {
		mqttManager.publishVideoState()
	}

	nativeLogger.Info().Interface("state", lastVideoState).Msg("video state updated")
}

func rpcGetVideoState() (native.VideoState, error) {
	notifyFailsafeMode(currentSession)
	return lastVideoState, nil
}

type rpcVideoSleepModeResponse struct {
	Supported bool `json:"supported"`
	Enabled   bool `json:"enabled"`
	Duration  int  `json:"duration"`
}

func rpcGetVideoSleepMode() rpcVideoSleepModeResponse {
	sleepMode, _ := nativeInstance.VideoGetSleepMode()
	return rpcVideoSleepModeResponse{
		Supported: nativeInstance.VideoSleepModeSupported(),
		Enabled:   sleepMode,
		Duration:  config.VideoSleepAfterSec,
	}
}

func rpcSetVideoSleepMode(duration int) error {
	if duration < 0 {
		duration = -1 // disable
	}

	config.VideoSleepAfterSec = duration
	if err := SaveConfig(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	// we won't restart the ticker here,
	// as the session can't be inactive when this function is called
	return nil
}

func stopVideoSleepModeTicker() {
	nativeLogger.Trace().Msg("stopping HDMI sleep mode ticker")

	if videoSleepModeCancel != nil {
		nativeLogger.Trace().Msg("canceling HDMI sleep mode ticker context")
		videoSleepModeCancel()
		videoSleepModeCancel = nil
		videoSleepModeCtx = nil
	}
}

func startVideoSleepModeTicker() {
	if !nativeInstance.VideoSleepModeSupported() {
		return
	}

	var duration time.Duration

	if config.VideoSleepAfterSec == 0 {
		duration = defaultVideoSleepModeDuration
	} else if config.VideoSleepAfterSec > 0 {
		duration = time.Duration(config.VideoSleepAfterSec) * time.Second
	} else {
		stopVideoSleepModeTicker()
		return
	}

	// Stop any existing timer and goroutine
	stopVideoSleepModeTicker()

	// Create new context for this ticker
	videoSleepModeCtx, videoSleepModeCancel = context.WithCancel(context.Background())

	go doVideoSleepModeTicker(videoSleepModeCtx, duration)
}

type rpcVirtualDisplayPolicyResponse struct {
	Supported bool `json:"supported"`
	Enabled   bool `json:"enabled"`
}

func rpcGetVirtualDisplayPolicy() rpcVirtualDisplayPolicyResponse {
	return rpcVirtualDisplayPolicyResponse{
		Supported: nativeInstance.VideoHotplugSupported(),
		Enabled:   config.DisableVirtualDisplayWhenIdle,
	}
}

func rpcSetVirtualDisplayPolicy(enabled bool) error {
	previous := config.DisableVirtualDisplayWhenIdle
	config.DisableVirtualDisplayWhenIdle = enabled

	if err := applyVirtualDisplayPolicy("policy_changed"); err != nil {
		config.DisableVirtualDisplayWhenIdle = previous
		return fmt.Errorf("failed to apply virtual display policy: %w", err)
	}

	if err := SaveConfig(); err != nil {
		config.DisableVirtualDisplayWhenIdle = previous
		if rollbackErr := applyVirtualDisplayPolicy("policy_rollback"); rollbackErr != nil {
			nativeLogger.Warn().Err(rollbackErr).Msg("failed to roll back virtual display policy after save error")
		}
		return fmt.Errorf("failed to save config: %w", err)
	}
	return nil
}

// primeVideoOutput configures the EDID and virtual display policy after the
// native process comes up (initial boot or restart). When the policy says to
// hide the display while idle, only the EDID cache is primed so a later
// VideoSetHotplug(true) can restore it; otherwise the EDID is applied
// directly. Either way, the policy is re-asserted so a restart with no active
// sessions does not cause a phantom monitor to pop in on the host PC.
func primeVideoOutput(reason string) {
	if config.DisableVirtualDisplayWhenIdle && nativeInstance.VideoHotplugSupported() {
		if err := nativeInstance.VideoCacheEDID(config.EdidString); err != nil {
			nativeLogger.Warn().Err(err).Str("reason", reason).Msg("error caching EDID")
		}
	} else if err := nativeInstance.VideoSetEDID(config.EdidString); err != nil {
		nativeLogger.Warn().Err(err).Str("reason", reason).Msg("error setting EDID")
	}
	if err := applyVirtualDisplayPolicy(reason); err != nil {
		nativeLogger.Warn().Err(err).Str("reason", reason).Msg("failed to apply virtual display policy")
	}
}

// applyVirtualDisplayPolicy reconciles HDMI hotplug presence with the
// "Disable virtual display when idle" policy. When the policy is off, hotplug
// is always enabled. When on, hotplug follows session presence so the host PC
// sees no virtual monitor while no client is connected.
//
// Safe to call on every session transition: VideoSetHotplug short-circuits
// when the requested state already matches the last applied state.
func applyVirtualDisplayPolicy(reason string) error {
	if !nativeInstance.VideoHotplugSupported() {
		return nil
	}

	enableHotplug := true
	if config.DisableVirtualDisplayWhenIdle {
		enableHotplug = getActiveSessions() > 0
	}

	nativeLogger.Debug().
		Str("reason", reason).
		Bool("enable_hotplug", enableHotplug).
		Msg("evaluating virtual display policy")

	if err := nativeInstance.VideoSetHotplug(enableHotplug); err != nil {
		nativeLogger.Warn().
			Err(err).
			Str("reason", reason).
			Bool("enable_hotplug", enableHotplug).
			Msg("failed to apply virtual display policy")
		return err
	}
	return nil
}

func doVideoSleepModeTicker(ctx context.Context, duration time.Duration) {
	timer := time.NewTimer(duration)
	defer timer.Stop()

	nativeLogger.Trace().Msg("HDMI sleep mode ticker started")

	for {
		select {
		case <-timer.C:
			if getActiveSessions() > 0 {
				nativeLogger.Warn().Msg("not going to enter HDMI sleep mode because there are active sessions")
				continue
			}

			nativeLogger.Trace().Msg("entering HDMI sleep mode")
			_ = nativeInstance.VideoSetSleepMode(true)
		case <-ctx.Done():
			nativeLogger.Trace().Msg("HDMI sleep mode ticker stopped")
			return
		}
	}
}
