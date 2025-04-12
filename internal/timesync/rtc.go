package timesync

import (
	"fmt"
	"os"
)

var (
	rtcDeviceSearchPaths = []string{
		"/dev/rtc",
		"/dev/rtc0",
		"/dev/rtc1",
		"/dev/misc/rtc",
		"/dev/misc/rtc0",
		"/dev/misc/rtc1",
	}
)

func getRtcDevicePath() (string, error) {
	for _, path := range rtcDeviceSearchPaths {
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("rtc device not found")
}
