package usbgadget

import (
	"github.com/jetkvm/kvm/internal/logging"
)

func (u *UsbGadget) getUsbGadgetLoggingContext() *logging.Context {
	return logging.GetSubsystemLogger("usbgadget").
		Str("gadget", u.name)
}

func (u *UsbGadget) getHidKeyboardLoggingContext() *logging.Context {
	return logging.GetSubsystemLogger("hid-keyboard").
		Str("gadget", u.name)
}

func (u *UsbGadget) getHidKeyboardAutoReleaseLoggingContext() *logging.Context {
	return logging.GetSubsystemLogger("hid-keyboard-auto-release").
		Str("gadget", u.name)
}

func (u *UsbGadget) logWithSuppression(context *logging.Context, counterName string, every int, err error, msg string, args ...interface{}) bool {
	u.logSuppressionLock.Lock()
	counter, ok := u.logSuppressionCounter[counterName] // returns 0, false if not found
	counter++
	u.logSuppressionCounter[counterName] = counter
	u.logSuppressionLock.Unlock()

	// log if it's the first time, and then every N times thereafter
	if !ok || counter%every == 0 {
		context.Str("counterName", counterName).Int("counter", counter).Err(err).Error().Msgf(msg, args...)
		return ok // return whether we've just exceeded the every interval
	}
	return false
}

func (u *UsbGadget) resetLogSuppressionCounter(counterName string) {
	u.logSuppressionLock.Lock()
	delete(u.logSuppressionCounter, counterName)
	u.logSuppressionLock.Unlock()
}
