package kvm

import (
	"sync"
	"time"

	"github.com/jetkvm/kvm/internal/usbgadget"
)

var gadget *usbgadget.UsbGadget

// call it only after the config is loaded.
func initUsbGadget() {
	gadget = usbgadget.NewUsbGadget(
		"jetkvm",
		config.UsbDevices,
		config.UsbConfig,
		usbLogger,
	)

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

	if err := gadget.OpenKeyboardHidFile(); err != nil {
		usbLogger.Error().Err(err).Msg("failed to open keyboard hid file")
	}

	// Initialize camera manager after gadget is ready
	initCameraManager()
}

func rpcKeyboardReport(modifier byte, keys []byte) error {
	return gadget.KeyboardReport(modifier, keys)
}

func rpcKeypressReport(key byte, press bool) error {
	// Block keyboard input while paste is in progress (paste uses rpcKeyboardReport directly)
	if isKeyboardMacroInProgress() {
		return nil // Silently drop - not an error, just blocked
	}
	return gadget.KeypressReport(key, press)
}

func rpcAbsMouseReport(x int, y int, buttons uint8) error {
	// Block mouse input while paste is in progress
	if isKeyboardMacroInProgress() {
		return nil // Silently drop
	}
	return gadget.AbsMouseReport(x, y, buttons)
}

func rpcRelMouseReport(dx int8, dy int8, buttons uint8) error {
	// Block mouse input while paste is in progress
	if isKeyboardMacroInProgress() {
		return nil // Silently drop
	}
	return gadget.RelMouseReport(dx, dy, buttons)
}

func rpcWheelReport(wheelY int8, wheelX int8) error {
	// Block scroll input while paste is in progress
	if isKeyboardMacroInProgress() {
		return nil // Silently drop
	}
	logger.Debug().Int8("wheelY", wheelY).Int8("wheelX", wheelX).Msg("rpcWheelReport called")
	return gadget.AbsMouseWheelReport(wheelY, wheelX)
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
			usbLogger.Info().Msg("No active RPC session, skipping USB state update")
			return
		}
		writeJSONRPCEvent("usbState", usbState, currentSession)
	}()
}

func checkUSBState() {
	usbStateLock.Lock()
	defer usbStateLock.Unlock()

	newState := gadget.GetUsbState()

	usbLogger.Trace().Str("old", usbState).Str("new", newState).Msg("Checking USB state")

	if newState == usbState {
		return
	}

	oldState := usbState
	usbState = newState
	usbLogger.Info().Str("from", oldState).Str("to", newState).Msg("USB state changed")

	if oldState == "configured" && newState != "configured" {
		usbLogger.Info().Msg("USB deconfigured, closing HID files")
		gadget.CloseHidFiles()
	}

	if newState == "configured" && oldState != "configured" {
		usbLogger.Info().Msg("USB configured, reopening HID files")
		gadget.CloseHidFiles()
		gadget.PreOpenHidFiles()
		if err := gadget.OpenKeyboardHidFile(); err != nil {
			usbLogger.Error().Err(err).Msg("failed to reopen keyboard hid file")
		}

		// Reinitialize UVC after USB reconfiguration
		// The UVC video device may have been recreated with a different path
		go reinitUVC()
	}

	requestDisplayUpdate(true, "usb_state_changed")
	triggerUSBStateUpdate()
}
