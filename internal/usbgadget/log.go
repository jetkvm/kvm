package usbgadget

import (
	"github.com/jetkvm/kvm/internal/logging"
	"github.com/rs/zerolog"
)

func (u *UsbGadget) getLoggingContext() zerolog.Context {
	context := logging.GetSubsystemLogger("usbgadget").
		With().
		Str("gadget", u.name)
	return context
}

func (u *UsbGadget) getHidMouseLoggingContext() zerolog.Context {
	context := logging.GetSubsystemLogger("hid-mouse").
		With().
		Str("gadget", u.name)
	return context
}

func (u *UsbGadget) getHidKeyboardLoggingContext() zerolog.Context {
	context := logging.GetSubsystemLogger("hid-keyboard").
		With().
		Str("gadget", u.name)
	return context
}

func (u *UsbGadget) getHidKeyboardAutoReleaseLoggingContext() zerolog.Context {
	context := logging.GetSubsystemLogger("hid-keyboard-auto-release").
		With().
		Str("gadget", u.name)
	return context
}

func (u *UsbGadget) logWithSuppression(context zerolog.Context, counterName string, every int, err error, msg string, args ...any) bool {
	u.logSuppressionLock.Lock()
	counter, ok := u.logSuppressionCounter[counterName] // returns 0, false if not found
	counter++
	u.logSuppressionCounter[counterName] = counter
	u.logSuppressionLock.Unlock()

	// log if it's the first time, and then every N times thereafter
	if !ok || counter%every == 0 {
		_ = logging.LogError(context.Str("counterName", counterName).Int("counter", counter), err, msg, args...)
		return ok // return whether we've just exceeded the every interval
	}
	return false
}

func (u *UsbGadget) resetLogSuppressionCounter(counterName string) {
	u.logSuppressionLock.Lock()
	delete(u.logSuppressionCounter, counterName)
	u.logSuppressionLock.Unlock()
}
