// Package keyboard provides shared keyboard text-to-macro conversion for VNC and RDP.
package keyboard

import (
	"encoding/base64"
	"fmt"
	"unicode/utf8"

	"github.com/jetkvm/kvm/internal/hidrpc"
)

// HID modifier constants (matches USB HID spec).
const (
	ModShiftLeft = 0x02
	ModAltRight  = 0x40 // AltGr on international keyboards
)

// MaxClipboardSize is the maximum clipboard text size to type (10KB).
const MaxClipboardSize = 10 * 1024

// domKeyToHID maps DOM key codes to USB HID key codes.
// This mapping is layout-independent - it represents the physical key positions.
var domKeyToHID = map[string]uint8{
	// Letter keys
	"KeyA": 0x04, "KeyB": 0x05, "KeyC": 0x06, "KeyD": 0x07,
	"KeyE": 0x08, "KeyF": 0x09, "KeyG": 0x0A, "KeyH": 0x0B,
	"KeyI": 0x0C, "KeyJ": 0x0D, "KeyK": 0x0E, "KeyL": 0x0F,
	"KeyM": 0x10, "KeyN": 0x11, "KeyO": 0x12, "KeyP": 0x13,
	"KeyQ": 0x14, "KeyR": 0x15, "KeyS": 0x16, "KeyT": 0x17,
	"KeyU": 0x18, "KeyV": 0x19, "KeyW": 0x1A, "KeyX": 0x1B,
	"KeyY": 0x1C, "KeyZ": 0x1D,
	// Digit keys
	"Digit1": 0x1E, "Digit2": 0x1F, "Digit3": 0x20, "Digit4": 0x21,
	"Digit5": 0x22, "Digit6": 0x23, "Digit7": 0x24, "Digit8": 0x25,
	"Digit9": 0x26, "Digit0": 0x27,
	// Punctuation and special keys
	"Minus":         0x2D, // - _
	"Equal":         0x2E, // = +
	"BracketLeft":   0x2F, // [ {
	"BracketRight":  0x30, // ] }
	"Backslash":     0x31, // \ |
	"Semicolon":     0x33, // ; :
	"Quote":         0x34, // ' "
	"Backquote":     0x35, // ` ~
	"Comma":         0x36, // , <
	"Period":        0x37, // . >
	"Slash":         0x38, // / ?
	"IntlBackslash": 0x64, // Non-US \ | (ISO keyboards)
	// Whitespace and control
	"Space": 0x2C,
	"Tab":   0x2B,
	"Enter": 0x28,
}

// keyCombo represents a character's key combination.
type keyCombo struct {
	key      string // DOM key code (e.g., "KeyA", "Digit1")
	shift    bool   // Requires Shift modifier
	altRight bool   // Requires AltGr (Right Alt) modifier
}

// keyboardLayout maps characters to their key combinations for a specific layout.
type keyboardLayout map[rune]keyCombo

// Keyboard layout definitions.
var layoutEnUS = keyboardLayout{
	// Letters (same physical keys, shift for uppercase)
	'a': {"KeyA", false, false}, 'b': {"KeyB", false, false}, 'c': {"KeyC", false, false}, 'd': {"KeyD", false, false},
	'e': {"KeyE", false, false}, 'f': {"KeyF", false, false}, 'g': {"KeyG", false, false}, 'h': {"KeyH", false, false},
	'i': {"KeyI", false, false}, 'j': {"KeyJ", false, false}, 'k': {"KeyK", false, false}, 'l': {"KeyL", false, false},
	'm': {"KeyM", false, false}, 'n': {"KeyN", false, false}, 'o': {"KeyO", false, false}, 'p': {"KeyP", false, false},
	'q': {"KeyQ", false, false}, 'r': {"KeyR", false, false}, 's': {"KeyS", false, false}, 't': {"KeyT", false, false},
	'u': {"KeyU", false, false}, 'v': {"KeyV", false, false}, 'w': {"KeyW", false, false}, 'x': {"KeyX", false, false},
	'y': {"KeyY", false, false}, 'z': {"KeyZ", false, false},
	'A': {"KeyA", true, false}, 'B': {"KeyB", true, false}, 'C': {"KeyC", true, false}, 'D': {"KeyD", true, false},
	'E': {"KeyE", true, false}, 'F': {"KeyF", true, false}, 'G': {"KeyG", true, false}, 'H': {"KeyH", true, false},
	'I': {"KeyI", true, false}, 'J': {"KeyJ", true, false}, 'K': {"KeyK", true, false}, 'L': {"KeyL", true, false},
	'M': {"KeyM", true, false}, 'N': {"KeyN", true, false}, 'O': {"KeyO", true, false}, 'P': {"KeyP", true, false},
	'Q': {"KeyQ", true, false}, 'R': {"KeyR", true, false}, 'S': {"KeyS", true, false}, 'T': {"KeyT", true, false},
	'U': {"KeyU", true, false}, 'V': {"KeyV", true, false}, 'W': {"KeyW", true, false}, 'X': {"KeyX", true, false},
	'Y': {"KeyY", true, false}, 'Z': {"KeyZ", true, false},
	// Digits
	'1': {"Digit1", false, false}, '2': {"Digit2", false, false}, '3': {"Digit3", false, false}, '4': {"Digit4", false, false},
	'5': {"Digit5", false, false}, '6': {"Digit6", false, false}, '7': {"Digit7", false, false}, '8': {"Digit8", false, false},
	'9': {"Digit9", false, false}, '0': {"Digit0", false, false},
	// Shifted digits (symbols)
	'!': {"Digit1", true, false}, '@': {"Digit2", true, false}, '#': {"Digit3", true, false}, '$': {"Digit4", true, false},
	'%': {"Digit5", true, false}, '^': {"Digit6", true, false}, '&': {"Digit7", true, false}, '*': {"Digit8", true, false},
	'(': {"Digit9", true, false}, ')': {"Digit0", true, false},
	// Punctuation
	'-': {"Minus", false, false}, '_': {"Minus", true, false},
	'=': {"Equal", false, false}, '+': {"Equal", true, false},
	'[': {"BracketLeft", false, false}, '{': {"BracketLeft", true, false},
	']': {"BracketRight", false, false}, '}': {"BracketRight", true, false},
	'\\': {"Backslash", false, false}, '|': {"Backslash", true, false},
	';': {"Semicolon", false, false}, ':': {"Semicolon", true, false},
	'\'': {"Quote", false, false}, '"': {"Quote", true, false},
	'`': {"Backquote", false, false}, '~': {"Backquote", true, false},
	',': {"Comma", false, false}, '<': {"Comma", true, false},
	'.': {"Period", false, false}, '>': {"Period", true, false},
	'/': {"Slash", false, false}, '?': {"Slash", true, false},
	// Whitespace
	' ': {"Space", false, false}, '\t': {"Tab", false, false}, '\n': {"Enter", false, false}, '\r': {"Enter", false, false},
}

// GetLayout returns the keyboard layout for the given code, falling back to en-US.
func GetLayout(layoutCode string) keyboardLayout {
	switch layoutCode {
	case "en-US", "":
		return layoutEnUS
	default:
		return layoutEnUS
	}
}

// TextToMacroSteps converts text to HID keyboard macro steps.
// Uses the specified keyboard layout. Characters not in the layout are skipped.
// pressDelayMs and releaseDelayMs control typing speed.
// Returns the macro steps and count of skipped characters.
func TextToMacroSteps(text string, layoutCode string, pressDelayMs, releaseDelayMs int) ([]hidrpc.KeyboardMacroStep, int) {
	layout := GetLayout(layoutCode)
	steps := make([]hidrpc.KeyboardMacroStep, 0, len(text)*2)
	skipped := 0

	for _, char := range text {
		combo, ok := layout[char]
		if !ok {
			skipped++
			continue
		}

		hidKey, ok := domKeyToHID[combo.key]
		if !ok {
			skipped++
			continue
		}

		// Build modifier byte
		var modifier uint8
		if combo.shift {
			modifier |= ModShiftLeft
		}
		if combo.altRight {
			modifier |= ModAltRight
		}

		// Key press step
		keys := make([]uint8, hidrpc.HidKeyBufferSize)
		keys[0] = hidKey
		steps = append(steps, hidrpc.KeyboardMacroStep{
			Modifier: modifier,
			Keys:     keys,
			Delay:    uint16(pressDelayMs),
		})

		// Key release step (all zeros)
		releaseKeys := make([]uint8, hidrpc.HidKeyBufferSize)
		steps = append(steps, hidrpc.KeyboardMacroStep{
			Modifier: 0,
			Keys:     releaseKeys,
			Delay:    uint16(releaseDelayMs),
		})
	}

	return steps, skipped
}

// ComputeDelays returns press and release delays based on a total delay.
// Returns (pressDelayMs, releaseDelayMs) - each is half the configured delay.
// For odd values, gives extra ms to release for better compatibility.
func ComputeDelays(totalDelayMs int) (int, int) {
	if totalDelayMs < 0 {
		totalDelayMs = 0
	}
	pressDelay := totalDelayMs / 2
	releaseDelay := totalDelayMs - pressDelay
	return pressDelay, releaseDelay
}

// DefaultDelayMs is the default total delay between keystrokes (16ms = ~60 chars/sec).
const DefaultDelayMs = 16

// ClipboardMode defines how clipboard content is processed for typing.
type ClipboardMode string

const (
	// ClipboardModeText types only ASCII characters, skipping non-typeable ones.
	ClipboardModeText ClipboardMode = "text"
	// ClipboardModeBase64Markers encodes content as base64 wrapped in markers.
	ClipboardModeBase64Markers ClipboardMode = "base64-markers"
	// ClipboardModeBase64Script types an OS-specific script to decode base64.
	ClipboardModeBase64Script ClipboardMode = "base64-script"
)

// TargetOS defines the target operating system for script generation.
type TargetOS string

const (
	TargetOSWindows TargetOS = "windows"
	TargetOSMacOS   TargetOS = "macos"
	TargetOSLinux   TargetOS = "linux"
)

// HasNonTypeableChars checks if text contains characters that cannot be typed
// on a standard US keyboard layout.
func HasNonTypeableChars(text string) bool {
	layout := GetLayout("en-US")
	for _, char := range text {
		if _, ok := layout[char]; !ok {
			return true
		}
	}
	return false
}

// IsValidUTF8Text checks if the data is valid UTF-8 text (not binary).
func IsValidUTF8Text(data []byte) bool {
	return utf8.Valid(data)
}

// EncodeBase64WithMarkers encodes data as base64 wrapped in markers.
func EncodeBase64WithMarkers(data []byte) string {
	encoded := base64.StdEncoding.EncodeToString(data)
	return fmt.Sprintf("---BEGIN CLIPBOARD (base64)---\n%s\n---END CLIPBOARD---", encoded)
}

// GenerateDecodeScript generates an OS-specific script to decode base64 content.
// The script writes decoded content to clipboard or a file.
func GenerateDecodeScript(data []byte, targetOS TargetOS, filename string) string {
	encoded := base64.StdEncoding.EncodeToString(data)

	if filename == "" {
		filename = "clipboard_content.txt"
	}

	switch targetOS {
	case TargetOSWindows:
		// PowerShell command to decode base64 and save to file
		return fmt.Sprintf(
			`powershell -Command "[System.Text.Encoding]::UTF8.GetString([System.Convert]::FromBase64String('%s')) | Set-Content -Path '%s' -NoNewline"`,
			encoded, filename)

	case TargetOSMacOS:
		// macOS bash command to decode base64 and save to file
		return fmt.Sprintf(
			`echo '%s' | base64 -D > '%s'`,
			encoded, filename)

	case TargetOSLinux:
		// Linux bash command to decode base64 and save to file
		return fmt.Sprintf(
			`echo '%s' | base64 -d > '%s'`,
			encoded, filename)

	default:
		// Fallback to Linux syntax
		return fmt.Sprintf(
			`echo '%s' | base64 -d > '%s'`,
			encoded, filename)
	}
}

// PrepareClipboardText prepares clipboard content for typing based on the mode.
// Returns the text to type and whether encoding was applied.
func PrepareClipboardText(data []byte, mode ClipboardMode, targetOS TargetOS) (string, bool) {
	// Check if it's valid UTF-8
	if !IsValidUTF8Text(data) {
		// Binary content - must encode
		switch mode {
		case ClipboardModeBase64Markers:
			return EncodeBase64WithMarkers(data), true
		case ClipboardModeBase64Script:
			return GenerateDecodeScript(data, targetOS, "clipboard_binary.bin"), true
		default:
			// Text mode can't handle binary - return empty
			return "", false
		}
	}

	text := string(data)

	// Check if text has non-typeable characters
	if !HasNonTypeableChars(text) {
		// Plain ASCII text - type directly regardless of mode
		return text, false
	}

	// Has non-typeable characters - handle based on mode
	switch mode {
	case ClipboardModeBase64Markers:
		return EncodeBase64WithMarkers(data), true
	case ClipboardModeBase64Script:
		return GenerateDecodeScript(data, targetOS, "clipboard_content.txt"), true
	default:
		// Text mode - return as-is, non-typeable chars will be skipped
		return text, false
	}
}
