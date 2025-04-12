package usbgadget

import "time"

func (u *UsbGadget) resetUserInputTime() {
	u.lastUserInput = time.Now()
}

func (u *UsbGadget) GetLastUserInputTime() time.Time {
	return u.lastUserInput
}
