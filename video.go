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

	videoConsumersMu sync.Mutex
	videoConsumers   = map[string]struct{}{}
)

// acquireVideoStreamWithCodec registers a named consumer of the
// capture pipeline. If the pipeline is being started fresh (this is
// the first consumer), the codec is pinned to `codecType` before
// VideoStart runs. VideoSetCodecType MUST be called before VideoStart
// and is not safe mid-stream, so subsequent acquirers cannot change
// it — coordination between consumers is the caller's responsibility.
//
// `codecType` is 0 for H.264 and 1 for H.265.
func acquireVideoStreamWithCodec(consumer string, codecType int) {
	videoConsumersMu.Lock()
	defer videoConsumersMu.Unlock()

	if _, exists := videoConsumers[consumer]; exists {
		return
	}
	isFirst := len(videoConsumers) == 0
	videoConsumers[consumer] = struct{}{}
	if isFirst {
		_ = nativeInstance.VideoSetCodecType(codecType)
		_ = nativeInstance.VideoStart()
		stopVideoSleepModeTicker()
	}
}

// releaseVideoStream unregisters a consumer. When the last consumer is
// released, the native video stream is stopped and the HDMI sleep ticker
// is restarted.
func releaseVideoStream(consumer string) {
	videoConsumersMu.Lock()
	defer videoConsumersMu.Unlock()

	if _, exists := videoConsumers[consumer]; !exists {
		return
	}
	delete(videoConsumers, consumer)
	if len(videoConsumers) == 0 {
		_ = nativeInstance.VideoStop()
		startVideoSleepModeTicker()
	}
}

// videoStreamHasConsumers reports whether any consumer currently holds the
// capture pipeline open. The HDMI sleep ticker uses this to decide whether
// it is safe to put the capture chip to sleep.
func videoStreamHasConsumers() bool {
	videoConsumersMu.Lock()
	defer videoConsumersMu.Unlock()
	return len(videoConsumers) > 0
}

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

func doVideoSleepModeTicker(ctx context.Context, duration time.Duration) {
	timer := time.NewTimer(duration)
	defer timer.Stop()

	nativeLogger.Trace().Msg("HDMI sleep mode ticker started")

	for {
		select {
		case <-timer.C:
			if videoStreamHasConsumers() {
				nativeLogger.Warn().Msg("not going to enter HDMI sleep mode because the capture pipeline has consumers")
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
