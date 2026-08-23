package usbgadget

import "fmt"

// WriteHIDReport only acknowledges a report after a configured USB gadget has
// accepted it. The write callback must return every error that means the
// report was not accepted by its HID endpoint.
func WriteHIDReport(state string, write func() error) error {
	if state != USBStateConfigured {
		return fmt.Errorf("USB is not configured for HID reports (state %q)", state)
	}
	return write()
}
