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
