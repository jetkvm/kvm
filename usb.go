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

func rpcKeyboardReport(modifier byte, keys []byte) error {
	return gadget.KeyboardReport(modifier, keys)
}

func rpcKeypressReport(key byte, press bool) error {
	return gadget.KeypressReport(key, press)
}

func rpcAbsMouseReport(x int, y int, buttons uint8) error {
	return gadget.AbsMouseReport(x, y, buttons)
}

func rpcRelMouseReport(dx int8, dy int8, buttons uint8) error {
	return gadget.RelMouseReport(dx, dy, buttons)
}

func rpcWheelReport(wheelY int8) error {
	return gadget.AbsMouseWheelReport(wheelY)
}

func rpcGetKeyboardLedState() (state usbgadget.KeyboardState) {
	return gadget.GetKeyboardState()
}

func rpcGetKeysDownState() (state usbgadget.KeysDownState) {
	return gadget.GetKeysDownState()
}

var (
	usbState     = "unknown"
	usbStateLock sync.Mutex

	usbEmulationDesired = true
	lastUSBRecoveryTry  time.Time
)

const usbRecoveryRetryInterval = 5 * time.Second

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

func shouldAttemptUSBRecovery(state string, desired bool, lastAttempt time.Time, now time.Time) bool {
	if state != "not attached" || !desired {
		return false
	}

	return lastAttempt.IsZero() || now.Sub(lastAttempt) >= usbRecoveryRetryInterval
}

func attemptUSBRecovery(state string) string {
	now := time.Now()

	usbStateLock.Lock()
	desired := usbEmulationDesired
	lastAttempt := lastUSBRecoveryTry
	shouldRecover := shouldAttemptUSBRecovery(state, desired, lastAttempt, now)
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

	if err := gadget.ReopenKeyboardHidFile(); err != nil {
		usbLogger.Warn().Err(err).Msg("failed to reopen keyboard HID file after USB gadget rebind")
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
	if newState == "not attached" {
		newState = attemptUSBRecovery(newState)
	}

	usbStateLock.Lock()
	defer usbStateLock.Unlock()

	if newState == usbState {
		return
	}

	oldState := usbState
	usbState = newState
	usbLogger.Info().Str("from", oldState).Str("to", newState).Msg("USB state changed")

	if newState != "not attached" {
		if err := gadget.OpenKeyboardHidFile(); err != nil {
			usbLogger.Warn().Err(err).Str("state", newState).Msg("failed to ensure keyboard HID file is open after USB state change")
		}
	}

	requestDisplayUpdate(false, "usb_state_changed")
	triggerUSBStateUpdate()
}
