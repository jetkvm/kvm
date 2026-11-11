// Package keyboard translates stored keyboard macros into the wire-level HID
// macro steps consumed by the macro executor (rpcExecuteKeyboardMacro). The key
// and modifier vocabulary is the same one the web UI stores into keyboard
// macros (ui/src/keyboardMappings.ts).
package keyboard

import (
	"fmt"

	"github.com/jetkvm/kvm/internal/hidrpc"
)

// MacroStep is one authored macro step: named keys pressed together with the
// named modifiers held, then released. It mirrors the config-level
// KeyboardMacroStep.
type MacroStep struct {
	Keys      []string
	Modifiers []string
	Delay     int
}

// keyCodes maps a key name to its USB HID usage ID. Ported from
// ui/src/keyboardMappings.ts `export const keys` so the device agrees with the
// names the web UI stores into keyboard macros.
var keyCodes = map[string]byte{
	"Again":                0x79,
	"AlternateErase":       0x9d,
	"AltGr":                0xe6, // aka AltRight
	"AltLeft":              0xe2,
	"AltRight":             0xe6,
	"Application":          0x65,
	"ArrowDown":            0x51,
	"ArrowLeft":            0x50,
	"ArrowRight":           0x4f,
	"ArrowUp":              0x52,
	"Attention":            0x9a,
	"Backquote":            0x35, // aka Grave
	"Backslash":            0x31,
	"Backspace":            0x2a,
	"BracketLeft":          0x2f, // aka LeftBrace
	"BracketRight":         0x30, // aka RightBrace
	"Cancel":               0x9b,
	"CapsLock":             0x39,
	"Clear":                0x9c,
	"ClearAgain":           0xa2,
	"Comma":                0x36,
	"Compose":              0xe3,
	"ContextMenu":          0x65,
	"ControlLeft":          0xe0,
	"ControlRight":         0xe4,
	"Copy":                 0x7c,
	"CrSel":                0xa3,
	"CurrencySubunit":      0xb5,
	"CurrencyUnit":         0xb4,
	"Cut":                  0x7b,
	"DecimalSeparator":     0xb3,
	"Delete":               0x4c,
	"Digit0":               0x27,
	"Digit1":               0x1e,
	"Digit2":               0x1f,
	"Digit3":               0x20,
	"Digit4":               0x21,
	"Digit5":               0x22,
	"Digit6":               0x23,
	"Digit7":               0x24,
	"Digit8":               0x25,
	"Digit9":               0x26,
	"End":                  0x4d,
	"Enter":                0x28,
	"Equal":                0x2e,
	"Escape":               0x29,
	"Execute":              0x74,
	"ExSel":                0xa4,
	"F1":                   0x3a,
	"F2":                   0x3b,
	"F3":                   0x3c,
	"F4":                   0x3d,
	"F5":                   0x3e,
	"F6":                   0x3f,
	"F7":                   0x40,
	"F8":                   0x41,
	"F9":                   0x42,
	"F10":                  0x43,
	"F11":                  0x44,
	"F12":                  0x45,
	"F13":                  0x68,
	"F14":                  0x69,
	"F15":                  0x6a,
	"F16":                  0x6b,
	"F17":                  0x6c,
	"F18":                  0x6d,
	"F19":                  0x6e,
	"F20":                  0x6f,
	"F21":                  0x70,
	"F22":                  0x71,
	"F23":                  0x72,
	"F24":                  0x73,
	"Find":                 0x7e,
	"Grave":                0x35,
	"HashTilde":            0x32, // non-US # and ~
	"Help":                 0x75,
	"Home":                 0x4a,
	"Insert":               0x49,
	"International7":       0x8d,
	"International8":       0x8e,
	"International9":       0x8f,
	"IntlBackslash":        0x64, // non-US \ and |
	"KeyA":                 0x04,
	"KeyB":                 0x05,
	"KeyC":                 0x06,
	"KeyD":                 0x07,
	"KeyE":                 0x08,
	"KeyF":                 0x09,
	"KeyG":                 0x0a,
	"KeyH":                 0x0b,
	"KeyI":                 0x0c,
	"KeyJ":                 0x0d,
	"KeyK":                 0x0e,
	"KeyL":                 0x0f,
	"KeyM":                 0x10,
	"KeyN":                 0x11,
	"KeyO":                 0x12,
	"KeyP":                 0x13,
	"KeyQ":                 0x14,
	"KeyR":                 0x15,
	"KeyS":                 0x16,
	"KeyT":                 0x17,
	"KeyU":                 0x18,
	"KeyV":                 0x19,
	"KeyW":                 0x1a,
	"KeyX":                 0x1b,
	"KeyY":                 0x1c,
	"KeyZ":                 0x1d,
	"KeyRO":                0x87,
	"KatakanaHiragana":     0x88,
	"Yen":                  0x89,
	"Henkan":               0x8a,
	"Muhenkan":             0x8b,
	"KPJPComma":            0x8c,
	"Hangeul":              0x90,
	"Hanja":                0x91,
	"Katakana":             0x92,
	"Hiragana":             0x93,
	"ZenkakuHankaku":       0x94,
	"LockingCapsLock":      0x82,
	"LockingNumLock":       0x83,
	"LockingScrollLock":    0x84,
	"Lang6":                0x95,
	"Lang7":                0x96,
	"Lang8":                0x97,
	"Lang9":                0x98,
	"Menu":                 0x76,
	"MetaLeft":             0xe3,
	"MetaRight":            0xe7,
	"Minus":                0x2d,
	"Mute":                 0x7f,
	"NumLock":              0x53, // and Clear
	"Numpad0":              0x62, // and Insert
	"Numpad00":             0xb0,
	"Numpad000":            0xb1,
	"Numpad1":              0x59, // and End
	"Numpad2":              0x5a, // and Down Arrow
	"Numpad3":              0x5b, // and Page Down
	"Numpad4":              0x5c, // and Left Arrow
	"Numpad5":              0x5d,
	"Numpad6":              0x5e, // and Right Arrow
	"Numpad7":              0x5f, // and Home
	"Numpad8":              0x60, // and Up Arrow
	"Numpad9":              0x61, // and Page Up
	"NumpadAdd":            0x57,
	"NumpadAnd":            0xc7,
	"NumpadAt":             0xce,
	"NumpadBackspace":      0xbb,
	"NumpadBinary":         0xda,
	"NumpadCircumflex":     0xc3,
	"NumpadClear":          0xd8,
	"NumpadClearEntry":     0xd9,
	"NumpadColon":          0xcb,
	"NumpadComma":          0x85,
	"NumpadDecimal":        0x63, // and Delete
	"NumpadDecimalBase":    0xdc,
	"NumpadDelete":         0x63,
	"NumpadDivide":         0x54,
	"NumpadDownArrow":      0x5a,
	"NumpadEnd":            0x59,
	"NumpadEnter":          0x58,
	"NumpadEqual":          0x67,
	"NumpadExclamation":    0xcf,
	"NumpadGreaterThan":    0xc6,
	"NumpadHexadecimal":    0xdd,
	"NumpadHome":           0x5f,
	"NumpadKeyA":           0xbc,
	"NumpadKeyB":           0xbd,
	"NumpadKeyC":           0xbe,
	"NumpadKeyD":           0xbf,
	"NumpadKeyE":           0xc0,
	"NumpadKeyF":           0xc1,
	"NumpadLeftArrow":      0x5c,
	"NumpadLeftBrace":      0xb8,
	"NumpadLeftParen":      0xb6,
	"NumpadLessThan":       0xc5,
	"NumpadLogicalAnd":     0xc8,
	"NumpadLogicalOr":      0xca,
	"NumpadMemoryAdd":      0xd3,
	"NumpadMemoryClear":    0xd2,
	"NumpadMemoryDivide":   0xd6,
	"NumpadMemoryMultiply": 0xd5,
	"NumpadMemoryRecall":   0xd1,
	"NumpadMemoryStore":    0xd0,
	"NumpadMemorySubtract": 0xd4,
	"NumpadMultiply":       0x55,
	"NumpadOctal":          0xdb,
	"NumpadOctathorpe":     0xcc,
	"NumpadOr":             0xc9,
	"NumpadPageDown":       0x5b,
	"NumpadPageUp":         0x61,
	"NumpadPercent":        0xc4,
	"NumpadPlusMinus":      0xd7,
	"NumpadRightArrow":     0x5e,
	"NumpadRightBrace":     0xb9,
	"NumpadRightParen":     0xb7,
	"NumpadSpace":          0xcd,
	"NumpadSubtract":       0x56,
	"NumpadTab":            0xba,
	"NumpadUpArrow":        0x60,
	"NumpadXOR":            0xc2,
	"Octothorpe":           0x32, // non-US # and ~
	"Operation":            0xa1,
	"Out":                  0xa0,
	"PageDown":             0x4e,
	"PageUp":               0x4b,
	"Paste":                0x7d,
	"Pause":                0x48,
	"Period":               0x37, // aka Dot
	"Power":                0x66,
	"PrintScreen":          0x46,
	"Prior":                0x9d,
	"Quote":                0x34, // aka Single Quote or Apostrophe
	"Return":               0x9e,
	"ScrollLock":           0x47,
	"Select":               0x77,
	"Semicolon":            0x33,
	"Separator":            0x9f,
	"ShiftLeft":            0xe1,
	"ShiftRight":           0xe5,
	"Slash":                0x38,
	"Space":                0x2c,
	"Stop":                 0x78,
	"SystemRequest":        0x9a, // aka Attention
	"Tab":                  0x2b,
	"ThousandsSeparator":   0xb2,
	"Tilde":                0x35,
	"Undo":                 0x7a,
	"VolumeDown":           0x81,
	"VolumeUp":             0x80,
}

// modifierMasks maps a modifier name to its bit in the HID report modifier
// byte. Ported from ui/src/keyboardMappings.ts `export const modifiers`.
var modifierMasks = map[string]byte{
	"ControlLeft":  0x01,
	"ControlRight": 0x10,
	"ShiftLeft":    0x02,
	"ShiftRight":   0x20,
	"AltLeft":      0x04,
	"AltRight":     0x40,
	"MetaLeft":     0x08,
	"MetaRight":    0x80,
	"AltGr":        0x40,
}

// Timing for converted macro steps, mirroring the web UI's macro execution
// (executeMacroRemote in ui/src/hooks/useKeyboard.ts): each press is held for
// 20ms, and a release step with no delay of its own falls back to 100ms.
const (
	pressDelayMs   = 20
	defaultDelayMs = 100
)

// HIDSteps converts authored macro steps into the wire-level steps consumed
// by rpcExecuteKeyboardMacro, following the web UI's macro execution
// (executeMacroRemote in ui/src/hooks/useKeyboard.ts): every step becomes a
// press report held for pressDelayMs followed by an all-zero release report
// carrying the step's delay. The explicit release matters —
// rpcDoExecuteKeyboardMacro plays reports as-is, so without it keys and
// modifiers would stay held on the host and bleed into the next step. Steps
// with nothing to press are skipped, as in the web UI.
//
// Two intentional divergences from the web UI, both toward failing closed for
// programmatic input: an unknown key or modifier name aborts the whole macro
// with an error (the web UI silently drops unknown keys), and keys beyond the
// 6-slot HID report are dropped to match the usbgadget's own KeyboardReport
// truncation (internal/usbgadget/hid_keyboard.go) rather than erroring as the
// web UI's remote path does.
func HIDSteps(steps []MacroStep) ([]hidrpc.KeyboardMacroStep, error) {
	hidSteps := make([]hidrpc.KeyboardMacroStep, 0, len(steps)*2)

	for i, step := range steps {
		if len(step.Keys) == 0 && len(step.Modifiers) == 0 {
			continue
		}

		var modifier byte
		for _, name := range step.Modifiers {
			mask, ok := modifierMasks[name]
			if !ok {
				return nil, fmt.Errorf("unknown modifier %q in step %d", name, i+1)
			}
			modifier |= mask
		}

		keys := make([]byte, hidrpc.HidKeyBufferSize)
		for j, name := range step.Keys {
			code, ok := keyCodes[name]
			if !ok {
				return nil, fmt.Errorf("unknown key %q in step %d", name, i+1)
			}
			if j < hidrpc.HidKeyBufferSize {
				keys[j] = code
			}
		}

		delay := step.Delay
		if delay <= 0 {
			delay = defaultDelayMs
		} else if delay > 65535 {
			delay = 65535
		}

		hidSteps = append(hidSteps,
			hidrpc.KeyboardMacroStep{Modifier: modifier, Keys: keys, Delay: pressDelayMs},
			hidrpc.KeyboardMacroStep{Keys: make([]byte, hidrpc.HidKeyBufferSize), Delay: uint16(delay)},
		)
	}

	return hidSteps, nil
}
