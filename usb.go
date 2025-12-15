package kvm

import (
	"sync"
	"time"

	"github.com/jetkvm/kvm/internal/logging"
	"github.com/jetkvm/kvm/internal/usbgadget"
)

var gadget *usbgadget.UsbGadget

// initUsbGadget initializes the USB gadget.
// call it only after the config is loaded.
func initUsbGadget() *usbgadget.UsbGadget {
	gadget = usbgadget.NewUsbGadget(
		"jetkvm",
		config.UsbDevices,
		config.UsbConfig,
	)

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

	go func() {
		for {
			// is the USB configured?
			if checkUSBState() {
				// ensure we have opened the keyboard hid file to listen for keyboard events
				if err := gadget.OpenKeyboardHidFile(); err != nil {
					logging.GetSubsystemLogger("usb").Error().Err(err).Msg("failed to open keyboard hid file")
					// but keep trying...
				}
			}
			time.Sleep(500 * time.Millisecond)
		}
	}()

	return gadget
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
)

func rpcGetUSBState() (state string) {
	return gadget.GetUsbState()
}

func triggerUSBStateUpdate() {
	go func() {
		if currentSession == nil {
			logging.GetSubsystemLogger("usb").Debug().Msg("No active RPC session, skipping USB state update")
			return
		}
		writeJSONRPCEvent("usbState", usbState, currentSession)
	}()
}

func checkUSBState() bool {
	usbStateLock.Lock()
	defer usbStateLock.Unlock()

	newState := gadget.GetUsbState()

	if newState != usbState {
		logging.GetSubsystemLogger("usb").Debug().Str("from", usbState).Str("to", newState).Msg("USB state changed")
		usbState = newState

		requestDisplayUpdate(true, "usb_state_changed")
		triggerUSBStateUpdate()
	}

	return newState == "configured"
}
