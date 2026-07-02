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
	if state == usbgadget.USBStateNotAttached || state == usbgadget.USBStateUnknown {
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

	usbEmulationDesired = true
	lastUSBRecoveryTry  time.Time
	// lastHidWriteRecoveryTry rate-limits write-timeout escalations separately:
	// lastUSBRecoveryTry is cleared on every loop iteration while the gadget is
	// attached, which would defeat rate limiting for a recovery that only runs
	// in the attached state.
	lastHidWriteRecoveryTry time.Time
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

// usbRecoveryReopenDelays spaces out attempts to reopen /dev/hidg0 after a
// rebind or reconfigure: the kernel recreates the chardevs but they take
// several seconds to become usable (ENXIO until the function driver attaches).
// Roughly 20 seconds total.
var usbRecoveryReopenDelays = []time.Duration{
	1 * time.Second,
	1 * time.Second,
	2 * time.Second,
	2 * time.Second,
	3 * time.Second,
	3 * time.Second,
	4 * time.Second,
	4 * time.Second,
}

// keyboardProbeFailureLimit bails out of the reopen ladder early once the
// keyboard chardev has repeatedly reopened but failed the write probe: the
// broken post-rebind state does not heal by waiting, only by escalating.
const keyboardProbeFailureLimit = 3

func tryReopenKeyboard(reason string) bool {
	probeFailures := 0
	for _, delay := range usbRecoveryReopenDelays {
		time.Sleep(delay)
		if err := gadget.ReopenKeyboardHidFile(); err != nil {
			continue
		}

		// Reopening is not proof of health: the #1512 broken state is a
		// chardev that opens fine while every write times out. While the host
		// is actively polling ("configured"), verify with a no-op report.
		if gadget.GetUsbState() != usbgadget.USBStateConfigured {
			// Host not polling yet (still enumerating, suspended, or off) — a
			// write probe would stall regardless of gadget health. Accept the
			// reopen; the runtime write-timeout streak remains as backstop.
			usbLogger.Info().Str("reason", reason).Msg("keyboard HID file reopened successfully after USB recovery")
			return true
		}

		if err := gadget.VerifyKeyboardWritable(); err != nil {
			probeFailures++
			usbLogger.Warn().Err(err).Str("reason", reason).Int("probe_failures", probeFailures).
				Msg("keyboard HID file reopened but not writable")
			if probeFailures >= keyboardProbeFailureLimit {
				return false
			}
			continue
		}

		usbLogger.Info().Str("reason", reason).Msg("keyboard HID file reopened and verified writable after USB recovery")
		return true
	}
	return false
}

// attemptHidWriteRecovery escalates to a full gadget reconfigure when keyboard
// HID writes keep timing out even though the UDC reports "configured". This is
// the aftermath of a UDC rebind that left the HID function broken: the chardev
// reopens fine, so the rebind recovery path declares success, but every write
// times out and is silently dropped (issue #1512). A plain rebind has already
// proven insufficient at this point, so go straight to the reconfigure that
// manual identifier cycling would otherwise trigger.
func attemptHidWriteRecovery(state string) string {
	if state != usbgadget.USBStateConfigured {
		// Write timeouts outside "configured" (e.g. host suspend) are expected;
		// don't let them accumulate into a spurious reconfigure right after the
		// state returns to "configured".
		gadget.ClearHidWriteTimeoutStreaks()
		return state
	}

	now := time.Now()

	usbStateLock.Lock()
	desired := usbEmulationDesired
	lastAttempt := lastHidWriteRecoveryTry
	usbStateLock.Unlock()

	timeouts := gadget.KeyboardWriteTimeoutStreak()
	if !usbgadget.ShouldEscalateHidWriteRecovery(state, desired, timeouts, lastAttempt, now) {
		return state
	}

	usbStateLock.Lock()
	lastHidWriteRecoveryTry = now
	usbStateLock.Unlock()

	usbLogger.Warn().
		Int("consecutive_timeouts", timeouts).
		Msg("keyboard HID writes are timing out while USB is configured; attempting full USB gadget reconfigure")

	if err := gadget.UpdateGadgetConfig(); err != nil {
		usbLogger.Warn().Err(err).Msg("failed to recover USB gadget with full gadget reconfigure")
		return gadget.GetUsbState()
	}
	gadget.ResetHIDFiles()

	if !tryReopenKeyboard("hid_write_timeout_reconfigure") {
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
	} else {
		newState = attemptHidWriteRecovery(newState)
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
		openErr := gadget.OpenKeyboardHidFile()
		if openErr != nil {
			usbLogger.Warn().Err(openErr).Str("state", newState).Msg("HID chardev broken after state change, attempting corrective rebind")

			lastUSBRecoveryTry = time.Now()
			usbStateLock.Unlock()

			gadget.ResetHIDFiles()
			if rebindErr := gadget.RebindUsb(true); rebindErr == nil {
				time.Sleep(1 * time.Second)
				_ = gadget.OpenKeyboardHidFile()
			}

			usbStateLock.Lock()
			usbState = gadget.GetUsbState()
			lastUSBRecoveryTry = time.Now()
		}
	}

	requestDisplayUpdate(false, "usb_state_changed")
	triggerUSBStateUpdate()
}
