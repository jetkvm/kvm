package usbgadget

import (
	"errors"
	"syscall"
)

// IsHIDTemporarilyUnavailableError matches transient HID gadget errors that
// can happen while the USB gadget is detaching/rebinding.
func IsHIDTemporarilyUnavailableError(err error) bool {
	if err == nil {
		return false
	}

	return errors.Is(err, syscall.ENXIO) || // no such device or address
		errors.Is(err, syscall.ESHUTDOWN) || // transport endpoint shutdown
		errors.Is(err, syscall.ENOTCONN) || // transport endpoint is not connected
		errors.Is(err, syscall.EPIPE) // broken pipe
}
