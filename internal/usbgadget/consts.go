package usbgadget

import "time"

const dwc3Path = "/sys/bus/platform/drivers/dwc3"

const udcClassPath = "/sys/class/udc"

// Leave room for host polling and scheduler delays while the previous HID
// report is pending. A 10 ms deadline drops reports during ordinary bursts.
const hidWriteTimeout = 100 * time.Millisecond

const hidProbeWriteTimeout = 500 * time.Millisecond
