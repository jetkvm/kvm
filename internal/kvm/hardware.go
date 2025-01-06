package kvm

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/jetkvm/kvm/internal/logging"
)

func extractSerialNumber() (string, error) {
	content, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return "", err
	}

	r, err := regexp.Compile("Serial\\s*:\\s*(\\S+)")
	if err != nil {
		return "", fmt.Errorf("failed to compile regex: %w", err)
	}

	matches := r.FindStringSubmatch(string(content))
	if len(matches) < 2 {
		return "", fmt.Errorf("no serial found")
	}

	return matches[1], nil
}

func readOtpEntropy() ([]byte, error) {
	content, err := os.ReadFile("/sys/bus/nvmem/devices/rockchip-otp0/nvmem")
	if err != nil {
		return nil, err
	}
	return content[0x17:0x1C], nil
}

var deviceID string
var deviceIDOnce sync.Once

func GetDeviceID() string {
	deviceIDOnce.Do(func() {
		serial, err := extractSerialNumber()
		if err != nil {
			logging.Logger.Warn("unknown serial number, the program likely not running on RV1106")
			deviceID = "unknown_device_id"
		} else {
			deviceID = serial
		}
	})
	return deviceID
}

func RunWatchdog(ctx context.Context) {
	file, err := os.OpenFile("/dev/watchdog", os.O_WRONLY, 0)
	if err != nil {
		logging.Logger.Warnf("unable to open /dev/watchdog: %v, skipping watchdog reset", err)
		return
	}
	defer file.Close()
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			_, err = file.Write([]byte{0})
			if err != nil {
				logging.Logger.Errorf("error writing to /dev/watchdog, system may reboot: %v", err)
			}
		case <-ctx.Done():
			//disarm watchdog with magic value
			_, err := file.Write([]byte("V"))
			if err != nil {
				logging.Logger.Errorf("failed to disarm watchdog, system may reboot: %v", err)
			}
			return
		}
	}
}

var builtAppVersion = "0.1.0+dev"

func GetLocalVersion() (systemVersion *semver.Version, appVersion *semver.Version, err error) {
	appVersion, err = semver.NewVersion(builtAppVersion)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid built-in app version: %w", err)
	}

	systemVersionBytes, err := os.ReadFile("/version")
	if err != nil {
		return nil, appVersion, fmt.Errorf("error reading system version: %w", err)
	}

	systemVersion, err = semver.NewVersion(strings.TrimSpace(string(systemVersionBytes)))
	if err != nil {
		return nil, appVersion, fmt.Errorf("invalid system version: %w", err)
	}

	return systemVersion, appVersion, nil
}
