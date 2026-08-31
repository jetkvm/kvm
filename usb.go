package kvm

import (
	"fmt"
	"sync"
	"time"

	"github.com/jetkvm/kvm/internal/usbgadget"
)

var gadget *usbgadget.UsbGadget

func effectiveUsbDevices() *usbgadget.Devices {
	if config == nil || config.UsbDevices == nil {
		return nil
	}

	devices := *config.UsbDevices
	return &devices
}

func effectiveAudioEnabled() bool {
	return config != nil &&
		config.UsbDevices != nil &&
		config.AudioEnabled &&
		config.UsbDevices.Audio
}

// initUsbGadget initializes the USB gadget.
// call it only after the config is loaded.
func initUsbGadget() {
	gadget = usbgadget.NewUsbGadget(
		"jetkvm",
		effectiveUsbDevices(),
		config.UsbConfig,
		usbLogger,
	)

	setUSBRecoveryTimer(time.Now())

	if present, known := gadget.IsUsbHostPresent(); known {
		lastHostOK = present
	}

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
		if gadget.HasAbsoluteMouse() {
			return gadget.AbsMouseWheelReport(wheelY, wheelX)
		}
		return gadget.RelMouseWheelReport(wheelY, wheelX)
	})
}

func rpcWakeHost() error {
	if gadget == nil {
		return fmt.Errorf("USB gadget is not initialized")
	}

	state := gadget.GetUsbState()
	if !usbgadget.IsUSBStateAttached(state) {
		return nil
	}

	for i := 0; i < 3; i++ {
		if err := gadget.WakeReport(true); err != nil && !usbgadget.IsHIDTemporarilyUnavailableError(err) {
			return err
		}

		time.Sleep(50 * time.Millisecond)

		if err := gadget.WakeReport(false); err != nil && !usbgadget.IsHIDTemporarilyUnavailableError(err) {
			return err
		}

		time.Sleep(150 * time.Millisecond)
	}

	return nil
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

	usbEmulationDesired   = true
	lastUSBRecoveryTry    time.Time
	lastHostOK            bool
	sessionlessSoftCycles int
	correctiveRebinds     int
)

func usbReadyForHidReports() bool {
	usbStateLock.Lock()
	state := usbState
	usbStateLock.Unlock()
	return usbgadget.IsUSBStateAttached(state)
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
	shouldRecover := usbgadget.ShouldAttemptUSBRecovery(state, usbEmulationDesired, lastUSBRecoveryTry, now)
	if shouldRecover {
		lastUSBRecoveryTry = now
	}
	usbStateLock.Unlock()

	if !shouldRecover {
		return state
	}

	udcBound, _ := gadget.IsUDCBound()
	gadgetAttached := gadget.IsGadgetAttachedToUDC()

	if udcBound && gadgetAttached {
		hostPresent, hostKnown := gadget.IsUsbHostPresent()
		hostOK := hostKnown && hostPresent

		usbStateLock.Lock()
		hostAppeared := hostOK && !lastHostOK
		lastHostOK = hostOK
		sessionlessSoftCycles++
		escalate := hostOK && (sessionlessSoftCycles == 6 || sessionlessSoftCycles%60 == 0)
		usbStateLock.Unlock()

		if escalate {
			usbLogger.Warn().
				Int("soft_cycles", sessionlessSoftCycles).
				Msg("host present but no session after repeated soft reconnects; escalating to UDC rebind")
		}

		if !hostAppeared && !escalate {
			usbLogger.Debug().
				Bool("host_present", hostPresent).
				Bool("host_known", hostKnown).
				Msg("no USB host session; soft-reconnecting gadget")
			if err := gadget.SoftReconnect(); err == nil {
				return gadget.GetUsbState()
			}
			usbLogger.Warn().Msg("soft reconnect failed; falling back to UDC rebind")
		}
	}

	usbLogger.Warn().
		Bool("udc_bound", udcBound).
		Bool("gadget_attached", gadgetAttached).
		Msg("USB gadget is detached while USB emulation should be enabled; rebinding USB gadget")

	if err := gadget.RebindUsb(true); err != nil {
		usbLogger.Warn().Err(err).Msg("failed to recover USB gadget by rebinding USB device controller")
		return state
	}

	// Clear stale /dev/hidg* handles from the pre-rebind gadget instance.
	// The next write/open must use the newly recreated device nodes.
	gadget.ResetHIDFiles()

	if tryReopenKeyboard("udc_rebind") {
		return gadget.GetUsbState()
	}

	usbLogger.Warn().Msg("keyboard HID file not ready after UDC rebind; attempting full USB gadget reconfigure")

	if err := gadget.UpdateGadgetConfig(); err != nil {
		usbLogger.Warn().Err(err).Msg("failed to recover USB gadget with full gadget reconfigure")
		return gadget.GetUsbState()
	}
	gadget.ResetHIDFiles()

	if !tryReopenKeyboard("gadget_reconfigure") {
		usbLogger.Warn().Msg("keyboard HID file not ready after full USB recovery retry window")
	}

	return gadget.GetUsbState()
}

var usbRecoveryReopenDelays = []time.Duration{
	time.Second, time.Second, 2 * time.Second, 2 * time.Second,
	3 * time.Second, 3 * time.Second, 4 * time.Second, 4 * time.Second,
}

func tryReopenKeyboard(reason string) bool {
	for _, delay := range usbRecoveryReopenDelays {
		setUSBRecoveryTimer(time.Now())
		time.Sleep(delay)
		if err := gadget.ReopenKeyboardHidFile(); err == nil {
			usbLogger.Info().Str("reason", reason).Msg("keyboard HID file reopened successfully after USB recovery")
			return true
		}
	}
	return false
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

	attached := usbgadget.IsUSBStateAttached(newState)

	if newState == usbState {
		return
	}

	oldState := usbState
	usbState = newState
	usbLogger.Info().Str("from", oldState).Str("to", newState).Msg("USB state changed")

	if attached {
		openErr := gadget.OpenKeyboardHidFile()
		if openErr == nil {
			lastUSBRecoveryTry = time.Time{}
			sessionlessSoftCycles = 0
			correctiveRebinds = 0
		} else {
			now := time.Now()
			gate := usbgadget.USBRecoveryRetryInterval << min(correctiveRebinds, 6)
			if lastUSBRecoveryTry.IsZero() || now.Sub(lastUSBRecoveryTry) >= gate {
				correctiveRebinds++
				usbLogger.Warn().Err(openErr).Str("state", newState).Msg("HID chardev broken after state change, attempting corrective rebind")

				usbStateLock.Unlock()

				if err := rebindAndRecoverHID("hid-chardev-broken"); err != nil {
					usbLogger.Warn().Err(err).Msg("corrective rebind failed")
				}

				usbStateLock.Lock()
				usbState = gadget.GetUsbState()
			} else {
				usbState = oldState
				return
			}
		}
	}

	requestDisplayUpdate(false, "usb_state_changed")
	triggerUSBStateUpdate()
}
