package timesync

import (
	halSystem "github.com/jetkvm/kvm/internal/hal/system"
)

func getRtcDevicePath() (string, error) {
	return halSystem.FindRTCDevicePath()
}
