package kvm

import (
	"context"
	"fmt"
	"time"

	"github.com/jetkvm/kvm/internal/native"
	"github.com/jetkvm/kvm/internal/sync"
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

var (
	hostDisplayAdvertiseLock = sync.Mutex{}
	hostDisplayAdvertised    bool
)

func configuredVideoEDID() string {
	if config.EdidString == "" || isInternalDisabledEDID(config.EdidString) {
		return native.DefaultEDID
	}
	return config.EdidString
}

func isInternalDisabledEDID(edid string) bool {
	return edid == native.DisabledEDID
}

func isHostDisplayAdvertised() bool {
	hostDisplayAdvertiseLock.Lock()
	defer hostDisplayAdvertiseLock.Unlock()
	return hostDisplayAdvertised
}

func shouldAdvertiseHostDisplayLocked() bool {
	return !config.HideDisplayWhenIdle || getActiveSessions() > 0
}

func setHostDisplayAdvertised(enabled bool, reason string, force bool) error {
	hostDisplayAdvertiseLock.Lock()
	defer hostDisplayAdvertiseLock.Unlock()

	return setHostDisplayAdvertisedLocked(enabled, reason, force)
}

func setHostDisplayAdvertisedLocked(enabled bool, reason string, force bool) error {
	if !force && enabled == hostDisplayAdvertised {
		return nil
	}

	edid := native.DisabledEDID
	if enabled {
		edid = configuredVideoEDID()
	}

	if err := nativeInstance.VideoSetEDID(edid); err != nil {
		nativeLogger.Warn().Err(err).Bool("advertised", enabled).Str("reason", reason).Msg("failed to update host display advertisement")
		return err
	}

	hostDisplayAdvertised = enabled
	nativeLogger.Info().Bool("advertised", enabled).Str("reason", reason).Msg("host display advertisement updated")
	return nil
}

// applyHostDisplayAdvertisement reconciles the advertised state without rewriting
// EDID when the host display is already in the desired state.
func applyHostDisplayAdvertisement(reason string) error {
	return updateHostDisplayAdvertisement(reason, false)
}

// reapplyHostDisplayAdvertisement rewrites EDID even if the advertised
// boolean is unchanged; use it when native state or configured EDID changed.
func reapplyHostDisplayAdvertisement(reason string) error {
	return updateHostDisplayAdvertisement(reason, true)
}

func updateHostDisplayAdvertisement(reason string, force bool) error {
	hostDisplayAdvertiseLock.Lock()
	defer hostDisplayAdvertiseLock.Unlock()

	return setHostDisplayAdvertisedLocked(shouldAdvertiseHostDisplayLocked(), reason, force)
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
