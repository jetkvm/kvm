package kvm

import (
	"sync"
	"time"

	"github.com/jetkvm/kvm/internal/usbgadget"
)

var gadget *usbgadget.UsbGadget

// initUsbGadget initializes the USB gadget.
// call it only after the config is loaded.
func initUsbGadget() {
	gadget = usbgadget.NewUsbGadget(
		"jetkvm",
		config.UsbDevices,
		config.UsbConfig,
		usbLogger,
	)

	setUSBRecoveryTimer(time.Now())

	go func() {
		for {
			checkUSBState()
			time.Sleep(500 * time.Millisecond)
		}
	}()

	gadget.SetOnKeyboardStateChange(func(state usbgadget.KeyboardState) {
		if currentSession != nil {
			currentSession.reportHidRPCKeyboardLedState(state)
		}
	})

	gadget.SetOnKeysDownChange(func(state usbgadget.KeysDownState) {
		if currentSession != nil {
			currentSession.enqueueKeysDownState(state)
		}
	})

	gadget.SetOnKeepAliveReset(func() {
		if currentSession != nil {
			currentSession.resetKeepAliveTime()
		}
	})

	// open the keyboard hid file to listen for keyboard events
	if err := gadget.OpenKeyboardHidFile(); err != nil {
		usbLogger.Error().Err(err).Msg("failed to open keyboard hid file")
	}
}

// rpcHidReport wraps a HID gadget call with the common guard (skip if USB not
// ready) and error suppression (swallow transient HID errors during rebind).
func rpcHidReport(fn func() error) error {
	if !usbReadyForHidReports() {
		return nil
	}
	if err := fn(); err != nil && !usbgadget.IsHIDTemporarilyUnavailableError(err) {
		return err
	}
	return nil
}

func rpcKeyboardReport(modifier byte, keys []byte) error {
	return rpcHidReport(func() error { return gadget.KeyboardReport(modifier, keys) })
}

func rpcKeypressReport(key byte, press bool) error {
	return rpcHidReport(func() error { return gadget.KeypressReport(key, press) })
}

func rpcAbsMouseReport(x int, y int, buttons uint8) error {
	return rpcHidReport(func() error { return gadget.AbsMouseReport(x, y, buttons) })
}

func rpcRelMouseReport(dx int8, dy int8, buttons uint8) error {
	return rpcHidReport(func() error { return gadget.RelMouseReport(dx, dy, buttons) })
}

func rpcWheelReport(wheelY int8, wheelX int8) error {
	return rpcHidReport(func() error {
		if err := gadget.AbsMouseWheelReport(wheelY, wheelX); err != nil {
			return err
		}
		return gadget.RelMouseWheelReport(wheelY, wheelX)
	})
}

func rpcGetKeyboardLedState() (state usbgadget.KeyboardState) {
	return gadget.GetKeyboardState()
}

func rpcGetKeysDownState() (state usbgadget.KeysDownState) {
	return gadget.GetKeysDownState()
}

var (
	usbState     = usbgadget.USBStateUnknown
	usbStateLock sync.Mutex

	usbEmulationDesired = true
	lastUSBRecoveryTry  time.Time
)

func usbReadyForHidReports() bool {
	usbStateLock.Lock()
	state := usbState
	usbStateLock.Unlock()
	return state != usbgadget.USBStateNotAttached && state != usbgadget.USBStateUnknown
}

func rpcGetUSBState() (state string) {
	return gadget.GetUsbState()
}

func setUSBEmulationDesired(enabled bool) {
	usbStateLock.Lock()
	defer usbStateLock.Unlock()

	usbEmulationDesired = enabled
}

func setUSBRecoveryTimer(lastAttempt time.Time) {
	usbStateLock.Lock()
	defer usbStateLock.Unlock()

	lastUSBRecoveryTry = lastAttempt
}

func attemptUSBRecovery(state string) string {
	now := time.Now()

	usbStateLock.Lock()
	desired := usbEmulationDesired
	lastAttempt := lastUSBRecoveryTry
	shouldRecover := usbgadget.ShouldAttemptUSBRecovery(state, desired, lastAttempt, now)
	if shouldRecover {
		lastUSBRecoveryTry = now
	}
	usbStateLock.Unlock()

	if !shouldRecover {
		return state
	}

	usbLogger.Warn().Msg("USB gadget is detached while USB emulation should be enabled; rebinding USB gadget")

	if err := gadget.RebindUsb(true); err != nil {
		usbLogger.Warn().Err(err).Msg("failed to recover USB gadget by rebinding USB device controller")
		return state
	}

	// Clear stale /dev/hidg* handles from the pre-rebind gadget instance.
	// The next write/open must use the newly recreated device nodes.
	gadget.ResetHIDFiles()

	// After rebind, the kernel recreates /dev/hidg* but the character
	// devices take several seconds to become usable (ENXIO until the
	// function driver attaches). Retry the keyboard HID file open with
	// increasing delays up to ~20 seconds total.
	delays := []time.Duration{
		1 * time.Second,
		1 * time.Second,
		2 * time.Second,
		2 * time.Second,
		3 * time.Second,
		3 * time.Second,
		4 * time.Second,
		4 * time.Second,
	}
	tryReopenKeyboard := func(openDelays []time.Duration, reason string) bool {
		for _, delay := range openDelays {
			time.Sleep(delay)
			if err := gadget.ReopenKeyboardHidFile(); err == nil {
				usbLogger.Info().Str("reason", reason).Msg("keyboard HID file reopened successfully after USB recovery")
				return true
			}
		}
		return false
	}

	if tryReopenKeyboard(delays, "udc_rebind") {
		return gadget.GetUsbState()
	}

	usbLogger.Warn().Msg("keyboard HID file not ready after UDC rebind; attempting full USB gadget reconfigure")

	if err := gadget.UpdateGadgetConfig(); err != nil {
		usbLogger.Warn().Err(err).Msg("failed to recover USB gadget with full gadget reconfigure")
		return gadget.GetUsbState()
	}
	gadget.ResetHIDFiles()

	if !tryReopenKeyboard(delays, "gadget_reconfigure") {
		usbLogger.Warn().Msg("keyboard HID file not ready after full USB recovery retry window")
	}

	return gadget.GetUsbState()
}

func triggerUSBStateUpdate() {
	go func() {
		if currentSession == nil {
			usbLogger.Info().Msg("No active RPC session, skipping USB state update")
			return
		}
		writeJSONRPCEvent("usbState", usbState, currentSession)
	}()
}

func checkUSBState() {
	newState := gadget.GetUsbState()
	if newState == usbgadget.USBStateNotAttached {
		newState = attemptUSBRecovery(newState)
	}

	usbStateLock.Lock()
	defer usbStateLock.Unlock()

	if newState != usbgadget.USBStateNotAttached {
		// Once USB is attached again, clear recovery rate limiting so any future
		// detach can be recovered immediately.
		lastUSBRecoveryTry = time.Time{}
	}

	if newState == usbState {
		return
	}

	oldState := usbState
	usbState = newState
	usbLogger.Info().Str("from", oldState).Str("to", newState).Msg("USB state changed")

	if newState != usbgadget.USBStateNotAttached {
		if err := gadget.OpenKeyboardHidFile(); err != nil {
			usbLogger.Warn().Err(err).Str("state", newState).Msg("failed to ensure keyboard HID file is open after USB state change")
		}
	}

	requestDisplayUpdate(false, "usb_state_changed")
	triggerUSBStateUpdate()
}
