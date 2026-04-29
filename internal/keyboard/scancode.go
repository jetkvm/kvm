// internal/keyboard/scancode.go
//
// Infers USB HID Usage IDs from physical key positions in the KLE grid.
//
// Standard keyboard grids are well-defined. This table covers ANSI and ISO
// standard layouts. For non-standard boards (JIS, ABNT2, ortholinear).
// You can override individual key scancodes post-parse with a "scancodes"
// dictionary in the KLE.json file.
//
// HID Usage Table: USB HID Usage Tables 1.3, Keyboard/Keypad Page (0x07)
// https://usb.org/sites/default/files/hut1_3_0.pdf

package keyboard

import "math"

// HID Usage IDs for standard keys (page 0x07)
const (
	// Letters
	hidA uint8 = 0x04
	hidB uint8 = 0x05
	hidC uint8 = 0x06
	hidD uint8 = 0x07
	hidE uint8 = 0x08
	hidF uint8 = 0x09
	hidG uint8 = 0x0A
	hidH uint8 = 0x0B
	hidI uint8 = 0x0C
	hidJ uint8 = 0x0D
	hidK uint8 = 0x0E
	hidL uint8 = 0x0F
	hidM uint8 = 0x10
	hidN uint8 = 0x11
	hidO uint8 = 0x12
	hidP uint8 = 0x13
	hidQ uint8 = 0x14
	hidR uint8 = 0x15
	hidS uint8 = 0x16
	hidT uint8 = 0x17
	hidU uint8 = 0x18
	hidV uint8 = 0x19
	hidW uint8 = 0x1A
	hidX uint8 = 0x1B
	hidY uint8 = 0x1C
	hidZ uint8 = 0x1D

	// Number row
	hidN1 uint8 = 0x1E
	hidN2 uint8 = 0x1F
	hidN3 uint8 = 0x20
	hidN4 uint8 = 0x21
	hidN5 uint8 = 0x22
	hidN6 uint8 = 0x23
	hidN7 uint8 = 0x24
	hidN8 uint8 = 0x25
	hidN9 uint8 = 0x26
	hidN0 uint8 = 0x27

	// Punctuation
	hidEnter     uint8 = 0x28
	hidEscape    uint8 = 0x29
	hidBackspace uint8 = 0x2A
	hidTab       uint8 = 0x2B
	hidSpace     uint8 = 0x2C
	hidMinus     uint8 = 0x2D // - _
	hidEqual     uint8 = 0x2E // = +
	hidLBracket  uint8 = 0x2F // [ {
	hidRBracket  uint8 = 0x30 // ] }
	hidBackslash uint8 = 0x31 // \ |
	hidHash      uint8 = 0x32 // # ~ (ISO layouts only)
	hidSemicolon uint8 = 0x33 // ; :
	hidQuote     uint8 = 0x34 // ' "
	hidGrave     uint8 = 0x35 // ` ~
	hidComma     uint8 = 0x36 // , <
	hidPeriod    uint8 = 0x37 // . >
	hidSlash     uint8 = 0x38 // / ?

	hidF1  uint8 = 0x3A
	hidF2  uint8 = 0x3B
	hidF3  uint8 = 0x3C
	hidF4  uint8 = 0x3D
	hidF5  uint8 = 0x3E
	hidF6  uint8 = 0x3F
	hidF7  uint8 = 0x40
	hidF8  uint8 = 0x41
	hidF9  uint8 = 0x42
	hidF10 uint8 = 0x43
	hidF11 uint8 = 0x44
	hidF12 uint8 = 0x45

	// Navigation cluster
	hidPrintScreen uint8 = 0x46
	hidScrollLock  uint8 = 0x47
	hidPause       uint8 = 0x48
	hidInsert      uint8 = 0x49
	hidHome        uint8 = 0x4A
	hidPageUp      uint8 = 0x4B
	hidDelete      uint8 = 0x4C
	hidEnd         uint8 = 0x4D
	hidPageDown    uint8 = 0x4E

	// Arrow keys
	hidArrowRight uint8 = 0x4F
	hidArrowLeft  uint8 = 0x50
	hidArrowDown  uint8 = 0x51
	hidArrowUp    uint8 = 0x52

	// Numpad
	hidNumLock uint8 = 0x53 // aka CLEAR
	hidKPSlash uint8 = 0x54
	hidKPStar  uint8 = 0x55
	hidKPMinus uint8 = 0x56
	hidKPPlus  uint8 = 0x57
	hidKPEnter uint8 = 0x58
	hidKP1     uint8 = 0x59
	hidKP2     uint8 = 0x5A
	hidKP3     uint8 = 0x5B
	hidKP4     uint8 = 0x5C
	hidKP5     uint8 = 0x5D
	hidKP6     uint8 = 0x5E
	hidKP7     uint8 = 0x5F
	hidKP8     uint8 = 0x60
	hidKP9     uint8 = 0x61
	hidKP0     uint8 = 0x62
	hidKPDot   uint8 = 0x63

	// key between LShift and Z on ISO layouts
	hidISOKey uint8 = 0x64 // Non-US ` |

	// Modifiers (Usage Page 0x07)
	hidLCtrl  uint8 = 0xE0
	hidLShift uint8 = 0xE1
	hidLAlt   uint8 = 0xE2
	hidLMeta  uint8 = 0xE3 // aka LGUI (Windows key, Command key)
	hidRCtrl  uint8 = 0xE4
	hidRShift uint8 = 0xE5
	hidRAlt   uint8 = 0xE6 // aka AltGr
	hidRMeta  uint8 = 0xE7 // aka LGUI (Windows key, Command key)

	hidCapsLock uint8 = 0x39

	// Additional keys that may not be present on all layouts
	hidApplication uint8 = 0x65

	// These keys are less common and may not be present on many keyboards,
	// but we document them for completeness.
	/*
		hidPower       uint8 = 0x66
		hidKPEquals    uint8 = 0x67

		hidF13 uint8 = 0x68
		hidF14 uint8 = 0x69
		hidF15 uint8 = 0x6A
		hidF16 uint8 = 0x6B
		hidF17 uint8 = 0x6C
		hidF18 uint8 = 0x6D
		hidF19 uint8 = 0x6E
		hidF20 uint8 = 0x6F
		hidF21 uint8 = 0x70
		hidF22 uint8 = 0x71
		hidF23 uint8 = 0x72
		hidF24 uint8 = 0x73

		hidExecute uint8 = 0x74
		hidHelp    uint8 = 0x75
		hidMenu    uint8 = 0x76
		hidSelect  uint8 = 0x77
		hidStop    uint8 = 0x78
		hidAgain   uint8 = 0x79
		hidUndo    uint8 = 0x7A
		hidCut     uint8 = 0x7B
		hidCopy    uint8 = 0x7C
		hidPaste   uint8 = 0x7D
		hidFind    uint8 = 0x7E
		hidMute    uint8 = 0x7F

		hidVolumeUp   uint8 = 0x80
		hidVolumeDown uint8 = 0x81

		hidLockingCapsLock   uint8 = 0x82
		hidLockingNumLock    uint8 = 0x83
		hidLockingScrollLock uint8 = 0x84

		hidKPComma      uint8 = 0x85
		hidKPEqualAS400 uint8 = 0x86

		hidIntl1 uint8 = 0x87
		hidIntl2 uint8 = 0x88
		hidIntl3 uint8 = 0x89
		hidIntl4 uint8 = 0x8A
		hidIntl5 uint8 = 0x8B
		hidIntl6 uint8 = 0x8C
		hidIntl7 uint8 = 0x8D
		hidIntl8 uint8 = 0x8E
		hidIntl9 uint8 = 0x8F

		hidLang1 uint8 = 0x90
		hidLang2 uint8 = 0x91
		hidLang3 uint8 = 0x92
		hidLang4 uint8 = 0x93
		hidLang5 uint8 = 0x94
		hidLang6 uint8 = 0x95
		hidLang7 uint8 = 0x96
		hidLang8 uint8 = 0x97
		hidLang9 uint8 = 0x98

		hidErase      uint8 = 0x99
		hidSysReq     uint8 = 0x9A
		hidCancel     uint8 = 0x9B
		hidClear      uint8 = 0x9C
		hidPrior      uint8 = 0x9D
		hidReturn     uint8 = 0x9E
		hidSeparator  uint8 = 0x9F
		hidOut        uint8 = 0xA0
		hidOper       uint8 = 0xA1
		hidClearAgain uint8 = 0xA2
		hidCrSel      uint8 = 0xA3
		hidExSel      uint8 = 0xA4

		hidKP00               uint8 = 0xB0
		hidKP000              uint8 = 0xB1
		hidThousandsSeparator uint8 = 0xB2
		hidDecimalSeparator   uint8 = 0xB3
		hidCurrencyUnit       uint8 = 0xB4
		hidCurrencySubUnit    uint8 = 0xB5

		hidKPLeftParen  uint8 = 0xB6
		hidKPRightParen uint8 = 0xB7
		hidKPLeftBrace  uint8 = 0xB8
		hidKPRightBrace uint8 = 0xB9

		hidKPTab       uint8 = 0xBA
		hidKPBackspace uint8 = 0xBB

		hidKPA uint8 = 0xBC
		hidKPB uint8 = 0xBD
		hidKPC uint8 = 0xBE
		hidKPD uint8 = 0xBF
		hidKPE uint8 = 0xC0
		hidKPF uint8 = 0xC1

		hidKPXor     uint8 = 0xC2
		hidKPCaret   uint8 = 0xC3
		hidKPPercent uint8 = 0xC4
		hidKPLess    uint8 = 0xC5
		hidKPGreater uint8 = 0xC6
		hidKPAnd     uint8 = 0xC7
		hidKPAndAnd  uint8 = 0xC8
		hidKPOr      uint8 = 0xC9
		hidKPOrOr    uint8 = 0xCA
		hidKPColon   uint8 = 0xCC
		hidKPHash    uint8 = 0xCD
		hidKPSpace   uint8 = 0xCE
		hidKPAt      uint8 = 0xCF
		hidKPExclam  uint8 = 0xD0

		hidKPMemStore    uint8 = 0xD1
		hidKPMemRecall   uint8 = 0xD2
		hidKPMemClear    uint8 = 0xD3
		hidKPMemAdd      uint8 = 0xD4
		hidKPMemSubtract uint8 = 0xD5
		hidKPMemMultiply uint8 = 0xD6
		hidKPMemDivide   uint8 = 0xD7

		hidKPPlusMinus  uint8 = 0xD8
		hidKPClear      uint8 = 0xD9
		hidKPClearEntry uint8 = 0xDA

		hidKPBinary      uint8 = 0xDB
		hidKPOctal       uint8 = 0xDC
		hidKPDecimal     uint8 = 0xDD
		hidKPHexadecimal uint8 = 0xDE
	*/

	hidUnknown uint8 = 0x00
)

// ---------------------------------------------------------------------------
// Position table
// ---------------------------------------------------------------------------

// posEntry maps an x-start position to an HID Usage ID.
type posEntry struct {
	xStart   float64
	scancode uint8
}

// Standard ANSI/ISO key grid.
//
// positionTable maps integer row index (y rounded) to a sorted list of
// (x_start, scancode) pairs. A key at position x matches the entry with
// the largest xStart that is still <= x.
// Full-size position table. Positions match the real KLE coordinate output
// from keyboard-layout-editor.com for standard ANSI 104 / ISO 105 layouts.
//
// Note: The standard KLE template has a y:0.5 gap between the function row
// and the number row, so rows are at y=0, 1.5, 2.5, 3.5, 4.5, 5.5.
// We use math.Round(y) in inferScancodeWithTable, so:
//
//	y=0.00 → row 0 (function)
//	y=1.50 → row 2 (number)    — rounds to 2, not 1
//	y=2.50 → row 3 (qwerty)    — rounds to 3, not 2
//	y=3.50 → row 4 (home)      — rounds to 4
//	y=4.50 → row 5 (shift)     — rounds to 5, not 4
//	y=5.50 → row 6 (modifiers) — rounds to 6, not 5
var fullSizeTable = map[int][]posEntry{
	// Function row (y=0)
	0: {
		{0, hidEscape},
		{2, hidF1}, {3, hidF2}, {4, hidF3}, {5, hidF4},
		{6.5, hidF5}, {7.5, hidF6}, {8.5, hidF7}, {9.5, hidF8},
		{11, hidF9}, {12, hidF10}, {13, hidF11}, {14, hidF12},
		{15.25, hidPrintScreen}, {16.25, hidScrollLock}, {17.25, hidPause},
	},
	// Number row (y≈1.5 → rounds to 2)
	2: {
		{0, hidGrave},
		{1, hidN1}, {2, hidN2}, {3, hidN3}, {4, hidN4}, {5, hidN5},
		{6, hidN6}, {7, hidN7}, {8, hidN8}, {9, hidN9}, {10, hidN0},
		{11, hidMinus}, {12, hidEqual}, {13, hidBackspace},
		// Nav cluster
		{15.25, hidInsert}, {16.25, hidHome}, {17.25, hidPageUp},
		// Numpad
		{18.5, hidNumLock}, {19.5, hidKPSlash}, {20.5, hidKPStar}, {21.5, hidKPMinus},
	},
	// QWERTY row (y≈2.5 → rounds to 3)
	3: {
		{0, hidTab},
		{1.5, hidQ}, {2.5, hidW}, {3.5, hidE}, {4.5, hidR}, {5.5, hidT},
		{6.5, hidY}, {7.5, hidU}, {8.5, hidI}, {9.5, hidO}, {10.5, hidP},
		{11.5, hidLBracket}, {12.5, hidRBracket}, {13.5, hidBackslash},
		// Nav cluster
		{15.25, hidDelete}, {16.25, hidEnd}, {17.25, hidPageDown},
		// Numpad
		{18.5, hidKP7}, {19.5, hidKP8}, {20.5, hidKP9}, {21.5, hidKPPlus},
	},
	// Home row (y≈3.5 → rounds to 4)
	4: {
		{0, hidCapsLock},
		{1.75, hidA}, {2.75, hidS}, {3.75, hidD}, {4.75, hidF}, {5.75, hidG},
		{6.75, hidH}, {7.75, hidJ}, {8.75, hidK}, {9.75, hidL},
		{10.75, hidSemicolon}, {11.75, hidQuote}, {12.75, hidEnter},
		// Numpad
		{18.5, hidKP4}, {19.5, hidKP5}, {20.5, hidKP6},
	},
	// Shift row (y≈4.5 → rounds to 5)
	5: {
		{0, hidLShift},
		{1.25, hidISOKey}, // ISO key (between LShift and Z on ISO layouts)
		{2.25, hidZ},      // ANSI Z starts at 2.25 (after 2.25u LShift)
		{3.25, hidX}, {4.25, hidC}, {5.25, hidV}, {6.25, hidB},
		{7.25, hidN}, {8.25, hidM},
		{9.25, hidComma}, {10.25, hidPeriod}, {11.25, hidSlash},
		{12.25, hidRShift},
		// Arrow up
		{16.25, hidArrowUp},
		// Numpad
		{18.5, hidKP1}, {19.5, hidKP2}, {20.5, hidKP3}, {21.5, hidKPEnter},
	},
	// Modifier row (y≈5.5 → rounds to 6)
	6: {
		{0, hidLCtrl}, {1.25, hidLMeta}, {2.5, hidLAlt},
		{3.75, hidSpace},
		{10, hidRAlt}, {11.25, hidRMeta}, {12.5, hidApplication},
		{13.75, hidRCtrl},
		// Arrow keys
		{15.25, hidArrowLeft}, {16.25, hidArrowDown}, {17.25, hidArrowRight},
		// Numpad
		{18.5, hidKP0}, {20.5, hidKPDot},
	},
}

// Compact layout table — for keyboards WITHOUT the y:0.5 gap between
// the function row and number row (75%, TKL).
// Rows are at y=0, 1, 2, 3, 4, 5 (no fractional Y offsets).
// The main typing area positions are identical to full-size.
//
// Note: 60%/65% keyboards that lack a function row are NOT supported
// by this table — their number row at y=0 would be misinterpreted as
// F-keys. Those layouts need scancode overrides in their KLE metadata.
var compactTable = map[int][]posEntry{
	// Row 0: function row (75%/TKL — Esc and F1-F12 packed together)
	0: {
		{0, hidEscape},
		{1, hidF1}, {2, hidF2}, {3, hidF3}, {4, hidF4},
		{5, hidF5}, {6, hidF6}, {7, hidF7}, {8, hidF8},
		{9, hidF9}, {10, hidF10}, {11, hidF11}, {12, hidF12},
		// Right-side keys (75%: PrtSc/ScrLk area)
		{13, hidPrintScreen}, {14, hidScrollLock}, {15, hidPause},
	},
	// Number row (y=1)
	1: {
		{0, hidGrave},
		{1, hidN1}, {2, hidN2}, {3, hidN3}, {4, hidN4}, {5, hidN5},
		{6, hidN6}, {7, hidN7}, {8, hidN8}, {9, hidN9}, {10, hidN0},
		{11, hidMinus}, {12, hidEqual}, {13, hidBackspace},
		// 75%: nav key on right
		{15, hidInsert}, {16, hidHome},
	},
	// QWERTY row
	2: {
		{0, hidTab},
		{1.5, hidQ}, {2.5, hidW}, {3.5, hidE}, {4.5, hidR}, {5.5, hidT},
		{6.5, hidY}, {7.5, hidU}, {8.5, hidI}, {9.5, hidO}, {10.5, hidP},
		{11.5, hidLBracket}, {12.5, hidRBracket}, {13.5, hidBackslash},
		{15, hidDelete}, {16, hidPageUp},
	},
	// Home row
	3: {
		{0, hidCapsLock},
		{1.75, hidA}, {2.75, hidS}, {3.75, hidD}, {4.75, hidF}, {5.75, hidG},
		{6.75, hidH}, {7.75, hidJ}, {8.75, hidK}, {9.75, hidL},
		{10.75, hidSemicolon}, {11.75, hidQuote}, {12.75, hidEnter},
		{15, hidPageDown}, {16, hidEnd},
	},
	// Shift row
	4: {
		{0, hidLShift},
		{1.25, hidISOKey},
		{2.25, hidZ},
		{3.25, hidX}, {4.25, hidC}, {5.25, hidV}, {6.25, hidB},
		{7.25, hidN}, {8.25, hidM},
		{9.25, hidComma}, {10.25, hidPeriod}, {11.25, hidSlash},
		{12.25, hidRShift},
		// Arrows (75%)
		{14.25, hidArrowUp},
		{15, hidEnd},
	},
	// Modifier row
	5: {
		{0, hidLCtrl}, {1.25, hidLMeta}, {2.5, hidLAlt},
		{3.75, hidSpace},
		{10, hidRAlt}, {11, hidRMeta}, {12, hidApplication}, {13, hidRCtrl},
		// Arrows
		{13.25, hidArrowLeft}, {14.25, hidArrowDown}, {15.25, hidArrowRight},
	},
}

// selectPositionTable chooses the appropriate scancode table based on
// the keyboard's physical dimensions and key count.
func selectPositionTable(boardW, boardH float64, keyCount int) map[int][]posEntry {
	// Full-size: wide board or many keys (104/105/109)
	if boardW > 20 || keyCount >= 100 {
		return fullSizeTable
	}
	// Compact layouts with a function row (75%, TKL): 6+ rows, 70+ keys
	if boardH >= 6 && keyCount >= 70 {
		return compactTable
	}
	// Everything else (60%, 65%, or very small boards): use full-size table
	// as a best-effort fallback. These layouts will likely need scancode
	// overrides in their KLE metadata for correct mapping.
	return fullSizeTable
}

// inferScancodeWithTable returns the USB HID Usage ID for a key at position
// (x, y) using the given position table. For ISO Enter (h >= 2, x < 15),
// returns hidEnter directly.
//
// Uses closest-match with tolerance: finds the table entry with minimum
// distance to the key position, and matches if distance < maxDistance.
func inferScancodeWithTable(x, y, w, h float64, table map[int][]posEntry) uint8 {
	// ISO / big-ass Enter: spans two rows, in the main typing area (x < 15)
	// Don't match numpad + or numpad Enter which also have h >= 2
	if h >= 2 && x < 15 {
		return hidEnter
	}

	// ISO hash key (#/~): on ISO layouts, the narrow key at x≈12.75 on the
	// home row is the hash key (w≈1), not Enter (w≥2). The width check alone
	// distinguishes them — no row index check needed, so this works for both
	// full-size (home row at y≈3.5→4) and compact (home row at y=3) tables.
	if approxEq(x, 12.75) && w < 1.5 {
		return hidHash
	}

	rowIdx := int(math.Round(y))
	row, ok := table[rowIdx]
	if !ok {
		return hidUnknown
	}

	// Closest-match algorithm: find the entry with minimum distance to x,
	// match if distance <= maxDistance tolerance (0.3u).
	// This handles positional variance across different keyboard layouts.
	const maxDistance = 0.3 // tolerance for key position variance
	var closest = hidUnknown
	var minDist = math.Inf(1)
	for _, entry := range row {
		dist := math.Abs(entry.xStart - x)
		if dist < minDist && dist <= maxDistance {
			minDist = dist
			closest = entry.scancode
		}
	}
	return closest // Will still be hidUnknown if the closest entry is beyond tolerance
}
