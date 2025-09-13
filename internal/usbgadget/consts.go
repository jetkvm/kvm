package usbgadget

import "time"

const dwc3Path = "/sys/bus/platform/drivers/dwc3"

// Timeout for HID writes to gadget endpoints. Some hosts poll at ~10ms, but
// under load this can be longer. Use a slightly higher value to reduce
// spurious timeouts while still preventing indefinite blocking.
const hidWriteTimeout = 50 * time.Millisecond

// Auto-release configuration. When keys are down and no successful HID write
// has happened for this duration, clear the keyboard state to prevent stuck
// keys after network hiccups.
const keyAutoReleaseAfter = 2 * time.Second
const keyAutoReleaseCheckInterval = 200 * time.Millisecond
