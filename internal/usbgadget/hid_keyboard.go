package usbgadget

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"time"
)

var keyboardConfig = gadgetConfigItem{
	order:      1000,
	device:     "hid.usb0",
	path:       []string{"functions", "hid.usb0"},
	configPath: []string{"hid.usb0"},
	attrs: gadgetAttributes{
		"protocol":      "1",
		"subclass":      "1",
		"report_length": "8",
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
	// https://www.usb.org/sites/default/files/documents/hid1_11.pdf
	// https://www.usb.org/sites/default/files/hut1_2.pdf
	KeyboardLedMaskNumLock    = 1 << 0
	KeyboardLedMaskCapsLock   = 1 << 1
	KeyboardLedMaskScrollLock = 1 << 2
	KeyboardLedMaskCompose    = 1 << 3
	KeyboardLedMaskKana       = 1 << 4
	ValidKeyboardLedMasks     = KeyboardLedMaskNumLock | KeyboardLedMaskCapsLock | KeyboardLedMaskScrollLock | KeyboardLedMaskCompose | KeyboardLedMaskKana
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
}

func getKeyboardState(b byte) KeyboardState {
	// should we check if it's the correct usage page?
	return KeyboardState{
		NumLock:    b&KeyboardLedMaskNumLock != 0,
		CapsLock:   b&KeyboardLedMaskCapsLock != 0,
		ScrollLock: b&KeyboardLedMaskScrollLock != 0,
		Compose:    b&KeyboardLedMaskCompose != 0,
		Kana:       b&KeyboardLedMaskKana != 0,
	}
}

func (u *UsbGadget) updateKeyboardState(b byte) {
	u.keyboardStateLock.Lock()
	defer u.keyboardStateLock.Unlock()

	if b&^ValidKeyboardLedMasks != 0 {
		u.log.Trace().Uint8("b", b).Msg("contains invalid bits, ignoring")
		return
	}

	newState := getKeyboardState(b)
	if reflect.DeepEqual(u.keyboardState, newState) {
		return
	}
	u.log.Info().Interface("old", u.keyboardState).Interface("new", newState).Msg("keyboardState updated")
	u.keyboardState = newState

	if u.onKeyboardStateChange != nil {
		(*u.onKeyboardStateChange)(newState)
	}
}

func (u *UsbGadget) SetOnKeyboardStateChange(f func(state KeyboardState)) {
	u.onKeyboardStateChange = &f
}

func (u *UsbGadget) GetKeyboardState() KeyboardState {
	u.keyboardStateLock.Lock()
	defer u.keyboardStateLock.Unlock()

	return u.keyboardState
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
				l.Trace().Msg("reading from keyboard")
				if u.keyboardHidFile == nil {
					u.logWithSupression("keyboardHidFileNil", 100, &l, nil, "keyboardHidFile is nil")
					// show the error every 100 times to avoid spamming the logs
					time.Sleep(time.Second)
					continue
				}
				// reset the counter
				u.resetLogSuppressionCounter("keyboardHidFileNil")

				n, err := u.keyboardHidFile.Read(buf)
				if err != nil {
					u.logWithSupression("keyboardHidFileRead", 100, &l, err, "failed to read")
					continue
				}
				u.resetLogSuppressionCounter("keyboardHidFileRead")

				l.Trace().Int("n", n).Bytes("buf", buf).Msg("got data from keyboard")
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

func (u *UsbGadget) keyboardWriteHidFile(data []byte) error {
	if err := u.openKeyboardHidFile(); err != nil {
		return err
	}

	_, err := u.keyboardHidFile.Write(data)
	if err != nil {
		u.logWithSupression("keyboardWriteHidFile", 100, u.log, err, "failed to write to hidg0")
		u.keyboardHidFile.Close()
		u.keyboardHidFile = nil
		return err
	}
	u.resetLogSuppressionCounter("keyboardWriteHidFile")
	return nil
}

func (u *UsbGadget) KeyboardReport(modifier uint8, keys []uint8) error {
	u.keyboardLock.Lock()
	defer u.keyboardLock.Unlock()

	if len(keys) > 6 {
		keys = keys[:6]
	}
	if len(keys) < 6 {
		keys = append(keys, make([]uint8, 6-len(keys))...)
	}

	err := u.keyboardWriteHidFile([]byte{modifier, 0, keys[0], keys[1], keys[2], keys[3], keys[4], keys[5]})
	if err != nil {
		return err
	}

	u.resetUserInputTime()
	return nil
}
