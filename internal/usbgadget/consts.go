package usbgadget

import "time"

const dwc3Path = "/sys/bus/platform/drivers/dwc3"

const hidWriteTimeout = 10 * time.Millisecond

// hidProbeWriteTimeout bounds the recovery write probe. More generous than
// hidWriteTimeout: right after enumeration the host may not have started
// polling the interrupt endpoint yet, and a false negative here escalates to
// a disruptive full gadget reconfigure.
const hidProbeWriteTimeout = 500 * time.Millisecond
