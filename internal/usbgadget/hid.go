package usbgadget

import "time"

func (u *UsbGadget) resetUserInputTime() {
	u.lastUserInput.Store(time.Now().UnixNano())
}

func (u *UsbGadget) GetLastUserInputTime() time.Time {
	nano := u.lastUserInput.Load()
	if nano == 0 {
		return time.Time{}
	}
	return time.Unix(0, nano)
}
