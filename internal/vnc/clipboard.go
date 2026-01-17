package vnc

// HID modifier constants (matches USB HID spec).
const (
	hidModShiftLeft = 0x02
	hidModAltRight  = 0x40 // AltGr on international keyboards
)

// HidKeyBufferSize is the size of the HID key buffer.
const HidKeyBufferSize = 6

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
// Each layout maps Unicode characters to their key combinations.

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

var layoutEnUK = keyboardLayout{
	// Letters (same as US)
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
	// UK-specific shifted digits
	'!': {"Digit1", true, false}, '"': {"Digit2", true, false}, '£': {"Digit3", true, false}, '$': {"Digit4", true, false},
	'%': {"Digit5", true, false}, '^': {"Digit6", true, false}, '&': {"Digit7", true, false}, '*': {"Digit8", true, false},
	'(': {"Digit9", true, false}, ')': {"Digit0", true, false},
	// UK-specific symbols
	'€':  {"Digit4", false, true}, // AltGr+4
	'@':  {"Quote", true, false},  // Shift+' (different from US!)
	'\'': {"Quote", false, false},
	'#':  {"Backslash", false, false}, '~': {"Backslash", true, false},
	'\\': {"IntlBackslash", false, false}, '|': {"IntlBackslash", true, false},
	// Common punctuation (same as US)
	'-': {"Minus", false, false}, '_': {"Minus", true, false},
	'=': {"Equal", false, false}, '+': {"Equal", true, false},
	'[': {"BracketLeft", false, false}, '{': {"BracketLeft", true, false},
	']': {"BracketRight", false, false}, '}': {"BracketRight", true, false},
	';': {"Semicolon", false, false}, ':': {"Semicolon", true, false},
	'`': {"Backquote", false, false},
	',': {"Comma", false, false}, '<': {"Comma", true, false},
	'.': {"Period", false, false}, '>': {"Period", true, false},
	'/': {"Slash", false, false}, '?': {"Slash", true, false},
	// Whitespace
	' ': {"Space", false, false}, '\t': {"Tab", false, false}, '\n': {"Enter", false, false}, '\r': {"Enter", false, false},
}

var layoutDeDE = keyboardLayout{
	// QWERTZ layout - Y and Z are swapped
	'a': {"KeyA", false, false}, 'b': {"KeyB", false, false}, 'c': {"KeyC", false, false}, 'd': {"KeyD", false, false},
	'e': {"KeyE", false, false}, 'f': {"KeyF", false, false}, 'g': {"KeyG", false, false}, 'h': {"KeyH", false, false},
	'i': {"KeyI", false, false}, 'j': {"KeyJ", false, false}, 'k': {"KeyK", false, false}, 'l': {"KeyL", false, false},
	'm': {"KeyM", false, false}, 'n': {"KeyN", false, false}, 'o': {"KeyO", false, false}, 'p': {"KeyP", false, false},
	'q': {"KeyQ", false, false}, 'r': {"KeyR", false, false}, 's': {"KeyS", false, false}, 't': {"KeyT", false, false},
	'u': {"KeyU", false, false}, 'v': {"KeyV", false, false}, 'w': {"KeyW", false, false}, 'x': {"KeyX", false, false},
	'y': {"KeyZ", false, false}, 'z': {"KeyY", false, false}, // SWAPPED!
	'A': {"KeyA", true, false}, 'B': {"KeyB", true, false}, 'C': {"KeyC", true, false}, 'D': {"KeyD", true, false},
	'E': {"KeyE", true, false}, 'F': {"KeyF", true, false}, 'G': {"KeyG", true, false}, 'H': {"KeyH", true, false},
	'I': {"KeyI", true, false}, 'J': {"KeyJ", true, false}, 'K': {"KeyK", true, false}, 'L': {"KeyL", true, false},
	'M': {"KeyM", true, false}, 'N': {"KeyN", true, false}, 'O': {"KeyO", true, false}, 'P': {"KeyP", true, false},
	'Q': {"KeyQ", true, false}, 'R': {"KeyR", true, false}, 'S': {"KeyS", true, false}, 'T': {"KeyT", true, false},
	'U': {"KeyU", true, false}, 'V': {"KeyV", true, false}, 'W': {"KeyW", true, false}, 'X': {"KeyX", true, false},
	'Y': {"KeyZ", true, false}, 'Z': {"KeyY", true, false}, // SWAPPED!
	// Digits
	'1': {"Digit1", false, false}, '2': {"Digit2", false, false}, '3': {"Digit3", false, false}, '4': {"Digit4", false, false},
	'5': {"Digit5", false, false}, '6': {"Digit6", false, false}, '7': {"Digit7", false, false}, '8': {"Digit8", false, false},
	'9': {"Digit9", false, false}, '0': {"Digit0", false, false},
	// German-specific shifted digits
	'!': {"Digit1", true, false}, '"': {"Digit2", true, false}, '§': {"Digit3", true, false}, '$': {"Digit4", true, false},
	'%': {"Digit5", true, false}, '&': {"Digit6", true, false}, '/': {"Digit7", true, false}, '(': {"Digit8", true, false},
	')': {"Digit9", true, false}, '=': {"Digit0", true, false},
	// German-specific AltGr symbols
	'@': {"KeyQ", false, true}, '€': {"KeyE", false, true}, 'µ': {"KeyM", false, true},
	'²': {"Digit2", false, true}, '³': {"Digit3", false, true}, '{': {"Digit7", false, true},
	'[': {"Digit8", false, true}, ']': {"Digit9", false, true}, '}': {"Digit0", false, true},
	'\\': {"Minus", false, true}, '~': {"BracketRight", false, true}, '|': {"IntlBackslash", false, true},
	// German punctuation
	'ß': {"Minus", false, false}, '?': {"Minus", true, false},
	'+': {"BracketRight", false, false}, '*': {"BracketRight", true, false},
	'#': {"Backslash", false, false}, '\'': {"Backslash", true, false},
	'-': {"Slash", false, false}, '_': {"Slash", true, false},
	'.': {"Period", false, false}, ':': {"Period", true, false},
	',': {"Comma", false, false}, ';': {"Comma", true, false},
	'<': {"IntlBackslash", false, false}, '>': {"IntlBackslash", true, false},
	// German umlauts (on dedicated keys)
	'ü': {"BracketLeft", false, false}, 'Ü': {"BracketLeft", true, false},
	'ö': {"Semicolon", false, false}, 'Ö': {"Semicolon", true, false},
	'ä': {"Quote", false, false}, 'Ä': {"Quote", true, false},
	// Whitespace
	' ': {"Space", false, false}, '\t': {"Tab", false, false}, '\n': {"Enter", false, false}, '\r': {"Enter", false, false},
}

var layoutFrFR = keyboardLayout{
	// AZERTY layout - completely different arrangement
	'a': {"KeyQ", false, false}, 'b': {"KeyB", false, false}, 'c': {"KeyC", false, false}, 'd': {"KeyD", false, false},
	'e': {"KeyE", false, false}, 'f': {"KeyF", false, false}, 'g': {"KeyG", false, false}, 'h': {"KeyH", false, false},
	'i': {"KeyI", false, false}, 'j': {"KeyJ", false, false}, 'k': {"KeyK", false, false}, 'l': {"KeyL", false, false},
	'm': {"Semicolon", false, false}, 'n': {"KeyN", false, false}, 'o': {"KeyO", false, false}, 'p': {"KeyP", false, false},
	'q': {"KeyA", false, false}, 'r': {"KeyR", false, false}, 's': {"KeyS", false, false}, 't': {"KeyT", false, false},
	'u': {"KeyU", false, false}, 'v': {"KeyV", false, false}, 'w': {"KeyZ", false, false}, 'x': {"KeyX", false, false},
	'y': {"KeyY", false, false}, 'z': {"KeyW", false, false},
	'A': {"KeyQ", true, false}, 'B': {"KeyB", true, false}, 'C': {"KeyC", true, false}, 'D': {"KeyD", true, false},
	'E': {"KeyE", true, false}, 'F': {"KeyF", true, false}, 'G': {"KeyG", true, false}, 'H': {"KeyH", true, false},
	'I': {"KeyI", true, false}, 'J': {"KeyJ", true, false}, 'K': {"KeyK", true, false}, 'L': {"KeyL", true, false},
	'M': {"Semicolon", true, false}, 'N': {"KeyN", true, false}, 'O': {"KeyO", true, false}, 'P': {"KeyP", true, false},
	'Q': {"KeyA", true, false}, 'R': {"KeyR", true, false}, 'S': {"KeyS", true, false}, 'T': {"KeyT", true, false},
	'U': {"KeyU", true, false}, 'V': {"KeyV", true, false}, 'W': {"KeyZ", true, false}, 'X': {"KeyX", true, false},
	'Y': {"KeyY", true, false}, 'Z': {"KeyW", true, false},
	// French number row (unshifted = symbols, shifted = digits!)
	'&': {"Digit1", false, false}, '1': {"Digit1", true, false},
	'é': {"Digit2", false, false}, '2': {"Digit2", true, false},
	'"': {"Digit3", false, false}, '3': {"Digit3", true, false},
	'\'': {"Digit4", false, false}, '4': {"Digit4", true, false},
	'(': {"Digit5", false, false}, '5': {"Digit5", true, false},
	'-': {"Digit6", false, false}, '6': {"Digit6", true, false},
	'è': {"Digit7", false, false}, '7': {"Digit7", true, false},
	'_': {"Digit8", false, false}, '8': {"Digit8", true, false},
	'ç': {"Digit9", false, false}, '9': {"Digit9", true, false},
	'à': {"Digit0", false, false}, '0': {"Digit0", true, false},
	// French AltGr symbols
	'~': {"Digit2", false, true}, '#': {"Digit3", false, true}, '{': {"Digit4", false, true},
	'[': {"Digit5", false, true}, '|': {"Digit6", false, true}, '`': {"Digit7", false, true},
	'\\': {"Digit8", false, true}, '^': {"Digit9", false, true}, '@': {"Digit0", false, true},
	']': {"Minus", false, true}, '}': {"Equal", false, true}, '€': {"KeyE", false, true},
	// French punctuation
	')': {"Minus", false, false}, '°': {"Minus", true, false},
	'=': {"Equal", false, false}, '+': {"Equal", true, false},
	'$': {"BracketRight", false, false}, '£': {"BracketRight", true, false},
	'ù': {"Quote", false, false}, '%': {"Quote", true, false},
	'*': {"Backslash", false, false}, 'µ': {"Backslash", true, false},
	',': {"KeyM", false, false}, '?': {"KeyM", true, false},
	';': {"Comma", false, false}, '.': {"Comma", true, false},
	':': {"Period", false, false}, '/': {"Period", true, false},
	'!': {"Slash", false, false}, '§': {"Slash", true, false},
	'<': {"IntlBackslash", false, false}, '>': {"IntlBackslash", true, false},
	// Whitespace
	' ': {"Space", false, false}, '\t': {"Tab", false, false}, '\n': {"Enter", false, false}, '\r': {"Enter", false, false},
}

// layoutRegistry maps layout codes to their character mappings.
var layoutRegistry = map[string]keyboardLayout{
	"en-US": layoutEnUS,
	"en-UK": layoutEnUK,
	"de-DE": layoutDeDE,
	"fr-FR": layoutFrFR,
}

// getKeyboardLayout returns the layout for the given code, falling back to en-US.
func getKeyboardLayout(layoutCode string) keyboardLayout {
	if layout, ok := layoutRegistry[layoutCode]; ok {
		return layout
	}
	return layoutEnUS
}

// Maximum clipboard text size for typing (100KB - matches typical use cases, larger content takes too long).
const maxClipboardTypeSize = 100 * 1024

// typeClipboardText types the given text via keyboard macro.
// Uses the keyboard layout from config.
// This implements VNC clipboard-as-keystrokes functionality.
// Respects VNCClipboardEnabled setting.
// Rejects text larger than 100KB to prevent accidental paste of large content.
func (c *Connection) typeClipboardText(text []byte) error {
	if !c.server.deps.Config.GetVNCClipboardEnabled() {
		c.server.deps.Logger.Debug().Int("bytes", len(text)).Msg("VNC clipboard: typing disabled, ignoring")
		return nil
	}

	if len(text) == 0 {
		return nil
	}

	if len(text) > maxClipboardTypeSize {
		if c.server.deps.Logger.Debug().Enabled() {
			c.server.deps.Logger.Debug().Int("bytes", len(text)).Int("maxBytes", maxClipboardTypeSize).Msg("VNC clipboard: text too large, ignoring")
		}
		return nil
	}

	layoutCode := "en-US" // Default layout
	// Note: KeyboardLayout config would need to be added to the Config interface
	// For now, using default en-US layout

	pressDelay, releaseDelay := c.getClipboardDelays()
	steps, skipped := c.textToMacroSteps(text, layoutCode, pressDelay, releaseDelay)
	if len(steps) == 0 {
		if c.server.deps.Logger.Debug().Enabled() {
			c.server.deps.Logger.Debug().Int("skipped", skipped).Str("layout", layoutCode).Msg("VNC clipboard: no typeable characters")
		}
		return nil
	}

	if skipped > 0 && c.server.deps.Logger.Debug().Enabled() {
		c.server.deps.Logger.Debug().Int("skipped", skipped).Int("typed", len(steps)/2).Str("layout", layoutCode).Msg("VNC clipboard: some characters not in layout")
	}

	err := c.server.deps.HID.KeyboardMacro(steps)
	if err != nil {
		c.server.deps.Logger.Error().Err(err).Int("chars", len(steps)/2).Msg("VNC clipboard: keyboard macro failed")
	}
	return err
}

// getClipboardDelays returns press and release delays based on config.VNCPasteDelayMs.
// Returns (pressDelayMs, releaseDelayMs) - each is half the configured delay.
func (c *Connection) getClipboardDelays() (int, int) {
	delay := max(c.server.deps.Config.GetVNCPasteDelayMs(), 0)
	// Split delay evenly between press and release
	// For odd values, give extra ms to release for better compatibility
	pressDelay := delay / 2
	releaseDelay := delay - pressDelay
	return pressDelay, releaseDelay
}

// textToMacroSteps converts text to keyboard macro steps for typing via USB HID.
// Uses the keyboard layout from config.
// Characters not in the layout mapping are skipped.
// pressDelayMs and releaseDelayMs control typing speed.
// Returns the macro steps and count of skipped characters.
func (c *Connection) textToMacroSteps(text []byte, layoutCode string, pressDelayMs, releaseDelayMs int) ([]KeyboardMacroStep, int) {
	layout := getKeyboardLayout(layoutCode)

	// Pre-allocate: each character needs 2 steps (press + release)
	steps := make([]KeyboardMacroStep, 0, len(text)*2)
	skipped := 0

	// Convert bytes to string for proper UTF-8 rune iteration
	textStr := string(text)

	for _, char := range textStr {
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
			modifier |= hidModShiftLeft
		}
		if combo.altRight {
			modifier |= hidModAltRight
		}

		// Key press step
		keys := make([]uint8, HidKeyBufferSize)
		keys[0] = hidKey
		steps = append(steps, KeyboardMacroStep{
			Modifier: modifier,
			Keys:     keys,
			Delay:    uint16(pressDelayMs),
		})

		// Key release step (all zeros)
		releaseKeys := make([]uint8, HidKeyBufferSize)
		steps = append(steps, KeyboardMacroStep{
			Modifier: 0,
			Keys:     releaseKeys,
			Delay:    uint16(releaseDelayMs),
		})
	}

	return steps, skipped
}
