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
// Base block DTDs: 1920x1080@60 (preferred, DTD0), 1280x720@120 (DTD1). 1280x720@60
// is still advertised via the Standard Timings block (0x81C0 at offset 40). NVIDIA
// drivers ignore non-VIC DTDs in the CTA extension, so the high-refresh DTD has to
// live in the base block to be picked up by GeForce display settings.
const DefaultEDID = "00FFFFFFFFFFFF0028B4010001EEFFC0302301038047287856EE91A3544C99260F5054000000D1C081C0318001010101010101010101023A801871382D40582C4500C48E2100001E773300A050D02B2030203500122C2100001A000000FD00174C0F5111000A202020202020000000FC004A65744B564D2076310A20202001D5020322D1431004012309070783010000E200CFE40D100401E305000065030C001000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000CF"

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
