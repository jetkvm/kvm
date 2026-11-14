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
// attribute holds the minor number of its own node.
func (u *UsbGadget) hidDevicePath(itemKey string) (string, error) {
	path, err := u.GetPath(itemKey)
	if err != nil {
		return "", err
	}
	devPath := filepath.Join(path, "dev")
	dev, err := os.ReadFile(devPath)
	if err != nil {
		return "", err
	}
	var major, minor int
	if _, err := fmt.Sscanf(strings.TrimSpace(string(dev)), "%d:%d", &major, &minor); err != nil {
		return "", fmt.Errorf("parse %s: %w", devPath, err)
	}
	return fmt.Sprintf("/dev/hidg%d", minor), nil
}
