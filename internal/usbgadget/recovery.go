package usbgadget

import "time"

// USB state strings as reported by the kernel via sysfs.
const (
	USBStateNotAttached = "not attached"
	USBStateUnknown     = "unknown"
	USBStateDefault     = "default"
)

const USBEnumerationGracePeriod = 15 * time.Second

// EnumerationRecovery permits one rebind for a stalled enumeration. The poller
// owns it; transient states during that rebind must not rearm the attempt.
type EnumerationRecovery struct {
	since     time.Time
	attempted bool
}

func (r *EnumerationRecovery) ShouldAttempt(state string, desired, hostPresent, hostKnown bool, lastRecovery, now time.Time) bool {
	if state == USBStateConfigured || (hostKnown && !hostPresent) {
		r.attempted = false
	}
	if state != USBStateDefault || !desired || !hostKnown || !hostPresent {
		r.since = time.Time{}
		return false
	}
	if r.since.IsZero() {
		r.since = now
	}
	if r.attempted || now.Sub(r.since) < USBEnumerationGracePeriod ||
		(!lastRecovery.IsZero() && now.Sub(lastRecovery) < USBEnumerationGracePeriod) {
		return false
	}
	r.attempted = true
	return true
}

// USBRecoveryRetryInterval is the minimum interval between USB recovery attempts.
const USBRecoveryRetryInterval = 5 * time.Second

func IsUSBStateAttached(state string) bool {
	return state != USBStateNotAttached && state != USBStateUnknown
}

// ShouldAttemptUSBRecovery returns true if a USB gadget recovery should be attempted,
// based on the current USB state, whether emulation is desired, and rate limiting.
func ShouldAttemptUSBRecovery(state string, desired bool, lastAttempt time.Time, now time.Time) bool {
	if state != USBStateNotAttached || !desired {
		return false
	}

	return lastAttempt.IsZero() || now.Sub(lastAttempt) >= USBRecoveryRetryInterval
}

const USBStateConfigured = "configured"

const HidWriteTimeoutEscalationThreshold = 3

const HidWriteRecoveryRetryInterval = 30 * time.Second

func ShouldEscalateHidWriteRecovery(state string, desired bool, consecutiveTimeouts int, lastAttempt time.Time, now time.Time) bool {
	if state != USBStateConfigured || !desired {
		return false
	}

	if consecutiveTimeouts < HidWriteTimeoutEscalationThreshold {
		return false
	}

	return lastAttempt.IsZero() || now.Sub(lastAttempt) >= HidWriteRecoveryRetryInterval
}
