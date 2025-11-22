package usbgadget

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/jetkvm/kvm/internal/logging"
	"github.com/rs/xid"
	"github.com/rs/zerolog"
)

var keyboardConfig = gadgetConfigItem{
	order:      1000,
	device:     "hid.usb0",
	path:       []string{"functions", "hid.usb0"},
	configPath: []string{"hid.usb0"},
	attrs: gadgetAttributes{
		"protocol":        "1",
		"subclass":        "1",
		"report_length":   "8",
		"no_out_endpoint": "0",
	},
	reportDesc: keyboardReportDesc,
}

// Source: https://www.kernel.org/doc/Documentation/usb/gadget_hid.txt
var keyboardReportDesc = []byte{
	0x05, 0x01, /* USAGE_PAGE (Generic Desktop)	          */
	0x09, 0x06, /* USAGE (Keyboard)                       */
	0xa1, 0x01, /* COLLECTION (Application)               */
	0x05, 0x07, /*   USAGE_PAGE (Keyboard)                */
	0x19, 0xe0, /*   USAGE_MINIMUM (Keyboard LeftControl) */
	0x29, 0xe7, /*   USAGE_MAXIMUM (Keyboard Right GUI)   */
	0x15, 0x00, /*   LOGICAL_MINIMUM (0)                  */
	0x25, 0x01, /*   LOGICAL_MAXIMUM (1)                  */
	0x75, 0x01, /*   REPORT_SIZE (1)                      */
	0x95, 0x08, /*   REPORT_COUNT (8)                     */
	0x81, 0x02, /*   INPUT (Data,Var,Abs)                 */
	0x95, 0x01, /*   REPORT_COUNT (1)                     */
	0x75, 0x08, /*   REPORT_SIZE (8)                      */
	0x81, 0x03, /*   INPUT (Cnst,Var,Abs)                 */
	0x95, 0x05, /*   REPORT_COUNT (5)                     */
	0x75, 0x01, /*   REPORT_SIZE (1)                      */

	0x05, 0x08, /*   USAGE_PAGE (LEDs)                    */
	0x19, 0x01, /*   USAGE_MINIMUM (Num Lock)             */
	0x29, 0x05, /*   USAGE_MAXIMUM (Kana)                 */
	0x91, 0x02, /*   OUTPUT (Data,Var,Abs)                */
	0x95, 0x01, /*   REPORT_COUNT (1)                     */
	0x75, 0x03, /*   REPORT_SIZE (3)                      */
	0x91, 0x03, /*   OUTPUT (Cnst,Var,Abs)                */
	0x95, 0x06, /*   REPORT_COUNT (6)                     */
	0x75, 0x08, /*   REPORT_SIZE (8)                      */
	0x15, 0x00, /*   LOGICAL_MINIMUM (0)                  */
	0x25, 0x65, /*   LOGICAL_MAXIMUM (101)                */
	0x05, 0x07, /*   USAGE_PAGE (Keyboard)                */
	0x19, 0x00, /*   USAGE_MINIMUM (Reserved)             */
	0x29, 0x65, /*   USAGE_MAXIMUM (Keyboard Application) */
	0x81, 0x00, /*   INPUT (Data,Ary,Abs)                 */
	0xc0, /* END_COLLECTION                         */
}

const (
	hidReadBufferSize = 8
	hidKeyBufferSize  = 6
	hidErrorRollOver  = 0x01
	// https://www.usb.org/sites/default/files/documents/hid1_11.pdf
	// https://www.usb.org/sites/default/files/hut1_2.pdf
	KeyboardLedMaskNumLock    = 1 << 0
	KeyboardLedMaskCapsLock   = 1 << 1
	KeyboardLedMaskScrollLock = 1 << 2
	KeyboardLedMaskCompose    = 1 << 3
	KeyboardLedMaskKana       = 1 << 4
	// power on/off LED is 5
	KeyboardLedMaskShift  = 1 << 6
	ValidKeyboardLedMasks = KeyboardLedMaskNumLock | KeyboardLedMaskCapsLock | KeyboardLedMaskScrollLock | KeyboardLedMaskCompose | KeyboardLedMaskKana | KeyboardLedMaskShift
)

// Synchronization between LED states and CAPS LOCK, NUM LOCK, SCROLL LOCK,
// COMPOSE, and KANA events is maintained by the host and NOT the keyboard. If
// using the keyboard descriptor in Appendix B, LED states are set by sending a
// 5-bit absolute report to the keyboard via a Set_Report(Output) request.
type KeyboardState struct {
	NumLock    bool `json:"num_lock"`
	CapsLock   bool `json:"caps_lock"`
	ScrollLock bool `json:"scroll_lock"`
	Compose    bool `json:"compose"`
	Kana       bool `json:"kana"`
	Shift      bool `json:"shift"` // This is not part of the main USB HID spec
	raw        byte
}

func (s KeyboardState) MarshalZerologObject(e *zerolog.Event) {
	e.Hex("State", []byte{s.raw})
}

// Byte returns the raw byte representation of the keyboard state.
func (k *KeyboardState) Byte() byte {
	return k.raw
}

func getKeyboardState(b byte) KeyboardState {
	// should we check if it's the correct usage page?
	return KeyboardState{
		NumLock:    b&KeyboardLedMaskNumLock != 0,
		CapsLock:   b&KeyboardLedMaskCapsLock != 0,
		ScrollLock: b&KeyboardLedMaskScrollLock != 0,
		Compose:    b&KeyboardLedMaskCompose != 0,
		Kana:       b&KeyboardLedMaskKana != 0,
		Shift:      b&KeyboardLedMaskShift != 0,
		raw:        b,
	}
}

func (u *UsbGadget) updateKeyboardState(state byte) {
	u.keyboardStateLock.Lock()
	defer u.keyboardStateLock.Unlock()

	context := u.getHidKeyboardLoggingContext().Uint8("state", state)

	if state&^ValidKeyboardLedMasks != 0 {
		logging.LogWarn(context, "ignoring invalid bits")
		state &= ValidKeyboardLedMasks
	}

	context = context.Uint8("old_state", u.keyboardState)

	if u.keyboardState == state {
		logging.LogTrace(context, "unchanged keyboardState")
		return
	}

	u.keyboardState = state
	logging.LogTrace(context, "updated keyboardState")

	if u.onKeyboardStateChange != nil {
		logging.LogTrace(context, "calling onKeyboardStateChange callback")
		(*u.onKeyboardStateChange)(getKeyboardState(state))
	}
}

func (u *UsbGadget) SetOnKeyboardStateChange(f func(state KeyboardState)) {
	u.onKeyboardStateChange = &f
}

func (u *UsbGadget) GetKeyboardState() KeyboardState {
	u.keyboardStateLock.Lock()
	defer u.keyboardStateLock.Unlock()

	return getKeyboardState(u.keyboardState)
}

func (u *UsbGadget) GetKeysDownState() KeysDownState {
	u.keyboardStateLock.Lock()
	defer u.keyboardStateLock.Unlock()

	return u.keysDownState
}

func (u *UsbGadget) ResetRollover() {
	u.keyboardStateLock.Lock()
	defer u.keyboardStateLock.Unlock()

	if u.keysDownState.Keys[0] == hidErrorRollOver {
		for i := range u.keysDownState.Keys {
			u.keysDownState.Keys[i] = 0
		}
	}
}

func (u *UsbGadget) SetOnKeysDownChange(f func(state KeysDownState)) {
	u.onKeysDownChange = &f
}

func (u *UsbGadget) SetOnKeepAliveReset(f func()) {
	u.onKeepAliveReset = &f
}

// DefaultAutoReleaseDuration is the default duration for auto-release of a key.
const DefaultAutoReleaseDuration = 100 * time.Millisecond

func (u *UsbGadget) scheduleAutoRelease(key byte) {
	context := u.getHidKeyboardLoggingContext().Uint8("key", key)

	u.kbdAutoReleaseLock.Lock()
	defer logging.UnlockWithTraceLog(context, &u.kbdAutoReleaseLock, "autoRelease scheduled")

	if u.kbdAutoReleaseTimers[key] != nil {
		logging.LogTrace(context, "deleting previous autoRelease timer")
		u.kbdAutoReleaseTimers[key].Stop()
		delete(u.kbdAutoReleaseTimers, key)
	}

	// We currently hardcode the duration to 100ms
	// However, it should be the same as the duration of the keep-alive reset called baseExtension.
	u.kbdAutoReleaseTimers[key] = time.AfterFunc(100*time.Millisecond, func() {
		u.performAutoRelease(key)
	})
}

func (u *UsbGadget) cancelAutoRelease(key byte) {
	context := u.getHidKeyboardLoggingContext().Uint8("key", key)

	u.kbdAutoReleaseLock.Lock()
	defer logging.UnlockWithTraceLog(context, &u.kbdAutoReleaseLock, "autoRelease cancelled")

	if timer := u.kbdAutoReleaseTimers[key]; timer != nil {
		logging.LogTrace(context, "deleting cancelled autoRelease timer")
		timer.Stop()
		delete(u.kbdAutoReleaseTimers, key)

		// Reset keep-alive timing when key is released
		if u.onKeepAliveReset != nil {
			logging.LogTrace(context, "calling onKeepAliveReset callback")
			(*u.onKeepAliveReset)()
		}
	}
}

func (u *UsbGadget) DelayAutoReleaseWithDuration(resetDuration time.Duration) {
	context := u.getHidKeyboardLoggingContext().Dur("reset_duration", resetDuration)

	u.kbdAutoReleaseLock.Lock()
	defer logging.UnlockWithTraceLog(context, &u.kbdAutoReleaseLock, "autoRelease delayed")

	logging.LogTrace(context, "delaying auto-release with dynamic duration")

	for _, timer := range u.kbdAutoReleaseTimers {
		if timer != nil {
			timer.Reset(resetDuration)
		}
	}
}

func (u *UsbGadget) performAutoRelease(key byte) {
	context := u.getHidKeyboardLoggingContext().Uint8("key", key)

	u.kbdAutoReleaseLock.Lock()

	if timer, ok := u.kbdAutoReleaseTimers[key]; !ok || timer == nil {
		logging.LogWarn(context, "autoRelease timer not found")
		u.kbdAutoReleaseLock.Unlock()
		return
	} else {
		logging.LogTrace(context, "deleting expired autoRelease timer")
		timer.Stop()
		delete(u.kbdAutoReleaseTimers, key)
		u.kbdAutoReleaseLock.Unlock()
	}

	// Skip if already released
	state := u.GetKeysDownState()
	alreadyReleased := true

	for i := range state.Keys {
		if state.Keys[i] == key {
			alreadyReleased = false
			break
		}
	}

	if alreadyReleased {
		logging.LogTrace(context, "already released")
		return
	}

	_, err := u.keypressReport(context, key, false)
	if err != nil {
		logging.LogWarn(context, "failed to release key")
	}
}

func (u *UsbGadget) listenKeyboardEvents() {
	if u.keyboardHidFile == nil {
		return
	}

	go func() {
		buf := make([]byte, hidReadBufferSize)
		for {
			path := u.keyboardHidFile.Name()
			context := u.getHidKeyboardLoggingContext().Str("path", path).Str("listener", "keyboardEvents")
			select {
			case <-u.keyboardStateCtx.Done():
				logging.LogInfo(context, "context done")
				return
			default:
				if u.keyboardHidFile == nil {
					logging.LogWarn(context, "keyboardHidFile is nil, exiting listener")
					return
				}

				logging.LogTrace(context, "reading from keyboard for LED state changes")
				n, err := u.keyboardHidFile.Read(buf)
				if err != nil {
					if errors.Is(err, io.EOF) {
						logging.LogTrace(context, "nothing read")
						time.Sleep(50 * time.Millisecond)
						continue
					}

					if u.logWithSuppression(context, "keyboardHidFileRead", 10, err, "failed to read") {
						// after too many consecutive errors, let's close and re-open the file
						u.keyboardHidFile.Close()
						u.keyboardHidFile = nil
						return
					} else {
						time.Sleep(5 * time.Millisecond)
						continue
					}
				}
				u.resetLogSuppressionCounter("keyboardHidFileRead")

				context := context.Int("n", n).Uints8("buf", buf)
				logging.LogTrace(context, "got data from keyboard")
				if n != 1 {
					logging.LogWarn(context, "expected 1 byte")
					continue
				}
				u.updateKeyboardState(buf[0])
				time.Sleep(10 * time.Millisecond)
			}
		}
	}()
}

var keyboardHidFileLock sync.Mutex

func (u *UsbGadget) OpenKeyboardHidFile() error {
	keyboardHidFileLock.Lock()
	defer keyboardHidFileLock.Unlock()

	return u.openKeyboardHidFileUnderMutex()
}

func (u *UsbGadget) openKeyboardHidFileUnderMutex() error {
	if u.keyboardHidFile != nil {
		return nil
	}

	if keyboardFile, err := os.OpenFile("/dev/hidg0", os.O_RDWR, 0666); err == nil {
		u.keyboardHidFile = keyboardFile
	} else {
		return fmt.Errorf("failed to open keyboard on hidg0: %w", err)
	}

	if u.keyboardStateCancel != nil {
		u.keyboardStateCancel()
		u.keyboardStateCancel = nil
	}

	u.keyboardStateCtx, u.keyboardStateCancel = context.WithCancel(context.Background())
	u.listenKeyboardEvents()

	return nil
}

func (u *UsbGadget) keyboardWriteHidFile(modifier byte, keys []byte) error {
	keyboardHidFileLock.Lock()
	defer keyboardHidFileLock.Unlock()

	if err := u.openKeyboardHidFileUnderMutex(); err != nil {
		return err
	}

	context := u.getHidKeyboardLoggingContext()

	_, err := u.writeWithTimeout(u.keyboardHidFile, append([]byte{modifier, 0x00}, keys[:hidKeyBufferSize]...))
	if err != nil {
		u.logWithSuppression(context.Uint8("modifier", modifier).Uints8("keys", keys), "keyboardWriteHidFile", 10, err, "failed to write keyboard")
		u.keyboardHidFile.Close()
		u.keyboardHidFile = nil
		return err
	}
	u.resetLogSuppressionCounter("keyboardWriteHidFile")
	return nil
}

func (u *UsbGadget) UpdateKeysDown(modifier byte, keys []byte) KeysDownState {
	state := KeysDownState{
		Modifier: modifier,
		Keys:     []byte(keys[:]),
	}

	u.keyboardStateLock.Lock()

	if u.keysDownState.Modifier == state.Modifier &&
		bytes.Equal(u.keysDownState.Keys, state.Keys) {
		u.keyboardStateLock.Unlock()
		return state // No change in key down state
	}

	u.keysDownState = state
	u.keyboardStateLock.Unlock()

	if u.onKeysDownChange != nil {
		(*u.onKeysDownChange)(state) // this enqueues to the outgoing hidrpc queue via usb.go → currentSession.enqueueKeysDownState(...)
	}
	return state
}

func (u *UsbGadget) KeyboardReport(modifier byte, keys []byte) error {
	defer u.resetUserInputTime()

	if len(keys) > hidKeyBufferSize {
		keys = keys[:hidKeyBufferSize]
	}
	if len(keys) < hidKeyBufferSize {
		keys = append(keys, make([]byte, hidKeyBufferSize-len(keys))...)
	}

	err := u.keyboardWriteHidFile(modifier, keys)
	if err != nil {
		logging.LogWarn(u.getHidKeyboardLoggingContext().Uint8("modifier", modifier).Uints8("keys", keys), "Could not write keyboard report")
	}

	u.UpdateKeysDown(modifier, keys)
	defer u.ResetRollover()
	return err
}

const (
	// https://www.usb.org/sites/default/files/documents/hut1_2.pdf
	// Dynamic Flags (DV)
	LeftControl  = 0xE0
	LeftShift    = 0xE1
	LeftAlt      = 0xE2
	LeftSuper    = 0xE3 // Left GUI (e.g. Windows key, Apple Command key)
	RightControl = 0xE4
	RightShift   = 0xE5
	RightAlt     = 0xE6
	RightSuper   = 0xE7 // Right GUI (e.g. Windows key, Apple Command key)
)

const (
	// https://www.usb.org/sites/default/files/documents/hid1_11.pdf Appendix C
	ModifierMaskLeftControl  = 0x01
	ModifierMaskRightControl = 0x10
	ModifierMaskLeftShift    = 0x02
	ModifierMaskRightShift   = 0x20
	ModifierMaskLeftAlt      = 0x04
	ModifierMaskRightAlt     = 0x40
	ModifierMaskLeftSuper    = 0x08
	ModifierMaskRightSuper   = 0x80
)

// KeyCodeToMaskMap is a slice of KeyCodeMask for quick lookup
var KeyCodeToMaskMap = map[byte]byte{
	LeftControl:  ModifierMaskLeftControl,
	LeftShift:    ModifierMaskLeftShift,
	LeftAlt:      ModifierMaskLeftAlt,
	LeftSuper:    ModifierMaskLeftSuper,
	RightControl: ModifierMaskRightControl,
	RightShift:   ModifierMaskRightShift,
	RightAlt:     ModifierMaskRightAlt,
	RightSuper:   ModifierMaskRightSuper,
}

func (u *UsbGadget) keypressReport(context zerolog.Context, key byte, press bool) (KeysDownState, error) {
	defer u.resetUserInputTime()
	context = context.Uint8("key", key).Bool("press", press)

	if context.Logger().GetLevel() <= zerolog.DebugLevel {
		requestID := xid.New()
		context = context.Stringer("requestID", requestID)
	}

	// IMPORTANT: This code parallels the logic in the kernel's hid-gadget driver
	// for handling key presses and releases. It ensures that the USB gadget
	// behaves similarly to a real USB HID keyboard. This logic is paralleled
	// in the client/browser-side code in useKeyboard.ts so make sure to keep
	// them in sync.
	var state = u.GetKeysDownState()
	context = context.Object("state", state)
	logging.LogTrace(context, "got keys down state")

	modifier := state.Modifier
	keys := append([]byte(nil), state.Keys...)

	if mask, exists := KeyCodeToMaskMap[key]; exists {
		// If the key is a modifier key, we update the keyboardModifier state
		// by setting or clearing the corresponding bit in the modifier byte.
		// This allows us to track the state of dynamic modifier keys like
		// Shift, Control, Alt, and Super.
		if press {
			modifier |= mask
		} else {
			modifier &^= mask
		}
	} else {
		// handle other keys that are not modifier keys by placing or removing them
		// from the key buffer since the buffer tracks currently pressed keys
		overrun := true
		for i := range hidKeyBufferSize {
			// If we find the key in the buffer the buffer, we either remove it (if press is false)
			// or do nothing (if down is true) because the buffer tracks currently pressed keys
			// and if we find a zero byte, we can place the key there (if press is true)
			if keys[i] == key || keys[i] == 0 {
				if press {
					keys[i] = key // overwrites the zero byte or the same key if already pressed
				} else {
					// we are releasing the key, remove it from the buffer
					if keys[i] != 0 {
						copy(keys[i:], keys[i+1:])
						keys[hidKeyBufferSize-1] = 0 // Clear the last byte
					}
				}
				overrun = false // We found a slot for the key
				break
			}
		}

		// If we reach here it means we didn't find an empty slot or the key in the buffer
		if overrun {
			if press {
				logging.LogWarn(context, "keyboard buffer overflow, key not added")
				// Fill all key slots with ErrorRollOver (0x01) to indicate overflow
				for i := range keys {
					keys[i] = hidErrorRollOver
				}
				defer u.ResetRollover() // after reporting rollover, we reset the buffer
			} else {
				// If we are releasing a key, and we didn't find it in a slot, who cares?
				logging.LogTrace(context, "key not found in buffer, nothing to release")
			}
		}
	}

	err := u.keyboardWriteHidFile(modifier, keys)
	return u.UpdateKeysDown(modifier, keys), err
}

func (u *UsbGadget) KeypressReport(key byte, press bool) error {
	context := u.getHidKeyboardLoggingContext().Uint8("key", key).Bool("press", press)

	state, err := u.keypressReport(context, key, press)
	if err != nil {
		_ = logging.LogWarnE(context, err, "failed to report key")
	}
	wasRolledOver := state.Keys[0] == hidErrorRollOver

	if wasRolledOver {
		u.cancelAutoRelease(key)
	} else if press {
		u.scheduleAutoRelease(key)
	} else {
		u.cancelAutoRelease(key)
	}

	return err
}
