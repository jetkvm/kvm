package kvm

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/jetkvm/kvm/internal/hal/usbgadget"
)

var gadget *usbgadget.UsbGadget

// call it only after the config is loaded.
func initUsbGadget() {
	cfg := loadCfg()
	gadget = usbgadget.NewUsbGadget(
		"jetkvm",
		cfg.UsbDevices,
		cfg.UsbConfig,
		usbLogger,
	)

	go func() {
		for {
			checkUSBState()
			time.Sleep(500 * time.Millisecond)
		}
	}()

	gadget.SetOnKeyboardStateChange(func(state usbgadget.KeyboardState) {
		if s := currentSession.Load(); s != nil {
			s.reportHidRPCKeyboardLedState(state)
		}
	})

	gadget.SetOnKeysDownChange(func(state usbgadget.KeysDownState) {
		if s := currentSession.Load(); s != nil {
			s.enqueueKeysDownState(state)
		}
	})

	gadget.SetOnKeepAliveReset(func() {
		if s := currentSession.Load(); s != nil {
			s.resetKeepAliveTime()
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

	// usbInitialTransitionDone distinguishes first-boot USB initialization
	// ("unknown" → "configured") from actual cable replugs. First Swap(true)
	// returns false (first boot), subsequent calls return true (real replug).
	usbInitialTransitionDone atomic.Bool

	// usbSelfTriggeredReset is set before programmatic UpdateGadgetConfig()
	// calls (setUsbDevices RPC, mass storage, audio recovery). This prevents
	// the resulting USB transition from being treated as a genuine cable replug.
	usbSelfTriggeredReset atomic.Bool
)

// MarkSelfTriggeredUSBReset marks the next USB configured transition as
// self-triggered (not a genuine cable replug). Call before UpdateGadgetConfig().
func MarkSelfTriggeredUSBReset() {
	usbSelfTriggeredReset.Store(true)
}

// resetUSBGadgetConfig marks the reset as self-triggered and reconfigures the USB gadget.
// This encapsulates the required mark-before-action protocol to prevent callers from
// forgetting MarkSelfTriggeredUSBReset(), which would misclassify the transition as a genuine replug.
func resetUSBGadgetConfig() error {
	MarkSelfTriggeredUSBReset()
	return gadget.UpdateGadgetConfig()
}

func getUsbState() string {
	usbStateLock.Lock()
	defer usbStateLock.Unlock()
	return usbState
}

func rpcGetUSBState() (state string) {
	return gadget.GetUsbState()
}

func triggerUSBStateUpdate() {
	state := getUsbState()
	go func() {
		s := currentSession.Load()
		if s == nil {
			usbLogger.Info().Msg("No active RPC session, skipping USB state update")
			return
		}
		writeJSONRPCEvent("usbState", state, s)
	}()
}

func checkUSBState() {
	usbStateLock.Lock()

	newState := gadget.GetUsbState()

	usbLogger.Trace().Str("old", usbState).Str("new", newState).Msg("Checking USB state")

	if newState == usbState {
		usbStateLock.Unlock()

		// Even when the UDC state hasn't changed, check for HID write failures.
		// When a USB cable is unplugged from the remote PC, the UDC may continue
		// reporting "configured" while all HID writes timeout. Detect this and
		// recover by closing stale file handles and reopening fresh ones.
		if newState == "configured" && gadget.TryRecoverHidFiles() {
			usbLogger.Warn().
				Int32("errors", gadget.GetConsecutiveWriteErrors()).
				Msg("HID write errors while USB configured, recovered file handles")
		}
		return
	}

	oldState := usbState
	usbState = newState
	usbStateLock.Unlock()

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

		// Determine if this is a genuine USB cable replug vs first boot or
		// programmatic reset (setUsbDevices, mass storage mount, etc).
		isFirstBoot := !usbInitialTransitionDone.Swap(true)
		selfTriggered := usbSelfTriggeredReset.Swap(false)
		isGenuineReplug := !isFirstBoot && !selfTriggered

		// Recover audio input (UAC1 ALSA device) after USB reconfiguration.
		// The ALSA device handle becomes stale after USB replug and must be reopened.
		go recoverAudioInputOnUSBReplug(isGenuineReplug)
	}

	requestDisplayUpdate(true, "usb_state_changed")
	triggerUSBStateUpdate()
}
