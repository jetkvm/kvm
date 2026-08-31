package usbgadget

import (
	"os"
	"path"
	"strings"
)

var extconClassPath = "/sys/class/extcon"

func (u *UsbGadget) IsVbusPresent() (present bool, known bool) {
	statePath := findUsbPhyExtconStatePath()
	if statePath == "" {
		return false, false
	}

	state, err := os.ReadFile(statePath)
	if err != nil {
		return false, false
	}

	for line := range strings.SplitSeq(string(state), "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "USB="); ok {
			return v == "1", true
		}
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
