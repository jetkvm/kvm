package usbgadget

import "time"

const dwc3Path = "/sys/bus/platform/drivers/dwc3"

const udcClassPath = "/sys/class/udc"

const hidWriteTimeout = 10 * time.Millisecond

const hidProbeWriteTimeout = 500 * time.Millisecond
