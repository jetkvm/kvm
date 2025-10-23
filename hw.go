package kvm

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
)

func extractSerialNumber() (string, error) {
	content, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return "", err
	}

	r, err := regexp.Compile(`Serial\s*:\s*(\S+)`)
	if err != nil {
		return "", fmt.Errorf("failed to compile regex: %w", err)
	}

	matches := r.FindStringSubmatch(string(content))
	if len(matches) < 2 {
		return "", fmt.Errorf("no serial found")
	}

	return matches[1], nil
}

func readOtpEntropy() ([]byte, error) { //nolint:unused
	content, err := os.ReadFile("/sys/bus/nvmem/devices/rockchip-otp0/nvmem")
	if err != nil {
		return nil, err
	}
	return content[0x17:0x1C], nil
}

func hwReboot(force bool, postRebootAction *PostRebootAction, delay time.Duration) error {
	logger.Info().Msgf("Reboot requested, rebooting in %d seconds...", delay)

	writeJSONRPCEvent("willReboot", postRebootAction, currentSession)
	time.Sleep(1 * time.Second) // Wait for the JSONRPCEvent to be sent

	nativeInstance.SwitchToScreenIfDifferent("rebooting_screen")
	time.Sleep(delay - (1 * time.Second)) // wait requested extra settle time

	args := []string{}
	if force {
		args = append(args, "-f")
	}

	cmd := exec.Command("reboot", args...)
	err := cmd.Start()
	if err != nil {
		logger.Error().Err(err).Msg("failed to reboot")
		switchToMainScreen()
		return fmt.Errorf("failed to reboot: %w", err)
	}

	// If the reboot command is successful, exit the program after 5 seconds
	go func() {
		time.Sleep(5 * time.Second)
		os.Exit(0)
	}()

	return nil
}

var deviceID string
var deviceIDOnce sync.Once

func GetDeviceID() string {
	deviceIDOnce.Do(func() {
		serial, err := extractSerialNumber()
		if err != nil {
			logger.Warn().Msg("unknown serial number, the program likely not running on RV1106")
			deviceID = "unknown_device_id"
		} else {
			deviceID = serial
		}
	})
	return deviceID
}

func GetDefaultHostname() string {
	deviceId := GetDeviceID()
	if deviceId == "unknown_device_id" {
		return "jetkvm"
	}

	return fmt.Sprintf("jetkvm-%s", strings.ToLower(deviceId))
}

func runWatchdog() {
	file, err := os.OpenFile("/dev/watchdog", os.O_WRONLY, 0)
	if err != nil {
		watchdogLogger.Warn().Err(err).Msg("unable to open /dev/watchdog, skipping watchdog reset")
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
				watchdogLogger.Warn().Err(err).Msg("error writing to /dev/watchdog, system may reboot")
			}
		case <-appCtx.Done():
			//disarm watchdog with magic value
			_, err := file.Write([]byte("V"))
			if err != nil {
				watchdogLogger.Warn().Err(err).Msg("failed to disarm watchdog, system may reboot")
			}
			return
		}
	}
}
