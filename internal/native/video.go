package native

import (
	"fmt"
	"os"
	"strings"
	"time"
)

const sleepModeFile = "/sys/devices/platform/ff470000.i2c/i2c-4/4-000f/sleep_mode"

// DefaultEDID is the default EDID for the video stream.
// CEA-861 extension with HDMI vendor block, audio support, and JetKVM manufacturer ID.
const DefaultEDID = "00ffffffffffff0028b4010001eeffc0302301038047287856ee91a3544c99260f5054000000d1c081c0318001010101010101010101023a801871382d40582c4500c48e2100001e011d007251d01e206e285500c48e2100001e000000fd00174c0f5111000a202020202020000000fc004a65744b564d2076310a202020011d020322d1431004012309070783010000e200cfe40d100401e305000065030c001000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000cf"

// Single-mode EDIDs that let the user advertise specific resolution+rate combos
// to the source PC, generated from scripts/edid_gen.py. CVT-RB v1 timings,
// EDID 1.4, no CEA extension. The 120 Hz variants halve per-frame source-side
// latency vs the default 1080p60. The TC358743 capture chip in JetKVM v1 has a
// hard ~120 Hz vrefresh ceiling.
const (
	// 1280x720 @ 120.0 Hz, pclk 131.75 MHz
	EDID720p120 = "00ffffffffffff0028b4020001eeffc03023010480351e780aee91a3544c99260f505400000001010101010101010101010101010101773300a050d02b2030200508122c2100001a000000100000000000000000000000000000000000fd0017fa0fff10000a202020202020000000fc004a65744b564d20373230703132005f"
	// 1280x720 @ 60.0 Hz, pclk 64.00 MHz
	EDID720p60 = "00ffffffffffff0028b4020001eeffc03023010480351e780aee91a3544c99260f505400000001010101010101010101010101010101001900a050d015203020a500122c2100001a000000100000000000000000000000000000000000fd0017fa0fff10000a202020202020000000fc004a65744b564d20373230703630006b"
	// 848x480 @ 120.0 Hz, pclk 61.50 MHz
	EDID480p120 = "00ffffffffffff0028b4020001eeffc03023010480351e780aee91a3544c99260f505400000001010101010101010101010101010101061850a030e01d1030202504122c2100001a000000100000000000000000000000000000000000fd0017fa0fff10000a202020202020000000fc004a65744b564d2034383070313200aa"
	// 848x480 @ 60.0 Hz, pclk 29.75 MHz
	EDID480p60 = "00ffffffffffff0028b4020001eeffc03023010480351e780aee91a3544c99260f5054000000010101010101010101010101010101019f0b50a030e00e1030203500122c2100001a000000100000000000000000000000000000000000fd0017fa0fff10000a202020202020000000fc004a65744b564d20343830703630001e"
)

var extraLockTimeout = 5 * time.Second

// VideoState is the state of the video stream.
type VideoState struct {
	Ready          bool                 `json:"ready"`
	Streaming      VideoStreamingStatus `json:"streaming"`
	Error          string               `json:"error,omitempty"` //no_signal, no_lock, out_of_range
	Width          int                  `json:"width"`
	Height         int                  `json:"height"`
	FramePerSecond float64              `json:"fps"`
}

func isSleepModeSupported() bool {
	_, err := os.Stat(sleepModeFile)
	return err == nil
}

const sleepModeWaitTimeout = 3 * time.Second

func (n *Native) waitForVideoStreamingStatus(status VideoStreamingStatus) error {
	timeout := time.After(sleepModeWaitTimeout)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		if videoGetStreamingStatus() == status {
			return nil
		}
		select {
		case <-timeout:
			return fmt.Errorf("timed out waiting for video streaming status to be %s", status.String())
		case <-ticker.C:
		}
	}
}

// before calling this function, make sure to lock n.videoLock
func (n *Native) setSleepMode(enabled bool) error {
	if !n.sleepModeSupported {
		return nil
	}

	bEnabled := "0"
	shouldWait := false
	if enabled {
		bEnabled = "1"

		switch videoGetStreamingStatus() {
		case VideoStreamingStatusActive:
			n.l.Info().Msg("stopping video stream to enable sleep mode")
			videoStop()
			shouldWait = true
		case VideoStreamingStatusStopping:
			n.l.Info().Msg("video stream is stopping, will enable sleep mode in a few seconds")
			shouldWait = true
		}
	}

	if shouldWait {
		if err := n.waitForVideoStreamingStatus(VideoStreamingStatusInactive); err != nil {
			return err
		}
	}

	return os.WriteFile(sleepModeFile, []byte(bEnabled), 0644)
}

func (n *Native) getSleepMode() (bool, error) {
	if !n.sleepModeSupported {
		return false, nil
	}

	data, err := os.ReadFile(sleepModeFile)
	if err == nil {
		return strings.TrimSpace(string(data)) == "1", nil
	}

	return false, nil
}

// VideoSetSleepMode sets the sleep mode for the video stream.
func (n *Native) VideoSetSleepMode(enabled bool) error {
	n.videoLock.Lock()
	defer n.videoLock.Unlock()

	return n.setSleepMode(enabled)
}

// VideoGetSleepMode gets the sleep mode for the video stream.
func (n *Native) VideoGetSleepMode() (bool, error) {
	n.videoLock.Lock()
	defer n.videoLock.Unlock()

	return n.getSleepMode()
}

// VideoSleepModeSupported checks if the sleep mode is supported.
func (n *Native) VideoSleepModeSupported() bool {
	return n.sleepModeSupported
}

// useExtraLock uses the extra lock to execute a function.
// if the lock is currently held by another goroutine, returns an error.
//
// it's used to ensure that only one change is made to the video stream at a time.
// as the change usually requires to restart video streaming
// TODO: check video streaming status instead of using a hardcoded timeout
func (n *Native) useExtraLock(fn func() error) error {
	if !n.extraLock.TryLock() {
		return fmt.Errorf("the previous change hasn't been completed yet")
	}
	err := fn()
	if err == nil {
		time.Sleep(extraLockTimeout)
	}
	n.extraLock.Unlock()
	return err
}

// VideoSetQualityFactor sets the quality factor for the video stream.
func (n *Native) VideoSetQualityFactor(factor float64) error {
	n.videoLock.Lock()
	defer n.videoLock.Unlock()

	return n.useExtraLock(func() error {
		return videoSetStreamQualityFactor(factor)
	})
}

// VideoGetQualityFactor gets the quality factor for the video stream.
func (n *Native) VideoGetQualityFactor() (float64, error) {
	n.videoLock.Lock()
	defer n.videoLock.Unlock()

	return videoGetStreamQualityFactor()
}

// VideoSetCodecType must be called before VideoStart(), not mid-stream.
func (n *Native) VideoSetCodecType(codecType int) error {
	n.videoLock.Lock()
	defer n.videoLock.Unlock()

	return videoSetCodecType(codecType)
}

func (n *Native) VideoGetCodecType() (int, error) {
	n.videoLock.Lock()
	defer n.videoLock.Unlock()

	return videoGetCodecType()
}

// VideoSetEDID sets the EDID for the video stream.
func (n *Native) VideoSetEDID(edid string) error {
	n.videoLock.Lock()
	defer n.videoLock.Unlock()

	if edid == "" {
		edid = DefaultEDID
	}

	return n.useExtraLock(func() error {
		return videoSetEDID(edid)
	})
}

// VideoGetEDID gets the EDID for the video stream.
func (n *Native) VideoGetEDID() (string, error) {
	n.videoLock.Lock()
	defer n.videoLock.Unlock()

	return videoGetEDID()
}

// VideoLogStatus gets the log status for the video stream.
func (n *Native) VideoLogStatus() (string, error) {
	n.videoLock.Lock()
	defer n.videoLock.Unlock()

	return videoLogStatus(), nil
}

// VideoStop stops the video stream.
func (n *Native) VideoStop() error {
	n.videoLock.Lock()
	defer n.videoLock.Unlock()

	videoStop()
	return nil
}

// VideoStart starts the video stream.
func (n *Native) VideoStart() error {
	n.videoLock.Lock()
	defer n.videoLock.Unlock()

	// check if the chip is currently in sleep mode
	wasSleeping, _ := n.getSleepMode()

	// disable sleep mode before starting video
	_ = n.setSleepMode(false)

	// when waking from sleep, the capture chip needs time to re-lock the HDMI
	// signal before we can start streaming (similar to the delay in useExtraLock)
	if wasSleeping {
		n.l.Info().Msg("capture chip was sleeping, waiting for signal re-lock")
		time.Sleep(extraLockTimeout)
	}

	videoStart()
	return nil
}

// VideoGetStreamingStatus gets the streaming status of the video.
func (n *Native) VideoGetStreamingStatus() VideoStreamingStatus {
	n.videoLock.Lock()
	defer n.videoLock.Unlock()

	return videoGetStreamingStatus()
}
