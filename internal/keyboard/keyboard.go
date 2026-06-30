// KLE (keyboard-layout-editor.com) JSON parser and transport type definitions.
//
// This:
//   1. Parses raw KLE JSON into a KeyboardLayout struct
//   2. Infers USB HID scancodes from physical key positions
//   3. Builds a character→HID map for the paste text system
//   4. Validates the resulting layout
//   5. Serialises to JSON for storage and RPC transport
//
// The KeyboardLayout struct (and its JSON representation) is the wire contract
// with the React frontend. Field names must match transport/schema.ts.
//
// KLE format reference:
//   https://github.com/ijprest/keyboard-layout-editor/wiki/Serialized-Data-Format
// HID Usage Table: USB HID Usage Tables 1.3, Keyboard/Keypad Page (0x07) for scancode reference:
//   https://usb.org/sites/default/files/hut1_3_0.pdf

package keyboard

import (
	"cmp"
	"encoding/json"
	"fmt"
	"math"
	"math/bits"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"golang.org/x/text/unicode/norm"
)

const (
	ModNone       uint8 = 0x00
	ModLCtrl      uint8 = 0x01
	ModLShift     uint8 = 0x02
	ModLAlt       uint8 = 0x04
	ModLMeta      uint8 = 0x08
	ModRCtrl      uint8 = 0x10
	ModRShift     uint8 = 0x20
	ModRAlt       uint8 = 0x40
	ModRMeta      uint8 = 0x80
	ModAltGr      uint8 = ModRAlt
	ModShiftAltGr uint8 = ModLShift | ModAltGr
)

// All of the field names and enumerations in JSON are single characters for minimal transport size.

// HIDCombo is a single USB HID key event: Usage ID + modifier byte.
// For dead key compositions, Prefix holds the dead key press that must
// be sent before the main key (e.g. ^+a → â).
type HIDCombo struct {
	Scancode  uint8     `json:"s"`
	Modifiers uint8     `json:"m"`
	Prefix    *HIDCombo `json:"p,omitempty"`
}

// KeyLegends holds the printable legend for each modifier layer.
// Fields are omitted from JSON when nil (no legend for that layer).
type KeyLegends struct {
	Normal     *string `json:"normal,omitempty"`
	Shift      *string `json:"shift,omitempty"`
	AltGr      *string `json:"altgr,omitempty"`
	ShiftAltGr *string `json:"shiftAltgr,omitempty"`
	Kana       *string `json:"kana,omitempty"`
	ShiftKana  *string `json:"shiftKana,omitempty"`
}

// KeyShape is the pre-computed CSS class for the keycap.
type KeyShape string

const (
	ShapeNormal      KeyShape = ""
	ShapeISOEnter    KeyShape = "iso-enter"
	ShapeBigAssEnter KeyShape = "big-ass-enter"
	ShapeSteppedCaps KeyShape = "stepped-caps"
)

// TransportKey is a single fully-resolved keycap.
// JSON field names must match transport/schema.ts TransportKey.
type TransportKey struct {
	X  float64 `json:"x"`
	Y  float64 `json:"y"`
	W  float64 `json:"w"`
	H  float64 `json:"h"`
	W2 float64 `json:"w2,omitempty"`
	H2 float64 `json:"h2,omitempty"`
	X2 float64 `json:"x2,omitempty"`
	Y2 float64 `json:"y2,omitempty"`

	Shape       KeyShape   `json:"shape"`
	Legends     KeyLegends `json:"legends"`
	Scancode    uint8      `json:"scancode"`
	DeadLegends []string   `json:"deadLegends,omitempty"`
	Homing      bool       `json:"homing"`
	Decal       bool       `json:"decal"`
	ControlLike bool       `json:"controlLike"`

	Color     string `json:"color,omitempty"`
	TextColor string `json:"textColor,omitempty"`
}

// KeyboardLayout is the fully processed layout, stored on disk and served
// over JSON-RPC. Field names must match transport/schema.ts KeyboardLayout.
type KeyboardLayout struct {
	ID      string              `json:"id"`
	Name    string              `json:"name"`
	Author  string              `json:"author,omitempty"`
	BoardW  float64             `json:"boardW"`
	BoardH  float64             `json:"boardH"`
	Keys    []TransportKey      `json:"keys"`
	CharMap map[string]HIDCombo `json:"charMap"`
}

// LayoutMeta is the lightweight listing type for the settings dropdown.
type LayoutMeta struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Builtin bool   `json:"builtin"`
}

// LayoutUploadResponse is returned by the HTTP upload endpoint on success.
type LayoutUploadResponse struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	KeyCount int      `json:"keyCount"`
	Warnings []string `json:"warnings,omitempty"`
}

// ---------------------------------------------------------------------------
// KLE raw types (internal to parser)
// ---------------------------------------------------------------------------

// kleRawKey is a KLE property object that appears inline in a row.
// All fields are optional; a field's zero value means "not specified".
type kleRawKey struct {
	// Per-next-key (reset after each key)
	X       float64 `json:"x"`
	Y       float64 `json:"y"`
	W       float64 `json:"w"`
	H       float64 `json:"h"`
	X2      float64 `json:"x2"`
	Y2      float64 `json:"y2"`
	W2      float64 `json:"w2"`
	H2      float64 `json:"h2"`
	Stepped bool    `json:"l"` // stepped
	Homing  bool    `json:"n"` // homing
	Decal   bool    `json:"d"` // decal
	// Persistent (carried forward until changed)
	KeyColor  string `json:"c"`  // key color
	TextColor string `json:"t"`  // text color
	Alignment int    `json:"a"`  // alignment (default 4)
	FontSize  int    `json:"f"`  // font size
	FontSize2 int    `json:"f2"` // secondary font size
}

// kleMetadata is the optional first element of the top-level KLE array.
// Standard KLE fields plus JetKVM extensions (deadKeys, scancodes).
type kleMetadata struct {
	Name      string `json:"name"`
	Author    string `json:"author"`
	Notes     string `json:"notes"`
	Backcolor string `json:"backcolor"`
	Radii     string `json:"radii"`

	// JetKVM extensions (not part of standard KLE format):

	// DeadKeys lists the legend characters that are dead keys on this layout.
	// Only keys whose normal legend matches one of these characters get the
	// CSS "dead" class. If empty/absent, no keys are marked as dead.
	// Example: ["^", "´", "`", "¨", "~"]
	DeadKeys []string `json:"deadKeys,omitempty"`

	// Scancodes maps key index (0-based, in parse order) to a USB HID Usage ID,
	// overriding the position-based inference. Use for non-standard layouts
	// where keys are in unusual positions.
	// Example: {"42": 76} — key #42 gets scancode 0x4C (Delete)
	Scancodes map[string]uint8 `json:"scancodes,omitempty"`
}

// deadKeyToCombining maps dead key legend characters to their Unicode
// combining character equivalents. Used for:
// 1. Detecting which keys are dead keys (for the CSS "dead" class)
// 2. Building composed character entries in the charMap (e.g. ^+a → â)
var deadKeyToCombining = map[rune]rune{
	'^': '\u0302', // combining circumflex accent
	'~': '\u0303', // combining tilde
	'˜': '\u0303', // combining tilde (spacing variant)
	'¨': '\u0308', // combining diaeresis
	'`': '\u0300', // combining grave accent
	'´': '\u0301', // combining acute accent
	'¸': '\u0327', // combining cedilla
	'˛': '\u0328', // combining ogonek
	'˙': '\u0307', // combining dot above
	'˚': '\u030A', // combining ring above (spacing modifier)
	'°': '\u030A', // combining ring above (degree sign — used on Czech keyboards)
	'˝': '\u030B', // combining double acute accent
	'ˇ': '\u030C', // combining caron (háček)
	'˘': '\u0306', // combining breve
}

// ParseKLE parses a KLE JSON byte slice and returns a fully processed
// KeyboardLayout. id and nameOverride allow the caller to set the layout's
// identifier and display name (e.g. from URL params at upload time).
func ParseKLE(rawJSON []byte, id string, nameOverride string) (*KeyboardLayout, error) {
	// KLE top level is a JSON array
	var topLevel []json.RawMessage
	if err := json.Unmarshal(rawJSON, &topLevel); err != nil {
		return nil, fmt.Errorf("KLE data must be a JSON array: %w", err)
	}

	if len(topLevel) == 0 {
		return nil, fmt.Errorf("KLE array is empty")
	}

	// Optional first element: metadata object
	startIdx := 0
	meta := kleMetadata{}
	var firstObj map[string]json.RawMessage
	if err := json.Unmarshal(topLevel[0], &firstObj); err == nil {
		// It's an object — treat as metadata
		_ = json.Unmarshal(topLevel[0], &meta)
		startIdx = 1
	}

	// Resolve display name: override > KLE meta > "Unnamed Layout"
	name := meta.Name
	if nameOverride != "" {
		name = nameOverride
	}
	if name == "" {
		name = "Unnamed Layout"
	}
	name = sanitizeName(name)
	author := sanitizeName(meta.Author)
	if author == "Unnamed Layout" {
		author = "" // don't show default placeholder for author
	}

	// Assign ID if not provided
	if id == "" {
		id = uuid.New().String()
	}

	// Build declared dead key set from metadata
	declaredDeadKeys := make(map[rune]bool)
	for _, ch := range meta.DeadKeys {
		if len(ch) > 0 {
			r, _ := utf8.DecodeRuneInString(ch)
			declaredDeadKeys[r] = true
		}
	}

	// --- Parse rows ---
	var keys []TransportKey

	// Persistent state (carries across rows and keys)
	currentColor := ""
	currentTextColor := ""
	currentY := 0.0

	for rowIdx := startIdx; rowIdx < len(topLevel); rowIdx++ {
		var row []json.RawMessage
		if err := json.Unmarshal(topLevel[rowIdx], &row); err != nil {
			return nil, fmt.Errorf("KLE row %d is not an array: %w", rowIdx, err)
		}

		currentX := 0.0

		// Per-next-key state
		nextX := 0.0
		nextY := 0.0
		nextW := 1.0
		nextH := 1.0
		var nextW2, nextH2, nextX2, nextY2 float64
		hasW2 := false
		nextStepped := false
		nextHoming := false
		nextDecal := false
		rowHadYOffset := false

		for _, item := range row {
			// Try to unmarshal as object (property modifier)
			var props kleRawKey
			if err := json.Unmarshal(item, &props); err == nil {
				// Check it's actually an object (not a string that happened to unmarshal)
				var check map[string]json.RawMessage
				if json.Unmarshal(item, &check) == nil {
					// Property object
					if props.X != 0 {
						nextX = props.X
					}
					if props.Y != 0 {
						nextY = props.Y
						rowHadYOffset = true
					}
					if props.W != 0 {
						nextW = props.W
					}
					if props.H != 0 {
						nextH = props.H
					}
					if props.W2 != 0 {
						nextW2 = props.W2
						hasW2 = true
					}
					if props.H2 != 0 {
						nextH2 = props.H2
					}
					if props.X2 != 0 {
						nextX2 = props.X2
					}
					if props.Y2 != 0 {
						nextY2 = props.Y2
					}
					if props.Stepped {
						nextStepped = true
					}
					if props.Homing {
						nextHoming = true
					}
					if props.Decal {
						nextDecal = true
					}
					// Persistent properties (color, font size) are applied immediately
					// and affect subsequent keys until overridden again.
					// Note: KLE alignment (props.Alignment) is parsed but not consumed —
					// we always use the standard KLE legend slot mapping.
					if props.KeyColor != "" {
						currentColor = props.KeyColor
					}
					if props.TextColor != "" {
						currentTextColor = props.TextColor
					}
					/*
						if props.FontSize != 0 {
							currentFontSize = props.FontSize
						}
						if props.FontSize2 != 0 {
							currentFontSize2 = props.FontSize2
						}
					*/
					continue
				}
			}

			// Try to unmarshal as string (key legend)
			var legendStr string
			if err := json.Unmarshal(item, &legendStr); err != nil {
				return nil, fmt.Errorf("KLE row %d: item is neither object nor string", rowIdx)
			}

			// Apply accumulated offsets
			currentX += nextX
			if rowHadYOffset {
				currentY += nextY
			}

			x := currentX
			y := currentY
			w := nextW
			h := nextH

			legends := parseLegends(legendStr)
			deadLegends := getDeadKeyInfo(legends, declaredDeadKeys)
			shape := detectShape(x, w, h, nextW2, nextH2, nextStepped, hasW2)

			// Scancode is inferred in a post-parse pass after board dimensions are known

			key := TransportKey{
				X:           x,
				Y:           y,
				W:           w,
				H:           h,
				Shape:       shape,
				Legends:     legends,
				DeadLegends: deadLegends,
				Homing:      nextHoming,
				Decal:       nextDecal,
			}

			if hasW2 {
				key.W2 = nextW2
				key.H2 = nextH2
				key.X2 = nextX2
				key.Y2 = nextY2
			}
			if currentColor != "" {
				key.Color = currentColor
			}
			if currentTextColor != "" {
				key.TextColor = currentTextColor
			}

			keys = append(keys, key)

			// Advance x by key width
			currentX += nextW

			// Reset per-next-key state
			nextX = 0
			nextY = 0
			nextW = 1
			nextH = 1
			nextW2 = 0
			nextH2 = 0
			nextX2 = 0
			nextY2 = 0
			hasW2 = false
			nextStepped = false
			nextDecal = false
			nextHoming = false
			rowHadYOffset = false
		}

		currentY += 1.0 // advance to next row
	}

	if len(keys) == 0 {
		return nil, fmt.Errorf("KLE data contains no keys")
	}

	// Compute board dimensions
	boardW := 0.0
	boardH := 0.0
	for _, k := range keys {
		if r := k.X + k.W; r > boardW {
			boardW = r
		}
		if b := k.Y + k.H; b > boardH {
			boardH = b
		}
	}

	// --- Infer scancodes (single pass, after board dimensions are known) ---
	table := selectPositionTable(boardW, boardH, len(keys))
	for i := range keys {
		k := &keys[i]
		if k.Decal {
			continue // decals don't send HID events
		}
		// Check for metadata scancode override first
		keyIdxStr := fmt.Sprintf("%d", i)
		if override, ok := meta.Scancodes[keyIdxStr]; ok {
			k.Scancode = override
			continue
		}
		// Infer from position using the selected table
		k.Scancode = inferScancodeWithTable(k.X, k.Y, k.W, k.H, table)
	}

	for i := range keys {
		keys[i].ControlLike = IsControlScancode(keys[i].Scancode)
	}

	normalizeControlLegendsForDisplay(keys)

	layout := &KeyboardLayout{
		ID:     id,
		Name:   name,
		Author: author,
		BoardW: boardW,
		BoardH: boardH,
		Keys:   keys,
	}

	layout.CharMap = buildCharMap(keys)
	addDeadKeyCompositions(keys, layout.CharMap, declaredDeadKeys)

	if err := validateLayout(layout); err != nil {
		return nil, err
	}

	return layout, nil
}

/**
 * Parse a KLE legend string into supported layer slots.
 *
 * KLE encodes legends as a newline-separated string. Following the standard
 * KLE convention (shift-first), JetKVM maps:
 *
 *   index 0 = shift         (top-left on keycap)
 *   index 1 = normal        (bottom-left on keycap)
 *   index 2 = shift+altgr   (top-right on keycap)
 *   index 3 = altgr         (bottom-right on keycap)
 *
 * For Japanese KLE exports with kana legends, JetKVM also maps:
 *
 *   index 8  = kana         (kana layer)
 *   index 10 = shift+kana   (shifted kana layer)
 *
 * Both JetKVM's built-in layouts and community KLE files use this convention.
 */
func parseLegends(legendStr string) KeyLegends {
	parts := strings.Split(legendStr, "\n")
	get := func(i int) *string {
		if i >= len(parts) {
			return nil
		}
		v := parts[i]
		if v == "" {
			return nil
		}
		return &v
	}

	legends := KeyLegends{
		Normal:     get(1),
		Shift:      get(0),
		AltGr:      get(3),
		ShiftAltGr: get(2),
		Kana:       get(8),
		ShiftKana:  get(10),
	}

	// Collapse same-on-both-layers shorthand: kbdlayout.info exports keys
	// like the numpad as "7\n7", "+\n+", "Space\nSpace" — the same legend on
	// both Shift and Normal slots. Reduce to a single Normal legend so the
	// downstream auto-case logic can fire (e.g. "Q\nQ" → "Q" → Normal="q",
	// Shift="Q") and the keycap renderer doesn't draw the same glyph twice
	// in different quadrants. Same treatment for the AltGr and Kana pairs.
	collapseEqual := func(primary, shifted **string) {
		if *primary != nil && *shifted != nil && **primary == **shifted {
			*shifted = nil
		}
	}
	collapseEqual(&legends.Normal, &legends.Shift)
	collapseEqual(&legends.AltGr, &legends.ShiftAltGr)
	collapseEqual(&legends.Kana, &legends.ShiftKana)

	// Auto-populate shift/normal legends for single-letter keys.
	//
	// Lowercase normal ("q") → generate shift: "Q"
	// Uppercase normal ("Q") with no shift → swap to normal: "q", shift: "Q"
	//
	// The second case handles standard KLE files that use uppercase letter legends
	// (common in externally-authored layouts from keyboard-layout-editor.com).
	//
	// Safety: we require that lower↔upper round-trips cleanly. This prevents
	// collisions with scripts where case mapping is not bijective — notably
	// Turkish dotless-ı (U+0131) and dotted-İ (U+0130), where ToUpper('ı')='I'
	// but ToLower('I')='i', not 'ı'. Those keys must specify both legends explicitly.
	if legends.Normal != nil && legends.Shift == nil {
		r, size := utf8.DecodeRuneInString(*legends.Normal)
		if size == len(*legends.Normal) && r != utf8.RuneError {
			upperR := unicode.ToUpper(r)
			lowerR := unicode.ToLower(r)
			if upperR != lowerR &&
				unicode.ToLower(upperR) == lowerR &&
				unicode.ToUpper(lowerR) == upperR {
				upper := string(upperR)
				lower := string(lowerR)
				if r == lowerR {
					// Already lowercase: just add shift
					legends.Shift = &upper
				} else {
					// Uppercase: swap to normal=lower, shift=upper
					legends.Normal = &lower
					legends.Shift = &upper
				}
			}
		}
	}

	// Mirror case: a single-string KLE legend (no '\n' separator) lands in the
	// Shift slot because position 0 = top-left in our schema. But a single
	// legend conceptually labels the *unmodified* press of the key — "Space"
	// means pressing Space (not Shift+Space), "Tab" means pressing Tab. Only
	// promote when the entire input was one legend; an explicit two-part
	// legend like "Q\n" with an empty normal slot means "shift-only" and
	// should be left alone.
	if len(parts) == 1 && legends.Normal == nil && legends.Shift != nil {
		legend := *legends.Shift
		legends.Normal = legends.Shift
		legends.Shift = nil

		// For single letters, generate the case pair (Normal=q, Shift=Q).
		r, size := utf8.DecodeRuneInString(legend)
		if size == len(legend) && r != utf8.RuneError && unicode.IsLetter(r) {
			upperR := unicode.ToUpper(r)
			lowerR := unicode.ToLower(r)
			if upperR != lowerR &&
				unicode.ToLower(upperR) == lowerR &&
				unicode.ToUpper(lowerR) == upperR {
				lower := string(lowerR)
				upper := string(upperR)
				legends.Normal = &lower
				legends.Shift = &upper
			}
		}
	}

	return legends
}

const maxNameLength = 100

// sanitizeName cleans a user-provided layout name:
// - strips control characters (U+0000–U+001F, U+007F–U+009F)
// - trims whitespace
// - truncates to maxNameLength
func sanitizeName(name string) string {
	cleaned := strings.Map(func(r rune) rune {
		if r < 0x20 || (r >= 0x7F && r <= 0x9F) {
			return -1 // drop control characters
		}
		return r
	}, name)
	cleaned = strings.TrimSpace(cleaned)
	if utf8.RuneCountInString(cleaned) > maxNameLength {
		runes := []rune(cleaned)
		cleaned = string(runes[:maxNameLength])
	}
	if cleaned == "" {
		return "Unnamed Layout"
	}
	return cleaned
}

func approxEq(a, b float64) bool {
	return math.Abs(a-b) < 0.25
}

func detectShape(x, w, h, w2, h2 float64, stepped, hasW2 bool) KeyShape {
	if stepped {
		return ShapeSteppedCaps
	}
	// ISO Enter: w=1.25, h=2, with a second rect
	if hasW2 && approxEq(h, 2) && approxEq(w, 1.25) && approxEq(w2, 1.5) {
		return ShapeISOEnter
	}
	// Big-ass Enter exists in the main typing area; avoid matching tall numpad keys.
	inMainTypingArea := x < 15
	// Big-ass Enter: tall and wider than normal
	if inMainTypingArea && approxEq(w, 1.5) && approxEq(h, 2) {
		return ShapeBigAssEnter
	}
	// Big-ass Enter: tall with no second rect
	if inMainTypingArea && h > 1.5 && !hasW2 {
		return ShapeBigAssEnter
	}
	return ShapeNormal
}

// getDeadKeyInfo checks which legend slots contain dead key characters.
// Returns a slice of slot names ("normal", "shift", "altgr",
// "shift-altgr", "kana", "shift-kana") for each slot where the legend
// character is in declaredDeadKeys.
// Returns an empty slice if no dead keys are declared or no legends match.
func getDeadKeyInfo(legends KeyLegends, declaredDeadKeys map[rune]bool) []string {
	var deadSlots []string

	if len(declaredDeadKeys) == 0 {
		return deadSlots
	}

	if legends.Normal != nil {
		r, _ := utf8.DecodeRuneInString(*legends.Normal)
		if declaredDeadKeys[r] {
			deadSlots = append(deadSlots, "normal")
		}
	}
	if legends.Shift != nil {
		r, _ := utf8.DecodeRuneInString(*legends.Shift)
		if declaredDeadKeys[r] {
			deadSlots = append(deadSlots, "shift")
		}
	}
	if legends.AltGr != nil {
		r, _ := utf8.DecodeRuneInString(*legends.AltGr)
		if declaredDeadKeys[r] {
			deadSlots = append(deadSlots, "altgr")
		}
	}
	if legends.ShiftAltGr != nil {
		r, _ := utf8.DecodeRuneInString(*legends.ShiftAltGr)
		if declaredDeadKeys[r] {
			deadSlots = append(deadSlots, "shift-altgr")
		}
	}
	if legends.Kana != nil {
		r, _ := utf8.DecodeRuneInString(*legends.Kana)
		if declaredDeadKeys[r] {
			deadSlots = append(deadSlots, "kana")
		}
	}
	if legends.ShiftKana != nil {
		r, _ := utf8.DecodeRuneInString(*legends.ShiftKana)
		if declaredDeadKeys[r] {
			deadSlots = append(deadSlots, "shift-kana")
		}
	}

	return deadSlots
}

func buildCharMap(keys []TransportKey) map[string]HIDCombo {
	m := make(map[string]HIDCombo)

	// Sort a copy by position for deterministic first-occurrence behaviour.
	// We must not mutate the original slice — it preserves KLE parse order,
	// which the scancodes metadata override uses.
	sorted := make([]TransportKey, len(keys))
	copy(sorted, keys)
	slices.SortStableFunc(sorted, func(a, b TransportKey) int {
		if comp := cmp.Compare(a.Y, b.Y); comp != 0 {
			return comp
		}
		return cmp.Compare(a.X, b.X)
	})

	for _, key := range sorted {
		if key.Scancode == 0 {
			continue // non-typeable keys and decals don't send HID events
		}

		addChar(m, key.Legends.Normal, key.Scancode, ModNone)
		addChar(m, key.Legends.Shift, key.Scancode, ModLShift)
		addChar(m, key.Legends.AltGr, key.Scancode, ModAltGr)
		addChar(m, key.Legends.ShiftAltGr, key.Scancode, ModShiftAltGr)
	}

	addPasteableControlChars(keys, m)

	return m
}

// addPasteableControlChars maps the small set of control characters that
// commonly appear in pasted text — newline (\n), carriage return (\r),
// CRLF (\r\n), and tab (\t) — to the corresponding HID scancode. Without
// these the paste path would silently drop them: PasteModal looks up each
// segmented input character verbatim in charMap, but addChar/Intl.Segmenter
// never produce entries for runes < U+0020.
//
// Layouts that lack an Enter or Tab key (decals only, hypothetically) get
// no entry — paste of those control chars will continue to be skipped.
func addPasteableControlChars(keys []TransportKey, m map[string]HIDCombo) {
	hasScancode := func(target uint8) bool {
		for _, k := range keys {
			if k.Scancode == target {
				return true
			}
		}
		return false
	}
	if hasScancode(hidEnter) {
		enter := HIDCombo{Scancode: hidEnter, Modifiers: ModNone}
		// Intl.Segmenter on the frontend treats CRLF as a single grapheme
		// cluster, so map all three forms.
		m["\n"] = enter
		m["\r"] = enter
		m["\r\n"] = enter
	}
	if hasScancode(hidTab) {
		m["\t"] = HIDCombo{Scancode: hidTab, Modifiers: ModNone}
	}
}

func addChar(m map[string]HIDCombo, legend *string, scancode, mods uint8) {
	if legend == nil || *legend == "" {
		return
	}
	if !ScancodeProducesText(scancode) {
		return
	}

	// Space's legend is normalized to "Space" by normalizeControlLegendsForDisplay
	// for the UI, and user-uploaded layouts may use "␠", " ", or "Space". The
	// pasted character that should map to this scancode is always U+0020.
	char := *legend
	if scancode == hidSpace {
		char = " "
	}

	if utf8.RuneCountInString(char) != 1 {
		// Only single Unicode codepoints; skip named keys like "Enter"
		return
	}
	r, _ := utf8.DecodeRuneInString(char)
	if r < 0x20 {
		// skip control characters
		return
	}
	if _, exists := m[char]; !exists {
		m[char] = HIDCombo{Scancode: scancode, Modifiers: mods}
	}
}

// normalizeControlLegendsForDisplay rewrites alternate forms of special-key
// legends to their canonical display form, using the taxonomy in
// keyaliases.json. For example, "Backspace" / "⟵" / "␈" / "BS" all become "⌫".
//
// This is intentionally limited to non-text keys plus Space so printable
// legends and typing behavior remain unchanged.
func normalizeControlLegendsForDisplay(keys []TransportKey) {
	normalize := func(legend **string) {
		if *legend == nil {
			return
		}
		canonical, ok := controlLegendDisplayMap[**legend]
		if !ok || canonical == **legend {
			return
		}
		v := canonical
		*legend = &v
	}

	for i := range keys {
		k := &keys[i]
		if k.Scancode == 0 {
			continue
		}
		if ScancodeProducesText(k.Scancode) && k.Scancode != hidSpace {
			continue
		}
		normalize(&k.Legends.Normal)
		normalize(&k.Legends.Shift)
		normalize(&k.Legends.AltGr)
		normalize(&k.Legends.ShiftAltGr)
		normalize(&k.Legends.Kana)
		normalize(&k.Legends.ShiftKana)
	}
}

// addDeadKeyCompositions enriches the charMap with composed characters
// produced by dead key + base key sequences.
//
// Only keys whose legend character appears in declaredDeadKeys are treated as
// dead keys. This prevents layouts without dead keys (e.g. en-US) from
// incorrectly generating compositions — on those layouts, characters like ^
// and ~ produce output directly and do not enter a dead key state.
//
// For each dead key, the function finds all base characters in the charMap and
// checks whether combining the dead key's Unicode combining character with the
// base produces a composed form (via NFC normalization). If so, the composed
// character is added to charMap with a Prefix pointing to the dead key's HIDCombo.
//
// Standalone dead key characters (e.g. typing just `^`) are also updated to
// require a Space follow-up, matching how real keyboards emit dead key characters.
func addDeadKeyCompositions(keys []TransportKey, charMap map[string]HIDCombo, declaredDeadKeys map[rune]bool) {
	if len(declaredDeadKeys) == 0 {
		return
	}

	type deadKeyInfo struct {
		combo      HIDCombo // scancode + modifiers to press the dead key
		combining  rune     // Unicode combining character
		displayKey rune     // the legend character as it appears on the keycap
		layerIdx   int      // 0..3 — used to break ties at equal modifier complexity
	}

	// Collect dead key legends that are both declared AND have a known
	// combining character mapping. When the same dead-key character appears on
	// multiple layers (e.g. ´ on both AltGr and ShiftAltGr in fr_BE) or on
	// multiple physical keys, we keep only the entry with the simplest
	// modifier — otherwise the composition loop and the standalone replacement
	// below could end up using different combos for the same character,
	// producing inconsistent dead-key-then-base sequences.
	chosen := map[rune]deadKeyInfo{}
	for _, key := range keys {
		if key.Scancode == 0 {
			continue
		}
		layers := []struct {
			legend *string
			mods   uint8
		}{
			{key.Legends.Normal, ModNone},
			{key.Legends.Shift, ModLShift},
			{key.Legends.AltGr, ModAltGr},
			{key.Legends.ShiftAltGr, ModShiftAltGr},
		}
		for layerIdx, layer := range layers {
			if layer.legend == nil {
				continue
			}
			r, _ := utf8.DecodeRuneInString(*layer.legend)
			if !declaredDeadKeys[r] {
				continue
			}
			combining, ok := deadKeyToCombining[r]
			if !ok {
				continue
			}
			candidate := deadKeyInfo{
				combo:      HIDCombo{Scancode: key.Scancode, Modifiers: layer.mods},
				combining:  combining,
				displayKey: r,
				layerIdx:   layerIdx,
			}
			existing, seen := chosen[r]
			// Prefer fewer modifier bits; tie-break with the layer enum order
			// (Normal < Shift < AltGr < ShiftAltGr) so the result is deterministic.
			if !seen ||
				bits.OnesCount8(candidate.combo.Modifiers) < bits.OnesCount8(existing.combo.Modifiers) ||
				(bits.OnesCount8(candidate.combo.Modifiers) == bits.OnesCount8(existing.combo.Modifiers) &&
					candidate.layerIdx < existing.layerIdx) {
				chosen[r] = candidate
			}
		}
	}

	if len(chosen) == 0 {
		return
	}

	// Materialise in displayKey order so charMap construction is deterministic
	// even when callers iterate it (or compare two parses for equality).
	deadKeys := make([]deadKeyInfo, 0, len(chosen))
	for _, dk := range chosen {
		deadKeys = append(deadKeys, dk)
	}
	slices.SortFunc(deadKeys, func(a, b deadKeyInfo) int {
		return cmp.Compare(a.displayKey, b.displayKey)
	})

	// Snapshot base chars to iterate (we'll be adding to charMap)
	type baseEntry struct {
		char  string
		r     rune
		combo HIDCombo
	}
	var bases []baseEntry
	for ch, combo := range charMap {
		r, _ := utf8.DecodeRuneInString(ch)
		if r >= 0x20 && combo.Prefix == nil {
			bases = append(bases, baseEntry{ch, r, combo})
		}
	}

	for _, dk := range deadKeys {
		prefix := dk.combo // copy for pointer stability

		for _, base := range bases {
			// Try NFC composition: base + combining → composed
			candidate := string([]rune{base.r, dk.combining})
			composed := norm.NFC.String(candidate)

			// If NFC produced a different single character, it's a valid composition
			if composed == candidate {
				continue
			}
			composedRune, _ := utf8.DecodeRuneInString(composed)
			if composedRune == utf8.RuneError || utf8.RuneCountInString(composed) != 1 {
				continue
			}

			composedStr := composed
			if _, exists := charMap[composedStr]; !exists {
				charMap[composedStr] = HIDCombo{
					Scancode:  base.combo.Scancode,
					Modifiers: base.combo.Modifiers,
					Prefix:    &prefix,
				}
			}
		}

		// Standalone dead key: dead key + Space → the dead key character itself.
		// Use the display rune captured from the actual key legend, not a
		// reverse lookup from deadKeyToCombining (which has duplicate values
		// and non-deterministic map iteration order).
		//
		// The Prefix==nil guard is defensive: deadKeys is already deduped to
		// one entry per displayKey above, so we shouldn't see this entry's
		// char already prefixed by an earlier loop iteration. The check still
		// prevents accidental rewrites if a future caller pre-populates
		// charMap with prefixed entries before invoking this function.
		deadChar := string(dk.displayKey)
		if existing, exists := charMap[deadChar]; exists && existing.Prefix == nil {
			charMap[deadChar] = HIDCombo{
				Scancode:  hidSpace,
				Modifiers: ModNone,
				Prefix:    &prefix,
			}
		}
	}
}

func validateLayout(layout *KeyboardLayout) error {
	if len(layout.Keys) < 10 {
		return fmt.Errorf("layout only has %d keys — does not look like a full keyboard", len(layout.Keys))
	}
	if layout.BoardW < 5 || layout.BoardW > 30 {
		return fmt.Errorf("unusual board width %.2f units (expected 5–30)", layout.BoardW)
	}
	if layout.BoardH < 2 || layout.BoardH > 10 {
		return fmt.Errorf("unusual board height %.2f units (expected 2–10)", layout.BoardH)
	}
	recognised, total := 0, 0
	for _, k := range layout.Keys {
		if k.Decal {
			continue // decorative labels don't have scancodes by design
		}
		total++
		if k.Scancode != 0 {
			recognised++
		}
	}
	if total == 0 {
		return fmt.Errorf("layout has no non-decal keys")
	}
	pct := float64(recognised) / float64(total) * 100
	if pct < 50 {
		return fmt.Errorf("only %.0f%% of keys mapped to HID scancodes — layout may be non-standard", pct)
	}
	return nil
}

// collectLayoutWarnings returns non-fatal issues found in a parsed layout.
// These don't prevent the layout from being used but may indicate problems
// the user should know about (e.g. unmapped keys, missing charMap coverage).
func collectLayoutWarnings(layout *KeyboardLayout) []string {
	var warnings []string

	// Check scancode coverage
	unmapped := 0
	total := 0
	for _, k := range layout.Keys {
		if k.Decal {
			continue
		}
		total++
		if k.Scancode == 0 {
			unmapped++
		}
	}
	if unmapped > 0 {
		pct := float64(total-unmapped) / float64(total) * 100
		warnings = append(warnings,
			fmt.Sprintf("%d of %d keys have no HID scancode (%.0f%% coverage). "+
				"These keys will not send input. Consider adding scancode overrides "+
				"in the KLE metadata.", unmapped, total, pct))
	}

	// Check charMap coverage — are common characters reachable?
	if len(layout.CharMap) == 0 {
		warnings = append(warnings, "No characters mapped — paste text will not work with this layout.")
	} else if len(layout.CharMap) < 30 {
		warnings = append(warnings,
			fmt.Sprintf("Only %d characters mapped (expected 50+). "+
				"Paste text may not work for some characters.", len(layout.CharMap)))
	}

	// Check for keys with legends but no scancode (likely position inference failures)
	legendless := 0
	for _, k := range layout.Keys {
		if k.Decal || k.Scancode != 0 {
			continue
		}
		if k.Legends.Normal != nil || k.Legends.Shift != nil {
			legendless++
		}
	}
	if legendless > 5 {
		warnings = append(warnings,
			fmt.Sprintf("%d keys have legends but no scancode — the layout's form factor "+
				"may not be fully supported. 75%% and larger keyboards work best.", legendless))
	}

	return warnings
}
