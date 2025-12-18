package diagnostics

import (
	"os"
	"path/filepath"
	"strings"
)

// LogUSBGadget logs USB gadget state and HID devices.
func (d *Diagnostics) LogUSBGadget() {
	d.logger.Info().Msg("--- USB Gadget ---")

	// List UDCs
	d.listDirLog("UDC list", "/sys/devices/platform/usbdrd")

	// Find the UDC name and read its state
	files, err := os.ReadDir("/sys/devices/platform/usbdrd")
	if err == nil {
		for _, file := range files {
			if file.IsDir() && strings.HasSuffix(file.Name(), ".usb") {
				udcName := file.Name()
				statePath := filepath.Join("/sys/class/udc", udcName, "state")
				d.readFileLog("UDC state ("+udcName+")", statePath)
			}
		}
	}

	// UDC binding
	d.readFileLog("gadget UDC binding", "/sys/kernel/config/usb_gadget/jetkvm/UDC")

	// Gadget config directory
	d.listDirLog("gadget config", "/sys/kernel/config/usb_gadget/jetkvm")

	// HID devices
	hidDevices := []string{"/dev/hidg0", "/dev/hidg1", "/dev/hidg2"}
	for _, path := range hidDevices {
		d.checkFileLog(filepath.Base(path), path)
	}
}
