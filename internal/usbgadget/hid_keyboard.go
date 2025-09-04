package usbgadget

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"time"
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

	if state&^ValidKeyboardLedMasks != 0 {
		u.log.Warn().Uint8("state", state).Msg("ignoring invalid bits")
		return
	}

	if u.keyboardState == state {
		return
	}
	u.log.Trace().Uint8("old", u.keyboardState).Uint8("new", state).Msg("keyboardState updated")
	u.keyboardState = state

	if u.onKeyboardStateChange != nil {
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

func (u *UsbGadget) updateKeyDownState(state KeysDownState) {
	u.log.Trace().Interface("old", u.keysDownState).Interface("new", state).Msg("acquiring keyboardStateLock for updateKeyDownState")

	// this is intentional to unlock keyboard state lock before onKeysDownChange callback
	{
		u.keyboardStateLock.Lock()
		defer u.keyboardStateLock.Unlock()

		if u.keysDownState.Modifier == state.Modifier &&
			bytes.Equal(u.keysDownState.Keys, state.Keys) {
			return // No change in key down state
		}

		u.log.Trace().Interface("old", u.keysDownState).Interface("new", state).Msg("keysDownState updated")
		u.keysDownState = state
	}

	if u.onKeysDownChange != nil {
		u.log.Trace().Interface("state", state).Msg("calling onKeysDownChange")
		(*u.onKeysDownChange)(state)
		u.log.Trace().Interface("state", state).Msg("onKeysDownChange called")
	}
}

func (u *UsbGadget) SetOnKeysDownChange(f func(state KeysDownState)) {
	u.onKeysDownChange = &f
}

func (u *UsbGadget) listenKeyboardEvents() {
	var path string
	if u.keyboardHidFile != nil {
		path = u.keyboardHidFile.Name()
	}
	l := u.log.With().Str("listener", "keyboardEvents").Str("path", path).Logger()
	l.Trace().Msg("starting")

	go func() {
		buf := make([]byte, hidReadBufferSize)
		for {
			select {
			case <-u.keyboardStateCtx.Done():
				l.Info().Msg("context done")
				return
			default:
				l.Trace().Msg("reading from keyboard for LED state changes")
				if u.keyboardHidFile == nil {
					u.logWithSuppression("keyboardHidFileNil", 100, &l, nil, "keyboardHidFile is nil")
					// show the error every 100 times to avoid spamming the logs
					time.Sleep(time.Second)
					continue
				}
				// reset the counter
				u.resetLogSuppressionCounter("keyboardHidFileNil")

				n, err := u.keyboardHidFile.Read(buf)
				if err != nil {
					u.logWithSuppression("keyboardHidFileRead", 100, &l, err, "failed to read")
					continue
				}
				u.resetLogSuppressionCounter("keyboardHidFileRead")

				l.Trace().Int("n", n).Uints8("buf", buf).Msg("got data from keyboard")
				if n != 1 {
					l.Trace().Int("n", n).Msg("expected 1 byte, got")
					continue
				}
				u.updateKeyboardState(buf[0])
			}
		}
	}()
}

func (u *UsbGadget) openKeyboardHidFile() error {
	if u.keyboardHidFile != nil {
		return nil
	}

	var err error
	u.keyboardHidFile, err = os.OpenFile("/dev/hidg0", os.O_RDWR, 0666)
	if err != nil {
		return fmt.Errorf("failed to open hidg0: %w", err)
	}

	if u.keyboardStateCancel != nil {
		u.keyboardStateCancel()
	}

	u.keyboardStateCtx, u.keyboardStateCancel = context.WithCancel(context.Background())
	u.listenKeyboardEvents()

	return nil
}

func (u *UsbGadget) OpenKeyboardHidFile() error {
	return u.openKeyboardHidFile()
}

func (u *UsbGadget) keyboardWriteHidFile(modifier byte, keys []byte) error {
	if err := u.openKeyboardHidFile(); err != nil {
		return err
	}

	_, err := u.writeWithTimeout(u.keyboardHidFile, append([]byte{modifier, 0x00}, keys[:hidKeyBufferSize]...))
	if err != nil {
		u.logWithSuppression("keyboardWriteHidFile", 100, u.log, err, "failed to write to hidg0")
		u.keyboardHidFile.Close()
		u.keyboardHidFile = nil
		return err
	}
	u.resetLogSuppressionCounter("keyboardWriteHidFile")
	return nil
}

func (u *UsbGadget) UpdateKeysDown(modifier byte, keys []byte) KeysDownState {
	// if we just reported an error roll over, we should clear the keys
	if keys[0] == hidErrorRollOver {
		for i := range keys {
			keys[i] = 0
		}
	}

	downState := KeysDownState{
		Modifier: modifier,
		Keys:     []byte(keys[:]),
	}
	u.updateKeyDownState(downState)
	return downState
}

func (u *UsbGadget) KeyboardReport(modifier byte, keys []byte) (KeysDownState, error) {
	u.keyboardLock.Lock()
	defer u.keyboardLock.Unlock()
	defer u.resetUserInputTime()

	if len(keys) > hidKeyBufferSize {
		keys = keys[:hidKeyBufferSize]
	}
	if len(keys) < hidKeyBufferSize {
		keys = append(keys, make([]byte, hidKeyBufferSize-len(keys))...)
	}

	err := u.keyboardWriteHidFile(modifier, keys)
	if err != nil {
		u.log.Warn().Uint8("modifier", modifier).Uints8("keys", keys).Msg("Could not write keyboard report to hidg0")
	}

	return u.UpdateKeysDown(modifier, keys), err
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

func (u *UsbGadget) KeypressReport(key byte, press bool) (KeysDownState, error) {
	u.keyboardLock.Lock()
	defer u.keyboardLock.Unlock()
	defer u.resetUserInputTime()

	// IMPORTANT: This code parallels the logic in the kernel's hid-gadget driver
	// for handling key presses and releases. It ensures that the USB gadget
	// behaves similarly to a real USB HID keyboard. This logic is paralleled
	// in the client/browser-side code in useKeyboard.ts so make sure to keep
	// them in sync.
	var state = u.keysDownState
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
				u.log.Error().Uint8("key", key).Msg("keyboard buffer overflow, key not added")
				// Fill all key slots with ErrorRollOver (0x01) to indicate overflow
				for i := range keys {
					keys[i] = hidErrorRollOver
				}
			} else {
				// If we are releasing a key, and we didn't find it in a slot, who cares?
				u.log.Warn().Uint8("key", key).Msg("key not found in buffer, nothing to release")
			}
		}
	}

	err := u.keyboardWriteHidFile(modifier, keys)
	if err != nil {
		u.log.Warn().Uint8("modifier", modifier).Uints8("keys", keys).Msg("Could not write keypress report to hidg0")
	}

	return u.UpdateKeysDown(modifier, keys), err
}
