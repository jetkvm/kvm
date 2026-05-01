package rfb

// HIDFromKeysym translates an X11 keysym (as carried in an RFB
// KeyEvent message) to a USB HID Usage ID. The boolean return value
// is false when the keysym has no mapping; the caller should drop
// the event in that case.
//
// The mapping intentionally returns the *unshifted* HID code for
// shifted ASCII characters (e.g. '!' → 0x1E, the same as '1').
// Standard VNC clients send a separate Shift_L/Shift_R keysym press
// alongside the symbol, and the host translates the combination
// itself. This matches what TigerVNC and PiKVM do.
//
// Modifier keysyms (Shift_L / Control_L / etc.) map to HID Usage
// IDs 0xE0–0xE7. JetKVM's KeypressReport recognises these and
// updates the modifier byte automatically.
func HIDFromKeysym(keysym uint32) (byte, bool) {
	hid, ok := keysymToHID[keysym]
	return hid, ok
}

// IsModifierKeysym reports whether the keysym refers to one of the
// shift / control / alt / super / meta keys.
func IsModifierKeysym(keysym uint32) bool {
	switch keysym {
	case keysymShiftL, keysymShiftR,
		keysymControlL, keysymControlR,
		keysymAltL, keysymAltR,
		keysymMetaL, keysymMetaR,
		keysymSuperL, keysymSuperR,
		keysymHyperL, keysymHyperR,
		keysymISOLevel3Shift:
		return true
	}
	return false
}

// X11 keysym constants used by the table below. Source: X11/keysymdef.h.
const (
	// Whitespace / control
	keysymBackSpace   uint32 = 0xff08
	keysymTab         uint32 = 0xff09
	keysymLinefeed    uint32 = 0xff0a
	keysymClear       uint32 = 0xff0b
	keysymReturn      uint32 = 0xff0d
	keysymPause       uint32 = 0xff13
	keysymScrollLock  uint32 = 0xff14
	keysymSysReq      uint32 = 0xff15
	keysymEscape      uint32 = 0xff1b
	keysymDelete      uint32 = 0xffff
	keysymPrint       uint32 = 0xff61

	// Cursor / nav
	keysymHome     uint32 = 0xff50
	keysymLeft     uint32 = 0xff51
	keysymUp       uint32 = 0xff52
	keysymRight    uint32 = 0xff53
	keysymDown     uint32 = 0xff54
	keysymPageUp   uint32 = 0xff55
	keysymPageDown uint32 = 0xff56
	keysymEnd      uint32 = 0xff57
	keysymBegin    uint32 = 0xff58
	keysymInsert   uint32 = 0xff63
	keysymMenu     uint32 = 0xff67

	// Lock / numpad
	keysymNumLock     uint32 = 0xff7f
	keysymKPSpace     uint32 = 0xff80
	keysymKPTab       uint32 = 0xff89
	keysymKPEnter     uint32 = 0xff8d
	keysymKPHome      uint32 = 0xff95
	keysymKPLeft      uint32 = 0xff96
	keysymKPUp        uint32 = 0xff97
	keysymKPRight     uint32 = 0xff98
	keysymKPDown      uint32 = 0xff99
	keysymKPPageUp    uint32 = 0xff9a
	keysymKPPageDown  uint32 = 0xff9b
	keysymKPEnd       uint32 = 0xff9c
	keysymKPBegin     uint32 = 0xff9d
	keysymKPInsert    uint32 = 0xff9e
	keysymKPDelete    uint32 = 0xff9f
	keysymKPEqual     uint32 = 0xffbd
	keysymKPMultiply  uint32 = 0xffaa
	keysymKPAdd       uint32 = 0xffab
	keysymKPSeparator uint32 = 0xffac
	keysymKPSubtract  uint32 = 0xffad
	keysymKPDecimal   uint32 = 0xffae
	keysymKPDivide    uint32 = 0xffaf
	keysymKP0         uint32 = 0xffb0

	// Function keys (F1=0xffbe ... F12=0xffc9, F13=0xffca ... F24=0xffd5)
	keysymF1  uint32 = 0xffbe
	keysymF13 uint32 = 0xffca

	// Modifiers
	keysymShiftL         uint32 = 0xffe1
	keysymShiftR         uint32 = 0xffe2
	keysymControlL       uint32 = 0xffe3
	keysymControlR       uint32 = 0xffe4
	keysymCapsLock       uint32 = 0xffe5
	keysymShiftLock      uint32 = 0xffe6
	keysymMetaL          uint32 = 0xffe7
	keysymMetaR          uint32 = 0xffe8
	keysymAltL           uint32 = 0xffe9
	keysymAltR           uint32 = 0xffea
	keysymSuperL         uint32 = 0xffeb
	keysymSuperR         uint32 = 0xffec
	keysymHyperL         uint32 = 0xffed
	keysymHyperR         uint32 = 0xffee
	keysymISOLevel3Shift uint32 = 0xfe03 // AltGr
)

// USB HID Usage IDs used by the table. Mirrors the constants defined
// in [internal/usbgadget/hid_keyboard.go] for the modifiers; the rest
// follow the standard 85/101/102-key layout.
const (
	hidA               byte = 0x04
	hid1               byte = 0x1e
	hidEnter           byte = 0x28
	hidEscape          byte = 0x29
	hidBackspace       byte = 0x2a
	hidTab             byte = 0x2b
	hidSpace           byte = 0x2c
	hidMinus           byte = 0x2d
	hidEqual           byte = 0x2e
	hidLeftBracket     byte = 0x2f
	hidRightBracket    byte = 0x30
	hidBackslash       byte = 0x31
	hidSemicolon       byte = 0x33
	hidApostrophe      byte = 0x34
	hidGrave           byte = 0x35
	hidComma           byte = 0x36
	hidPeriod          byte = 0x37
	hidSlash           byte = 0x38
	hidCapsLock        byte = 0x39
	hidF1              byte = 0x3a
	hidPrintScreen     byte = 0x46
	hidScrollLock      byte = 0x47
	hidPause           byte = 0x48
	hidInsert          byte = 0x49
	hidHome            byte = 0x4a
	hidPageUp          byte = 0x4b
	hidDelete          byte = 0x4c
	hidEnd             byte = 0x4d
	hidPageDown        byte = 0x4e
	hidArrowRight      byte = 0x4f
	hidArrowLeft       byte = 0x50
	hidArrowDown       byte = 0x51
	hidArrowUp         byte = 0x52
	hidNumLock         byte = 0x53
	hidKpDivide        byte = 0x54
	hidKpMultiply      byte = 0x55
	hidKpSubtract      byte = 0x56
	hidKpAdd           byte = 0x57
	hidKpEnter         byte = 0x58
	hidKp1             byte = 0x59
	hidKp0             byte = 0x62
	hidKpDecimal       byte = 0x63
	hidContextMenu     byte = 0x65
	hidKpEqual         byte = 0x67
	hidF13             byte = 0x68

	hidLeftControl  byte = 0xe0
	hidLeftShift    byte = 0xe1
	hidLeftAlt      byte = 0xe2
	hidLeftSuper    byte = 0xe3
	hidRightControl byte = 0xe4
	hidRightShift   byte = 0xe5
	hidRightAlt     byte = 0xe6
	hidRightSuper   byte = 0xe7
)

// keysymToHID is the lookup table used by HIDFromKeysym. Each entry
// is one X11 keysym mapped to one USB HID Usage ID.
var keysymToHID = func() map[uint32]byte {
	m := make(map[uint32]byte, 200)

	// Letters: a–z and A–Z share the same HID code; clients send Shift
	// separately for the uppercase form.
	for i := 0; i < 26; i++ {
		m[uint32('a')+uint32(i)] = hidA + byte(i)
		m[uint32('A')+uint32(i)] = hidA + byte(i)
	}

	// Digits: 1–9 then 0.
	for i := 0; i < 9; i++ {
		m[uint32('1')+uint32(i)] = hid1 + byte(i)
	}
	m[uint32('0')] = hid1 + 9 // 0x27

	// Shifted digits land on the same scancodes (host applies Shift).
	m[uint32('!')] = hid1 + 0
	m[uint32('@')] = hid1 + 1
	m[uint32('#')] = hid1 + 2
	m[uint32('$')] = hid1 + 3
	m[uint32('%')] = hid1 + 4
	m[uint32('^')] = hid1 + 5
	m[uint32('&')] = hid1 + 6
	m[uint32('*')] = hid1 + 7
	m[uint32('(')] = hid1 + 8
	m[uint32(')')] = hid1 + 9 // 0x27

	// Punctuation pairs (shifted/unshifted on the same key).
	m[uint32('-')] = hidMinus
	m[uint32('_')] = hidMinus
	m[uint32('=')] = hidEqual
	m[uint32('+')] = hidEqual
	m[uint32('[')] = hidLeftBracket
	m[uint32('{')] = hidLeftBracket
	m[uint32(']')] = hidRightBracket
	m[uint32('}')] = hidRightBracket
	m[uint32('\\')] = hidBackslash
	m[uint32('|')] = hidBackslash
	m[uint32(';')] = hidSemicolon
	m[uint32(':')] = hidSemicolon
	m[uint32('\'')] = hidApostrophe
	m[uint32('"')] = hidApostrophe
	m[uint32('`')] = hidGrave
	m[uint32('~')] = hidGrave
	m[uint32(',')] = hidComma
	m[uint32('<')] = hidComma
	m[uint32('.')] = hidPeriod
	m[uint32('>')] = hidPeriod
	m[uint32('/')] = hidSlash
	m[uint32('?')] = hidSlash
	m[uint32(' ')] = hidSpace

	// Whitespace / control
	m[keysymBackSpace] = hidBackspace
	m[keysymTab] = hidTab
	m[keysymLinefeed] = hidEnter
	m[keysymReturn] = hidEnter
	m[keysymEscape] = hidEscape
	m[keysymDelete] = hidDelete
	m[keysymPause] = hidPause
	m[keysymScrollLock] = hidScrollLock
	m[keysymSysReq] = hidPrintScreen
	m[keysymPrint] = hidPrintScreen

	// Navigation
	m[keysymHome] = hidHome
	m[keysymLeft] = hidArrowLeft
	m[keysymUp] = hidArrowUp
	m[keysymRight] = hidArrowRight
	m[keysymDown] = hidArrowDown
	m[keysymPageUp] = hidPageUp
	m[keysymPageDown] = hidPageDown
	m[keysymEnd] = hidEnd
	m[keysymInsert] = hidInsert
	m[keysymMenu] = hidContextMenu

	// Numpad
	m[keysymNumLock] = hidNumLock
	m[keysymKPSpace] = hidSpace
	m[keysymKPTab] = hidTab
	m[keysymKPEnter] = hidKpEnter
	m[keysymKPMultiply] = hidKpMultiply
	m[keysymKPAdd] = hidKpAdd
	m[keysymKPSubtract] = hidKpSubtract
	m[keysymKPDecimal] = hidKpDecimal
	m[keysymKPSeparator] = hidKpDecimal // close enough; differs by locale
	m[keysymKPDivide] = hidKpDivide
	m[keysymKPEqual] = hidKpEqual
	// KP_0 ... KP_9 -> 0x62, 0x59, 0x5a, ..., 0x61
	m[keysymKP0] = hidKp0
	for i := 1; i <= 9; i++ {
		m[keysymKP0+uint32(i)] = hidKp1 + byte(i-1)
	}
	// KP nav variants share their non-KP HID codes.
	m[keysymKPHome] = hidHome
	m[keysymKPLeft] = hidArrowLeft
	m[keysymKPUp] = hidArrowUp
	m[keysymKPRight] = hidArrowRight
	m[keysymKPDown] = hidArrowDown
	m[keysymKPPageUp] = hidPageUp
	m[keysymKPPageDown] = hidPageDown
	m[keysymKPEnd] = hidEnd
	m[keysymKPInsert] = hidInsert
	m[keysymKPDelete] = hidDelete

	// Function keys F1–F12 (0xffbe–0xffc9) -> 0x3a–0x45
	for i := 0; i < 12; i++ {
		m[keysymF1+uint32(i)] = hidF1 + byte(i)
	}
	// F13–F24 (0xffca–0xffd5) -> 0x68–0x73
	for i := 0; i < 12; i++ {
		m[keysymF13+uint32(i)] = hidF13 + byte(i)
	}

	// Modifiers
	m[keysymShiftL] = hidLeftShift
	m[keysymShiftR] = hidRightShift
	m[keysymControlL] = hidLeftControl
	m[keysymControlR] = hidRightControl
	m[keysymAltL] = hidLeftAlt
	m[keysymAltR] = hidRightAlt
	m[keysymMetaL] = hidLeftSuper
	m[keysymMetaR] = hidRightSuper
	m[keysymSuperL] = hidLeftSuper
	m[keysymSuperR] = hidRightSuper
	m[keysymHyperL] = hidLeftSuper
	m[keysymHyperR] = hidRightSuper
	m[keysymISOLevel3Shift] = hidRightAlt // AltGr emulated as Right Alt
	m[keysymCapsLock] = hidCapsLock
	m[keysymShiftLock] = hidCapsLock // historic synonym

	return m
}()
