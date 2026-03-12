package usbgadget

import "strings"

// IsHIDTemporarilyUnavailableError matches transient HID gadget errors that
// can happen while the USB gadget is detaching/rebinding.
func IsHIDTemporarilyUnavailableError(err error) bool {
	if err == nil {
		return false
	}

	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "no such device or address") ||
		strings.Contains(msg, "transport endpoint shutdown") ||
		strings.Contains(msg, "transport endpoint is not connected") ||
		strings.Contains(msg, "broken pipe")
}
