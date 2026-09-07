package usbgadget

import "os"

func (u *UsbGadget) openHIDFile(name string, flag int, perm os.FileMode) (*os.File, error) {
	if u.hidOpenFile != nil {
		return u.hidOpenFile(name, flag, perm)
	}
	return os.OpenFile(name, flag, perm)
}
