package native

import (
	"os"
)

const sleepModeFile = "/sys/devices/platform/ff470000.i2c/i2c-4/4-000f/sleep_mode"

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

// VideoSetQualityFactor sets the quality factor for the video stream.
func (n *Native) VideoSetQualityFactor(factor float64) error {
	n.videoLock.Lock()
	defer n.videoLock.Unlock()

	return videoSetStreamQualityFactor(factor)
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

	return videoSetEDID(edid)
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
