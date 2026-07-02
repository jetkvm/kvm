package usbgadget

import "time"

// USB state strings as reported by the kernel via sysfs.
const (
	USBStateNotAttached = "not attached"
	USBStateUnknown     = "unknown"
)

// USBRecoveryRetryInterval is the minimum interval between USB recovery attempts.
const USBRecoveryRetryInterval = 5 * time.Second

// ShouldAttemptUSBRecovery returns true if a USB gadget recovery should be attempted,
// based on the current USB state, whether emulation is desired, and rate limiting.
func ShouldAttemptUSBRecovery(state string, desired bool, lastAttempt time.Time, now time.Time) bool {
	if state != USBStateNotAttached || !desired {
		return false
	}

	return lastAttempt.IsZero() || now.Sub(lastAttempt) >= USBRecoveryRetryInterval
}

// USBStateConfigured is the UDC sysfs state when the host has configured the gadget.
const USBStateConfigured = "configured"

// HidWriteTimeoutEscalationThreshold is the number of consecutive keyboard HID
// write timeouts, while the gadget reports "configured", after which recovery
// escalates to a full gadget reconfigure.
const HidWriteTimeoutEscalationThreshold = 3

// HidWriteRecoveryRetryInterval is the minimum interval between write-timeout
// escalations. A full reconfigure forces the host to re-enumerate the gadget,
// so repeated attempts are spaced well apart.
const HidWriteRecoveryRetryInterval = 30 * time.Second

// ShouldEscalateHidWriteRecovery reports whether keyboard HID write timeouts
// should trigger a full USB gadget reconfigure. A UDC rebind can leave
// /dev/hidg0 openable but non-functional: writes time out while the UDC still
// reports "configured". Only a full gadget reconfigure recovers from that
// state. Write timeouts in any other UDC state (e.g. "suspended" while the
// host sleeps) are expected and must not trigger recovery.
func ShouldEscalateHidWriteRecovery(state string, desired bool, consecutiveTimeouts int, lastAttempt time.Time, now time.Time) bool {
	if state != USBStateConfigured || !desired {
		return false
	}

	if consecutiveTimeouts < HidWriteTimeoutEscalationThreshold {
		return false
	}

	return lastAttempt.IsZero() || now.Sub(lastAttempt) >= HidWriteRecoveryRetryInterval
}
