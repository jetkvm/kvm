package kvm

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/jpeg"
	"image/png"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

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

const (
	screenshotAutoStartTimeout  = 10 * time.Second
	screenshotSnapshotRetryWait = 300 * time.Millisecond
	screenshotLease             = 5 * time.Minute
)

var (
	screenshotInFlightMu sync.Mutex
	screenshotInFlight   int

	// Capture started for a screenshot stays up until the lease runs out, so
	// a client polling for changes does not restart it on every call.
	screenshotLeaseMu    sync.Mutex
	screenshotLeaseUntil time.Time
	screenshotLeaseTimer *time.Timer
)

// captureScreenshot returns a JPEG snapshot, starting video capture on
// demand if nothing is currently streaming (e.g. no browser session is
// open). Capture then stays up for screenshotLease after the last
// screenshot and is stopped unless a session is streaming, so a session
// that paused the stream, as a hidden tab or the virtual media dialog
// does, gets it back paused once the screenshots stop.
func captureScreenshot() ([]byte, error) {
	screenshotInFlightMu.Lock()
	screenshotInFlight++
	screenshotInFlightMu.Unlock()
	defer func() {
		screenshotInFlightMu.Lock()
		screenshotInFlight--
		screenshotInFlightMu.Unlock()
	}()

	deadline := time.Now().Add(screenshotAutoStartTimeout)
	screenshotLeaseMu.Lock()
	coldStart := time.Now().After(screenshotLeaseUntil) && !sessionStreaming()
	screenshotLeaseMu.Unlock()

	// Started under the pause lock so it cannot interleave with a pause.
	videoPauseMu.Lock()
	_ = nativeInstance.VideoStart()
	videoPauseMu.Unlock()
	defer extendScreenshotLease()

	if coldStart {
		// The first frames after a start can come from the capture pool as
		// it was before the stop, so skip one and let the pool turn over.
		if _, err := snapshotBefore(deadline); err != nil {
			return nil, err
		}
		time.Sleep(100 * time.Millisecond)
	}
	return snapshotBefore(deadline)
}

// snapshotBefore asks the native side for a frame until deadline. It retries
// on any error, not just ErrVideoNotStreaming: the native side also returns
// transient per-attempt errors (encoder timeout, no frame yet) while capture
// is still spinning up or between frames.
func snapshotBefore(deadline time.Time) ([]byte, error) {
	for {
		jpegBytes, err := nativeInstance.VideoGetSnapshot()
		if err == nil {
			return jpegBytes, nil
		}
		if !time.Now().Before(deadline) {
			return nil, err
		}
		time.Sleep(screenshotSnapshotRetryWait)
	}
}

// sessionStreaming reports whether a WebRTC session is receiving video.
func sessionStreaming() bool {
	videoPauseMu.Lock()
	paused := videoPausedBy != nil
	videoPauseMu.Unlock()
	return !paused && getActiveSessions() > 0
}

func extendScreenshotLease() {
	screenshotLeaseMu.Lock()
	defer screenshotLeaseMu.Unlock()
	screenshotLeaseUntil = time.Now().Add(screenshotLease)
	if screenshotLeaseTimer != nil {
		screenshotLeaseTimer.Stop()
	}
	screenshotLeaseTimer = time.AfterFunc(screenshotLease, releaseScreenshotLease)
}

// releaseScreenshotLease stops capture once no screenshot has asked for it
// for screenshotLease, unless a session is streaming.
func releaseScreenshotLease() {
	screenshotLeaseMu.Lock()
	expired := !time.Now().Before(screenshotLeaseUntil)
	screenshotLeaseMu.Unlock()
	screenshotInFlightMu.Lock()
	busy := screenshotInFlight > 0
	screenshotInFlightMu.Unlock()
	if !expired || busy || sessionStreaming() {
		return
	}
	_ = nativeInstance.VideoStop()
}

// handleScreenshotJPEG returns a single JPEG-encoded frame of the current
// video feed, straight from the native capture. Starts video capture on
// demand if nothing is currently streaming.
func handleScreenshotJPEG(c *gin.Context) {
	jpegBytes, err := captureScreenshot()
	if err != nil {
		writeScreenshotError(c, err)
		return
	}
	c.Data(http.StatusOK, "image/jpeg", jpegBytes)
}

// handleScreenshotPNG returns the same frame as handleScreenshotJPEG,
// re-encoded as PNG. The capture hardware only produces JPEG, so this costs
// a decode+re-encode on the device CPU; fine for an occasional snapshot.
func handleScreenshotPNG(c *gin.Context) {
	jpegBytes, err := captureScreenshot()
	if err != nil {
		writeScreenshotError(c, err)
		return
	}

	img, err := jpeg.Decode(bytes.NewReader(jpegBytes))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to decode captured frame"})
		return
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to encode PNG"})
		return
	}

	c.Data(http.StatusOK, "image/png", buf.Bytes())
}

func writeScreenshotError(c *gin.Context, err error) {
	if errors.Is(err, native.ErrVideoNotStreaming) {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "no video signal detected (timed out starting capture)"})
		return
	}
	c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
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
