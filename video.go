package kvm

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jetkvm/kvm/internal/hal/native"
)

var (
	lastVideoState       atomic.Pointer[native.VideoState]
	videoSleepMu         sync.Mutex // protects videoSleepModeCtx and videoSleepModeCancel
	videoSleepModeCtx    context.Context
	videoSleepModeCancel context.CancelFunc
	emptyVideoState      native.VideoState
)

// getLastVideoState returns the last known video state, never nil.
func getLastVideoState() *native.VideoState {
	if s := lastVideoState.Load(); s != nil {
		return s
	}
	return &emptyVideoState
}

const (
	defaultVideoSleepModeDuration = 1 * time.Minute
)

func triggerVideoStateUpdate() {
	state := getLastVideoState()
	go func() {
		writeJSONRPCEvent("videoInputState", state, currentSession.Load())
	}()

	nativeLogger.Info().Interface("state", state).Msg("video state updated")
}

func rpcGetVideoState() (native.VideoState, error) {
	notifyFailsafeMode(currentSession.Load())
	return *getLastVideoState(), nil
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
		Duration:  loadCfg().VideoSleepAfterSec,
	}
}

func rpcSetVideoSleepMode(duration int) error {
	if duration < 0 {
		duration = -1 // disable
	}

	if err := updateAndSaveConfig(func(cfg *Config) {
		cfg.VideoSleepAfterSec = duration
	}); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	// we won't restart the ticker here,
	// as the session can't be inactive when this function is called
	return nil
}

func stopVideoSleepModeTicker() {
	videoSleepMu.Lock()
	defer videoSleepMu.Unlock()

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

	sleepSec := loadCfg().VideoSleepAfterSec
	if sleepSec == 0 {
		duration = defaultVideoSleepModeDuration
	} else if sleepSec > 0 {
		duration = time.Duration(sleepSec) * time.Second
	} else {
		stopVideoSleepModeTicker()
		return
	}

	// Stop any existing timer and goroutine
	stopVideoSleepModeTicker()

	videoSleepMu.Lock()
	// Create new context for this ticker
	videoSleepModeCtx, videoSleepModeCancel = context.WithCancel(context.Background())
	ctx := videoSleepModeCtx
	videoSleepMu.Unlock()

	go doVideoSleepModeTicker(ctx, duration)
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
				timer.Reset(duration)
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
