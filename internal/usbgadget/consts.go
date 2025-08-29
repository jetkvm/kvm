package usbgadget

import "time"

const dwc3Path = "/sys/bus/platform/drivers/dwc3"

const hidWriteTimeout = 10 * time.Millisecond
