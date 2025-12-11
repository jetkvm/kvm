package usbgadget

import (
	"errors"

	"github.com/jetkvm/kvm/internal/logging"
	"github.com/rs/zerolog"
)

func (u *UsbGadget) getUsbGadgetLogger() *zerolog.Logger {
	logger := logging.GetSubsystemLogger("usbgadget").
		With().
		Str("gadget", u.name).
		Logger()
	return &logger
}

func (u *UsbGadget) getHidKeyboardLogger() *zerolog.Logger {
	logger := logging.GetSubsystemLogger("hid-keyboard").
		With().
		Str("gadget", u.name).
		Logger()
	return &logger
}

func (u *UsbGadget) getHidKeyboardAutoReleaseLogger() *zerolog.Logger {
	logger := logging.GetSubsystemLogger("hid-keyboard-auto-release").
		With().
		Str("gadget", u.name).
		Logger()
	return &logger
}

func (u *UsbGadget) logWithSuppression(counterName string, every int, logger *zerolog.Logger, err error, msg string, args ...interface{}) bool {
	u.logSuppressionLock.Lock()
	counter, ok := u.logSuppressionCounter[counterName] // returns 0, false if not found
	counter++
	u.logSuppressionCounter[counterName] = counter
	u.logSuppressionLock.Unlock()

	// log if it's the first time, and then every N times thereafter
	if !ok || counter%every == 0 {
		logger.Error().Str("counterName", counterName).Int("counter", counter).Err(err).Msgf(msg, args...)
		return ok // return whether we've just exceeded the every interval
	}
	return false
}

func (u *UsbGadget) resetLogSuppressionCounter(counterName string) {
	u.logSuppressionLock.Lock()
	delete(u.logSuppressionCounter, counterName)
	u.logSuppressionLock.Unlock()
}

func (u *UsbGadget) logWarn(msg string, err error) error {
	if err == nil {
		err = errors.New(msg)
	}

	u.getUsbGadgetLogger().Warn().Err(err).Msg(msg)

	if u.strictMode {
		return err
	}

	return nil
}

func (u *UsbGadget) logError(msg string, err error) error {
	if err == nil {
		err = errors.New(msg)
	}

	u.getUsbGadgetLogger().Error().Err(err).Msg(msg)

	if u.strictMode {
		return err
	}

	return nil
}
