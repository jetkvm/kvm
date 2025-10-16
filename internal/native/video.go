package native

import (
	"fmt"
	"os"
	"time"
)

const sleepModeFile = "/sys/devices/platform/ff470000.i2c/i2c-4/4-000f/sleep_mode"

// DefaultEDID is the default EDID for the video stream.
const DefaultEDID = "00ffffffffffff0052620188008888881c150103800000780a0dc9a05747982712484c00000001010101010101010101010101010101023a801871382d40582c4500c48e2100001e011d007251d01e206e285500c48e2100001e000000fc00543734392d6648443732300a20000000fd00147801ff1d000a202020202020017b"

var extraLockTimeout = 5 * time.Second

// VideoState is the state of the video stream.
type VideoState struct {
	Ready          bool    `json:"ready"`
	Error          string  `json:"error,omitempty"` //no_signal, no_lock, out_of_range
	Width          int     `json:"width"`
	Height         int     `json:"height"`
	FramePerSecond float64 `json:"fps"`
}

func isSleepModeSupported() bool {
	_, err := os.Stat(sleepModeFile)
	return err == nil
}

func (n *Native) setSleepMode(enabled bool) error {
	if !n.sleepModeSupported {
		return nil
	}

	bEnabled := "0"
	if enabled {
		bEnabled = "1"
	}
	return os.WriteFile(sleepModeFile, []byte(bEnabled), 0644)
}

func (n *Native) getSleepMode() (bool, error) {
	if !n.sleepModeSupported {
		return false, nil
	}

	data, err := os.ReadFile(sleepModeFile)
	if err == nil {
		return string(data) == "1", nil
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

	// disable sleep mode before starting video
	_ = n.setSleepMode(false)

	videoStart()
	return nil
}
