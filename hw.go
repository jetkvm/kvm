package kvm

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	halSystem "github.com/jetkvm/kvm/internal/hal/system"
	"github.com/jetkvm/kvm/internal/ota"
)

func hwReboot(force bool, postRebootAction *ota.PostRebootAction, delay time.Duration) error {
	logger.Info().Dur("delayMs", delay).Msg("reboot requested")

	writeJSONRPCEvent("willReboot", postRebootAction, currentSession.Load())
	time.Sleep(1 * time.Second) // Wait for the JSONRPCEvent to be sent

	nativeInstance.SwitchToScreenIfDifferent("rebooting_screen")
	if delay > 1*time.Second {
		time.Sleep(delay - 1*time.Second) // wait requested extra settle time
	}

	err := halSystem.Reboot(force)
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
		serial, err := halSystem.ReadCPUSerialFromProc()
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
	halSystem.RunWatchdog(appCtx, halSystem.WatchdogOptions{
		OnOpenError: func(err error) {
			watchdogLogger.Warn().Err(err).Msg("unable to open /dev/watchdog, skipping watchdog reset")
		},
		OnWriteError: func(err error) {
			watchdogLogger.Warn().Err(err).Msg("error writing to /dev/watchdog, system may reboot")
		},
	})
}
