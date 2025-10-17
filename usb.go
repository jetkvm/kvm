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

	go func() {
		for {
			checkUSBState()
			time.Sleep(500 * time.Millisecond)
		}
	}()

	gadget.SetOnKeyboardStateChange(func(state usbgadget.KeyboardState) {
		// Check if keystrokes should be private
		if currentSessionSettings != nil && currentSessionSettings.PrivateKeystrokes {
			// Report to primary session only
			if primary := sessionManager.GetPrimarySession(); primary != nil {
				primary.reportHidRPCKeyboardLedState(state)
			}
		} else {
			// Report to all authorized sessions (primary and observers, but not pending)
			sessionManager.ForEachSession(func(s *Session) {
				if s.Mode == SessionModePrimary || s.Mode == SessionModeObserver {
					s.reportHidRPCKeyboardLedState(state)
				}
			})
		}
	})

	gadget.SetOnKeysDownChange(func(state usbgadget.KeysDownState) {
		// Check if keystrokes should be private
		if currentSessionSettings != nil && currentSessionSettings.PrivateKeystrokes {
			// Report to primary session only
			if primary := sessionManager.GetPrimarySession(); primary != nil {
				primary.enqueueKeysDownState(state)
			}
		} else {
			// Report to all authorized sessions (primary and observers, but not pending)
			sessionManager.ForEachSession(func(s *Session) {
				if s.Mode == SessionModePrimary || s.Mode == SessionModeObserver {
					s.enqueueKeysDownState(state)
				}
			})
		}
	})

	gadget.SetOnKeepAliveReset(func() {
		// Reset keep-alive for primary session
		if primary := sessionManager.GetPrimarySession(); primary != nil {
			primary.resetKeepAliveTime()
		}
	})

	// open the keyboard hid file to listen for keyboard events
	if err := gadget.OpenKeyboardHidFile(); err != nil {
		usbLogger.Error().Err(err).Msg("failed to open keyboard hid file")
	}
}

func (s *Session) rpcKeyboardReport(modifier byte, keys []byte) error {
	if s == nil || !s.HasPermission(PermissionKeyboardInput) {
		return ErrPermissionDeniedKeyboard
	}
	sessionManager.UpdateLastActive(s.ID)
	return gadget.KeyboardReport(modifier, keys)
}

func (s *Session) rpcKeypressReport(key byte, press bool) error {
	if s == nil || !s.HasPermission(PermissionKeyboardInput) {
		return ErrPermissionDeniedKeyboard
	}
	sessionManager.UpdateLastActive(s.ID)
	return gadget.KeypressReport(key, press)
}

func (s *Session) rpcAbsMouseReport(x int, y int, buttons uint8) error {
	if s == nil || !s.HasPermission(PermissionMouseInput) {
		return ErrPermissionDeniedMouse
	}
	sessionManager.UpdateLastActive(s.ID)
	return gadget.AbsMouseReport(x, y, buttons)
}

func (s *Session) rpcRelMouseReport(dx int8, dy int8, buttons uint8) error {
	if s == nil || !s.HasPermission(PermissionMouseInput) {
		return ErrPermissionDeniedMouse
	}
	sessionManager.UpdateLastActive(s.ID)
	return gadget.RelMouseReport(dx, dy, buttons)
}

func (s *Session) rpcWheelReport(wheelY int8) error {
	if s == nil || !s.HasPermission(PermissionMouseInput) {
		return ErrPermissionDeniedMouse
	}
	sessionManager.UpdateLastActive(s.ID)
	return gadget.AbsMouseWheelReport(wheelY)
}

// RPC functions that route to the primary session
func rpcKeyboardReport(modifier byte, keys []byte) error {
	if primary := sessionManager.GetPrimarySession(); primary != nil {
		return primary.rpcKeyboardReport(modifier, keys)
	}
	return ErrNotPrimarySession
}

func rpcKeypressReport(key byte, press bool) error {
	if primary := sessionManager.GetPrimarySession(); primary != nil {
		return primary.rpcKeypressReport(key, press)
	}
	return ErrNotPrimarySession
}

func rpcAbsMouseReport(x int, y int, buttons uint8) error {
	if primary := sessionManager.GetPrimarySession(); primary != nil {
		return primary.rpcAbsMouseReport(x, y, buttons)
	}
	return ErrNotPrimarySession
}

func rpcRelMouseReport(dx int8, dy int8, buttons uint8) error {
	if primary := sessionManager.GetPrimarySession(); primary != nil {
		return primary.rpcRelMouseReport(dx, dy, buttons)
	}
	return ErrNotPrimarySession
}

func rpcWheelReport(wheelY int8) error {
	if primary := sessionManager.GetPrimarySession(); primary != nil {
		return primary.rpcWheelReport(wheelY)
	}
	return ErrNotPrimarySession
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
		broadcastJSONRPCEvent("usbState", usbState)
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

	usbState = newState
	usbLogger.Info().Str("from", usbState).Str("to", newState).Msg("USB state changed")

	requestDisplayUpdate(true, "usb_state_changed")
	triggerUSBStateUpdate()
}
