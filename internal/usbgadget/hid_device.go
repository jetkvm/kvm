package usbgadget

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// hidDevicePath returns the /dev/hidgN node of a HID function. The kernel
// numbers the nodes in the order the functions were created, so when a HID
// device is disabled the later ones move down a number. The function's dev
// attribute holds the minor number of its own node; fallback is used when it
// cannot be read.
func (u *UsbGadget) hidDevicePath(itemKey, fallback string) string {
	path, err := u.GetPath(itemKey)
	if err != nil {
		return fallback
	}
	dev, err := os.ReadFile(filepath.Join(path, "dev"))
	if err != nil {
		return fallback
	}
	var major, minor int
	if _, err := fmt.Sscanf(strings.TrimSpace(string(dev)), "%d:%d", &major, &minor); err != nil {
		return fallback
	}
	return fmt.Sprintf("/dev/hidg%d", minor)
}
