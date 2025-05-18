package usbgadget

import (
	"fmt"
)

func (u *UsbGadget) logWarn(msg string, err error) error {
	if err == nil {
		err = fmt.Errorf(msg)
	}
	if u.strictMode {
		return err
	}
	u.log.Warn().Err(err).Msg(msg)
	return nil
}

func (u *UsbGadget) logError(msg string, err error) error {
	if err == nil {
		err = fmt.Errorf(msg)
	}
	if u.strictMode {
		return err
	}
	u.log.Error().Err(err).Msg(msg)
	return nil
}
