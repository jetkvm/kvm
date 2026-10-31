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

// USBHostEnumerationTimeout is how long a bound gadget in the "not attached"
// state waits for a present host to enumerate it before one corrective
// rebind. A host that reboots keeps VBUS up while its controller is down, and
// enumerates the bound gadget by itself once the controller is back.
// Reconnecting or rebinding the gadget in that window races the enumeration
// and can leave a function unresponsive. The wait is measured from the first
// recovery poll that sees the state, so it runs up to one poll interval long.
const USBHostEnumerationTimeout = 120 * time.Second

// HostEnumerationWait tracks how long a present host has left a bound gadget
// unenumerated, and permits one rebind per episode. The poller owns it.
type HostEnumerationWait struct {
	since     time.Time
	attempted bool
}

// ShouldRebind reports whether the bound gadget should be rebound now: once,
// after the host has been present with the gadget still not attached for
// USBHostEnumerationTimeout. The episode ends with Reset or with the host
// going away.
func (w *HostEnumerationWait) ShouldRebind(hostPresent bool, now time.Time) bool {
	if !hostPresent {
		w.Reset()
		return false
	}
	if w.since.IsZero() {
		w.since = now
		return false
	}
	if w.attempted || now.Sub(w.since) < USBHostEnumerationTimeout {
		return false
	}
	w.attempted = true
	return true
}

// Reset ends the episode. Call it once the gadget is attached again, or
// whenever the gadget is reconnected or rebound for another reason.
func (w *HostEnumerationWait) Reset() {
	w.since = time.Time{}
	w.attempted = false
}

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
