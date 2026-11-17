package usbgadget

import (
	"os"
	"path"
	"strconv"
	"strings"
)

var extconClassPath = "/sys/class/extcon"

func (u *UsbGadget) IsUsbHostPresent() (present bool, known bool) {
	statePath := findUsbPhyExtconStatePath()
	if statePath == "" {
		return false, false
	}

	state, err := os.ReadFile(statePath)
	if err != nil {
		return false, false
	}

	return extconHostState(string(state))
}

func extconHostState(payload string) (hostPresent bool, known bool) {
	var sawHostCable, sawNonDataCable bool
	for line := range strings.SplitSeq(payload, "\n") {
		key, val, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		enabled, err := strconv.Atoi(strings.TrimSpace(val))
		if err != nil {
			continue
		}
		switch key {
		case "USB-HOST", "SDP", "CDP":
			sawHostCable = true
			if enabled != 0 {
				return true, true
			}
		case "DCP", "SLOW-CHARGER", "USB_VBUS_EN":
			if enabled != 0 {
				sawNonDataCable = true
			}
		}
	}
	if sawHostCable || sawNonDataCable {
		return false, true
	}
	return false, false
}

func findUsbPhyExtconStatePath() string {
	entries, err := os.ReadDir(extconClassPath)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		nameBytes, err := os.ReadFile(path.Join(extconClassPath, entry.Name(), "name"))
		if err != nil {
			continue
		}
		name := strings.TrimSpace(string(nameBytes))
		if strings.Contains(name, "usb") && strings.Contains(name, "phy") {
			return path.Join(extconClassPath, entry.Name(), "state")
		}
	}
	return ""
}
