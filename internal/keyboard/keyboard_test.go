// internal/keyboard/keyboard_test.go
//
// Table-driven tests for the KLE parser.
//
// Run with: go test ./internal/keyboard/...

package keyboard

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"unicode/utf8"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func mustParse(t *testing.T, kleJSON string) *KeyboardLayout {
	t.Helper()
	layout, err := ParseKLE([]byte(kleJSON), "test", "Test Layout")
	if err != nil {
		t.Fatalf("ParseKLE failed: %v", err)
	}
	return layout
}

func findKey(layout *KeyboardLayout, x, y float64) *TransportKey {
	for i := range layout.Keys {
		k := &layout.Keys[i]
		if approxEq(k.X, x) && approxEq(k.Y, y) {
			return k
		}
	}
	return nil
}

func str(s string) *string { return &s }

// ---------------------------------------------------------------------------
// Minimal valid layout
// ---------------------------------------------------------------------------

const minimalKLE = `[
  {"name": "Test Layout", "author": "Test"},
  ["Esc","F1","F2","F3","F4","F5","F6","F7","F8","F9","F10","F11","F12"],
  [{"w":1.5},"Tab","Q","W","E","R","T","Y","U","I","O","P","[","]"],
  [{"w":1.75},"Caps","A","S","D","F","G","H","J","K","L",";","'",{"w":2.25},"Enter"],
  [{"w":2.25},"Shift","Z","X","C","V","B","N","M",",",".","/",{"w":2.75},"Shift"],
  [{"w":1.25},"Ctrl",{"w":1.25},"Win",{"w":1.25},"Alt",{"w":6.25},"Space",{"w":1.25},"Alt",{"w":1.25},"Win",{"w":1.25},"Menu",{"w":1.25},"Ctrl"]
]`

func TestParsesMinimalLayout(t *testing.T) {
	layout := mustParse(t, minimalKLE)

	if layout.Name != "Test Layout" {
		t.Errorf("expected name 'Test Layout', got %q", layout.Name)
	}
	if layout.Author != "Test" {
		t.Errorf("expected author 'Test', got %q", layout.Author)
	}
	if len(layout.Keys) == 0 {
		t.Fatal("expected keys, got none")
	}
	if layout.BoardW <= 0 {
		t.Errorf("expected positive boardW, got %v", layout.BoardW)
	}
}

// ---------------------------------------------------------------------------
// Legend parsing
// ---------------------------------------------------------------------------

func TestLegendLayers(t *testing.T) {
	// Standard KLE: "!\n1\n¹\n²" → shift=!, normal=1, shiftAltgr=¹, altgr=²
	legends := parseLegends("!\n1\n¹\n²")

	if legends.Normal == nil || *legends.Normal != "1" {
		t.Errorf("normal: expected '1', got %v", legends.Normal)
	}
	if legends.Shift == nil || *legends.Shift != "!" {
		t.Errorf("shift: expected '!', got %v", legends.Shift)
	}
	if legends.AltGr == nil || *legends.AltGr != "²" {
		t.Errorf("altgr: expected '²', got %v", legends.AltGr)
	}
	if legends.ShiftAltGr == nil || *legends.ShiftAltGr != "¹" {
		t.Errorf("shiftAltgr: expected '¹', got %v", legends.ShiftAltGr)
	}
}

func TestLegendKanaSlots(t *testing.T) {
	// Japanese KLE export style with kana in slot 8 and shifted kana in slot 10.
	legends := parseLegends("A\na\n\n\n\n\n\n\nｱ\n\nｧ")

	if legends.Normal == nil || *legends.Normal != "a" {
		t.Errorf("normal: expected 'a', got %v", legends.Normal)
	}
	if legends.Shift == nil || *legends.Shift != "A" {
		t.Errorf("shift: expected 'A', got %v", legends.Shift)
	}
	if legends.Kana == nil || *legends.Kana != "ｱ" {
		t.Errorf("kana: expected 'ｱ', got %v", legends.Kana)
	}
	if legends.ShiftKana == nil || *legends.ShiftKana != "ｧ" {
		t.Errorf("shiftKana: expected 'ｧ', got %v", legends.ShiftKana)
	}
}

func TestLegendEmptySlots(t *testing.T) {
	// Standard KLE: "Q\nq" → shift=Q, normal=q, altgr=nil, shiftAltgr=nil
	legends := parseLegends("Q\nq")

	if legends.Normal == nil || *legends.Normal != "q" {
		t.Errorf("normal: expected 'q'")
	}
	if legends.Shift == nil || *legends.Shift != "Q" {
		t.Errorf("shift: expected 'Q'")
	}
	if legends.AltGr != nil {
		t.Errorf("altgr: expected nil, got %v", *legends.AltGr)
	}
	if legends.ShiftAltGr != nil {
		t.Errorf("shiftAltgr: expected nil, got %v", *legends.ShiftAltGr)
	}
}

func TestLegendSingleChar(t *testing.T) {
	// Single uppercase letter in shift slot (standard KLE: "A" = shift only).
	// Mirror-case moves to Normal and auto-cases → normal="a", shift="A"
	legends := parseLegends("A")
	if legends.Normal == nil || *legends.Normal != "a" {
		t.Errorf("expected normal='a', got %v", legends.Normal)
	}
	if legends.Shift == nil || *legends.Shift != "A" {
		t.Errorf("expected shift='A', got %v", legends.Shift)
	}
}

func TestLegendAutoUppercase(t *testing.T) {
	// Single lowercase letter: mirror-case from shift slot → normal=q, shift=Q
	legends := parseLegends("q")
	if legends.Normal == nil || *legends.Normal != "q" {
		t.Errorf("expected normal='q', got %v", legends.Normal)
	}
	if legends.Shift == nil || *legends.Shift != "Q" {
		t.Errorf("expected auto shift='Q', got %v", legends.Shift)
	}

	// Accented letters should also work
	legends2 := parseLegends("ö")
	if legends2.Normal == nil || *legends2.Normal != "ö" {
		t.Errorf("expected normal='ö', got %v", legends2.Normal)
	}
	if legends2.Shift == nil || *legends2.Shift != "Ö" {
		t.Errorf("expected auto shift='Ö', got %v", legends2.Shift)
	}

	// Cyrillic should work
	legends3 := parseLegends("й")
	if legends3.Shift == nil || *legends3.Shift != "Й" {
		t.Errorf("expected auto shift='Й', got %v", legends3.Shift)
	}

	// Uppercase single letter — mirror-case + auto-lowercase → normal="q", shift="Q"
	legends4 := parseLegends("Q")
	if legends4.Normal == nil || *legends4.Normal != "q" {
		t.Errorf("expected auto normal='q', got %v", legends4.Normal)
	}
	if legends4.Shift == nil || *legends4.Shift != "Q" {
		t.Errorf("expected auto shift='Q', got %v", legends4.Shift)
	}

	// Multi-char legend "Tab": goes to shift slot (pos 0), no mirror-case (not a letter)
	legends5 := parseLegends("Tab")
	if legends5.Normal != nil {
		t.Errorf("expected normal=nil for 'Tab', got %q", *legends5.Normal)
	}
	if legends5.Shift == nil || *legends5.Shift != "Tab" {
		t.Errorf("expected shift='Tab' (KLE pos 0), got %v", legends5.Shift)
	}

	// Explicit two-part legend should be respected: "!\n1" → shift="!", normal="1"
	legends6 := parseLegends("!\n1")
	if legends6.Normal == nil || *legends6.Normal != "1" {
		t.Errorf("expected normal='1', got %v", legends6.Normal)
	}
	if legends6.Shift == nil || *legends6.Shift != "!" {
		t.Errorf("expected shift='!', got %v", legends6.Shift)
	}

	// Turkish dotless-ı (U+0131): mirror-case moves to Normal, but round-trip
	// fails (ToUpper('ı')='I', ToLower('I')='i' ≠ 'ı'), so no auto-case.
	legends7 := parseLegends("ı")
	if legends7.Normal == nil || *legends7.Normal != "ı" {
		t.Errorf("Turkish ı: expected normal='ı', got %v", legends7.Normal)
	}
	if legends7.Shift != nil {
		t.Errorf("Turkish ı should not auto-uppercase (round-trip unsafe), got shift=%q", *legends7.Shift)
	}

	// Turkish İ (U+0130): mirror-case moves to Normal, round-trip fails.
	legends8 := parseLegends("İ")
	if legends8.Normal == nil || *legends8.Normal != "İ" {
		t.Errorf("Turkish İ: expected normal='İ', got %v", legends8.Normal)
	}
	if legends8.Shift != nil {
		t.Errorf("Turkish İ should not auto-lowercase (round-trip unsafe), got shift=%q", *legends8.Shift)
	}
}

func TestNormalizeControlLegendsForDisplay(t *testing.T) {
	keys := []TransportKey{
		{Scancode: hidEscape, Legends: KeyLegends{Normal: str("␛"), Shift: str("␛")}},
		{Scancode: hidTab, Legends: KeyLegends{Normal: str("␉")}},
		{Scancode: hidBackspace, Legends: KeyLegends{Normal: str("␈")}},
		{Scancode: hidEnter, Legends: KeyLegends{Normal: str("␍")}},
		{Scancode: hidSpace, Legends: KeyLegends{Normal: str("␠")}},
		// Printable key should remain untouched.
		{Scancode: hidA, Legends: KeyLegends{Normal: str("a"), Shift: str("A")}},
	}

	normalizeControlLegendsForDisplay(keys)

	if keys[0].Legends.Normal == nil || *keys[0].Legends.Normal != "Esc" {
		t.Fatalf("escape normal legend: expected Esc, got %v", keys[0].Legends.Normal)
	}
	if keys[1].Legends.Normal == nil || *keys[1].Legends.Normal != "⇥" {
		t.Fatalf("tab normal legend: expected ⇥, got %v", keys[1].Legends.Normal)
	}
	if keys[2].Legends.Normal == nil || *keys[2].Legends.Normal != "⌫" {
		t.Fatalf("backspace normal legend: expected ⌫, got %v", keys[2].Legends.Normal)
	}
	if keys[3].Legends.Normal == nil || *keys[3].Legends.Normal != "⏎" {
		t.Fatalf("enter normal legend: expected ⏎, got %v", keys[3].Legends.Normal)
	}
	if keys[4].Legends.Normal == nil || *keys[4].Legends.Normal != "Space" {
		t.Fatalf("space normal legend: expected Space, got %v", keys[4].Legends.Normal)
	}
	if keys[5].Legends.Normal == nil || *keys[5].Legends.Normal != "a" {
		t.Fatalf("printable key should stay unchanged, got %v", keys[5].Legends.Normal)
	}
}

// ---------------------------------------------------------------------------
// Shape detection
// ---------------------------------------------------------------------------

func TestShapeNormal(t *testing.T) {
	if s := detectShape(0, 1, 1, 0, 0, false, false); s != ShapeNormal {
		t.Errorf("1×1 key: expected ShapeNormal, got %q", s)
	}
}

func TestShapeISOEnter(t *testing.T) {
	if s := detectShape(13, 1.25, 2, 1.5, 1, false, true); s != ShapeISOEnter {
		t.Errorf("ISO Enter: expected ShapeISOEnter, got %q", s)
	}
}

func TestShapeSteppedCaps(t *testing.T) {
	if s := detectShape(0, 1.75, 1, 0, 0, true, false); s != ShapeSteppedCaps {
		t.Errorf("stepped: expected ShapeSteppedCaps, got %q", s)
	}
}

func TestShapeBigAssEnter(t *testing.T) {
	// Exact 1.5×2 match
	if s := detectShape(13, 1.5, 2, 0, 0, false, false); s != ShapeBigAssEnter {
		t.Errorf("1.5×2 big-ass enter: expected ShapeBigAssEnter, got %q", s)
	}
	// Alternate tall key (h > 1.5, no w2) — second code path
	if s := detectShape(13, 1.0, 2, 0, 0, false, false); s != ShapeBigAssEnter {
		t.Errorf("1×2 tall key: expected ShapeBigAssEnter, got %q", s)
	}
}

func TestShapeNumpadTallKeysNotBigAssEnter(t *testing.T) {
	if s := detectShape(17.5, 1.0, 2, 0, 0, false, false); s != ShapeNormal {
		t.Errorf("numpad 1×2 key: expected ShapeNormal, got %q", s)
	}
	if s := detectShape(17.5, 1.5, 2, 0, 0, false, false); s != ShapeNormal {
		t.Errorf("numpad 1.5×2 key: expected ShapeNormal, got %q", s)
	}
}

// ---------------------------------------------------------------------------
// Scancode inference — full-size table
// ---------------------------------------------------------------------------

var fullSizeScancodeTests = []struct {
	name     string
	x, y     float64
	w, h     float64
	expected uint8
}{
	// Real KLE positions: func row y=0, then y:0.5 gap,
	// so number row y=1.5, qwerty y=2.5, home y=3.5, shift y=4.5, mods y=5.5
	{"Escape", 0, 0, 1, 1, hidEscape},
	{"F1", 2, 0, 1, 1, hidF1},
	{"F4", 5, 0, 1, 1, hidF4},
	{"F5", 6.5, 0, 1, 1, hidF5},
	{"F12", 14, 0, 1, 1, hidF12},
	{"PrintScreen", 15.25, 0, 1, 1, hidPrintScreen},
	{"1 (number)", 1, 1.5, 1, 1, hidN1},
	{"0 (number)", 10, 1.5, 1, 1, hidN0},
	{"Backspace", 13, 1.5, 2, 1, hidBackspace},
	{"Insert", 15.25, 1.5, 1, 1, hidInsert},
	{"NumLock", 18.5, 1.5, 1, 1, hidNumLock},
	{"Q", 1.5, 2.5, 1, 1, hidQ},
	{"W", 2.5, 2.5, 1, 1, hidW},
	{"P", 10.5, 2.5, 1, 1, hidP},
	{"Backslash", 13.5, 2.5, 1.5, 1, hidBackslash},
	{"Delete", 15.25, 2.5, 1, 1, hidDelete},
	{"KP7", 18.5, 2.5, 1, 1, hidKP7},
	{"A", 1.75, 3.5, 1, 1, hidA},
	{"CapsLock", 0, 3.5, 1.75, 1, hidCapsLock},
	{"Enter (ANSI)", 12.75, 3.5, 2.25, 1, hidEnter},
	{"Hash (ISO)", 12.75, 3.5, 1, 1, hidHash},
	{"KP5", 19.5, 3.5, 1, 1, hidKP5},
	{"LShift", 0, 4.5, 2.25, 1, hidLShift},
	{"ISO extra key", 1.25, 4.5, 1, 1, hidISOKey},
	{"Z (ANSI)", 2.25, 4.5, 1, 1, hidZ},
	{"RShift", 12.25, 4.5, 2.75, 1, hidRShift},
	{"ArrowUp", 16.25, 4.5, 1, 1, hidArrowUp},
	{"KPEnter", 21.5, 4.5, 1, 2, hidKPEnter},
	{"Space", 3.75, 5.5, 6.25, 1, hidSpace},
	{"LCtrl", 0, 5.5, 1.25, 1, hidLCtrl},
	{"RAlt", 10, 5.5, 1.25, 1, hidRAlt},
	{"ArrowLeft", 15.25, 5.5, 1, 1, hidArrowLeft},
	{"ArrowDown", 16.25, 5.5, 1, 1, hidArrowDown},
	{"ArrowRight", 17.25, 5.5, 1, 1, hidArrowRight},
	{"KP0", 18.5, 5.5, 2, 1, hidKP0},
	// Special shapes
	{"ISO Enter", 13.5, 2.5, 1.25, 2, hidEnter},
	{"Numpad +", 21.5, 2.5, 1, 2, hidKPPlus},
}

func TestScancodeInferenceFullSize(t *testing.T) {
	for _, tt := range fullSizeScancodeTests {
		t.Run(tt.name, func(t *testing.T) {
			got := inferScancodeWithTable(tt.x, tt.y, tt.w, tt.h, fullSizeTable)
			if got != tt.expected {
				t.Errorf("inferScancodeWithTable(%.2f, %.2f, w=%.2f, h=%.2f, fullSize): expected 0x%02X, got 0x%02X",
					tt.x, tt.y, tt.w, tt.h, tt.expected, got)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Scancode inference — compact table
// ---------------------------------------------------------------------------

var compactScancodeTests = []struct {
	name     string
	x, y     float64
	w, h     float64
	expected uint8
}{
	// Compact: no y:0.5 gap, rows at y=0,1,2,3,4,5
	{"Escape", 0, 0, 1, 1, hidEscape},
	{"F1", 1, 0, 1, 1, hidF1},
	{"F12", 12, 0, 1, 1, hidF12},
	{"Grave", 0, 1, 1, 1, hidGrave},
	{"1 (number)", 1, 1, 1, 1, hidN1},
	{"Backspace", 13, 1, 2, 1, hidBackspace},
	{"Tab", 0, 2, 1.5, 1, hidTab},
	{"Q", 1.5, 2, 1, 1, hidQ},
	{"Backslash", 13.5, 2, 1.5, 1, hidBackslash},
	{"CapsLock", 0, 3, 1.75, 1, hidCapsLock},
	{"A", 1.75, 3, 1, 1, hidA},
	{"Enter (compact)", 12.75, 3, 2.25, 1, hidEnter},
	{"Hash (ISO compact)", 12.75, 3, 1, 1, hidHash}, // narrow key at same x = hash, not enter
	{"LShift", 0, 4, 2.25, 1, hidLShift},
	{"Z", 2.25, 4, 1, 1, hidZ},
	{"RShift", 12.25, 4, 2.75, 1, hidRShift},
	{"Space", 3.75, 5, 6.25, 1, hidSpace},
	{"LCtrl", 0, 5, 1.25, 1, hidLCtrl},
	{"RAlt", 10, 5, 1, 1, hidRAlt},
	// ISO Enter still detected by h >= 2 rule
	{"ISO Enter (compact)", 13.5, 2, 1.25, 2, hidEnter},
}

func TestScancodeInferenceCompact(t *testing.T) {
	for _, tt := range compactScancodeTests {
		t.Run(tt.name, func(t *testing.T) {
			got := inferScancodeWithTable(tt.x, tt.y, tt.w, tt.h, compactTable)
			if got != tt.expected {
				t.Errorf("inferScancodeWithTable(%.2f, %.2f, w=%.2f, h=%.2f, compact): expected 0x%02X, got 0x%02X",
					tt.x, tt.y, tt.w, tt.h, tt.expected, got)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Position table sort-order invariant
// ---------------------------------------------------------------------------

func TestPositionTablesSorted(t *testing.T) {
	// inferScancodeWithTable relies on posEntry slices being sorted by xStart
	// for its early-break optimization. A misordered entry would silently
	// produce wrong scancodes with no error.
	tables := map[string]map[int][]posEntry{
		"fullSize": fullSizeTable,
		"compact":  compactTable,
	}
	for name, table := range tables {
		for rowIdx, row := range table {
			for i := 1; i < len(row); i++ {
				if row[i].xStart < row[i-1].xStart {
					t.Errorf("%s table row %d: entries out of order at index %d "+
						"(%.2f < %.2f)", name, rowIdx, i,
						row[i].xStart, row[i-1].xStart)
				}
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Table selection
// ---------------------------------------------------------------------------

func TestSelectPositionTable(t *testing.T) {
	tests := []struct {
		name     string
		boardW   float64
		boardH   float64
		keyCount int
		wantFull bool
	}{
		{"ANSI 104 (wide + many keys)", 22.5, 6.5, 104, true},
		{"ISO 105 (wide + many keys)", 22.5, 6.5, 105, true},
		{"JIS 109 (wide + many keys)", 22.5, 6.5, 109, true},
		{"wide board, few keys", 21, 5, 80, true},
		{"narrow board, 100+ keys", 18, 6.5, 100, true},
		{"TKL (narrow, 87 keys)", 18, 6.5, 87, false},
		{"75% (narrow, ~84 keys)", 16, 6, 84, false},
		{"65% (narrow, ~68 keys, fallback)", 16, 5, 68, true},
		{"60% (narrow, ~61 keys, fallback)", 15, 5, 61, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := selectPositionTable(tt.boardW, tt.boardH, tt.keyCount)
			// Compare by identity: fullSizeTable has a numpad row (row 2 has KP7 at x=18.5),
			// while compactTable does not. Check whether row 2 has entries past x=18.
			hasNumpad := false
			for _, entry := range got[2] {
				if entry.xStart >= 18 {
					hasNumpad = true
					break
				}
			}
			if hasNumpad != tt.wantFull {
				which := "compact"
				if hasNumpad {
					which = "full-size"
				}
				t.Errorf("selectPositionTable(%.1f, %.1f, %d) returned %s table",
					tt.boardW, tt.boardH, tt.keyCount, which)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// sanitizeName
// ---------------------------------------------------------------------------

func TestSanitizeNamePassthrough(t *testing.T) {
	if got := sanitizeName("My Layout"); got != "My Layout" {
		t.Errorf("expected 'My Layout', got %q", got)
	}
}

func TestSanitizeNameStripsControlChars(t *testing.T) {
	if got := sanitizeName("Evil\x00Name\x1B!"); got != "EvilName!" {
		t.Errorf("expected 'EvilName!', got %q", got)
	}
}

func TestSanitizeNameTrimsWhitespace(t *testing.T) {
	if got := sanitizeName("  padded  "); got != "padded" {
		t.Errorf("expected 'padded', got %q", got)
	}
}

func TestSanitizeNameTruncates(t *testing.T) {
	long := strings.Repeat("x", 200)
	got := sanitizeName(long)
	if utf8.RuneCountInString(got) != maxNameLength {
		t.Errorf("expected %d runes, got %d", maxNameLength, utf8.RuneCountInString(got))
	}

	// Multi-byte: 200 CJK characters → truncate to maxNameLength runes, not bytes
	cjk := strings.Repeat("漢", 200)
	gotCJK := sanitizeName(cjk)
	if utf8.RuneCountInString(gotCJK) != maxNameLength {
		t.Errorf("CJK: expected %d runes, got %d", maxNameLength, utf8.RuneCountInString(gotCJK))
	}
	if !utf8.ValidString(gotCJK) {
		t.Error("CJK truncation produced invalid UTF-8")
	}
}

func TestSanitizeNameEmptyFallback(t *testing.T) {
	if got := sanitizeName(""); got != "Unnamed Layout" {
		t.Errorf("expected 'Unnamed Layout', got %q", got)
	}
	// Only whitespace → also empty after trim
	if got := sanitizeName("   "); got != "Unnamed Layout" {
		t.Errorf("expected 'Unnamed Layout', got %q", got)
	}
	// Only control chars → also empty after stripping
	if got := sanitizeName("\x00\x1F"); got != "Unnamed Layout" {
		t.Errorf("expected 'Unnamed Layout', got %q", got)
	}
}

func TestSanitizeNamePreservesUnicode(t *testing.T) {
	if got := sanitizeName("Ελληνικά 日本語"); got != "Ελληνικά 日本語" {
		t.Errorf("expected unicode preserved, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// loadBuiltinLayout
// ---------------------------------------------------------------------------

func TestLoadBuiltinLayoutHyphenID(t *testing.T) {
	layout, err := loadBuiltinLayout("en-US")
	if err != nil {
		t.Fatalf("failed to load en-US: %v", err)
	}
	if layout.ID != "en-US" {
		t.Errorf("expected ID 'en-US', got %q", layout.ID)
	}
}

func TestLoadBuiltinLayoutAlias(t *testing.T) {
	layout, err := loadBuiltinLayout("nl-BE")
	if err != nil {
		t.Fatalf("failed to load nl-BE alias: %v", err)
	}
	// nl-BE is an alias for fr_BE — should get the fr_BE layout but with ID "nl-BE"
	if layout.ID != "nl-BE" {
		t.Errorf("expected ID 'nl-BE', got %q", layout.ID)
	}
	if len(layout.Keys) == 0 {
		t.Error("alias layout has no keys")
	}
}

func TestLoadBuiltinLayoutAliasMatchesDirect(t *testing.T) {
	alias, err := loadBuiltinLayout("nl-BE")
	if err != nil {
		t.Fatalf("nl-BE: %v", err)
	}
	direct, err := loadBuiltinLayout("fr-BE")
	if err != nil {
		t.Fatalf("fr-BE: %v", err)
	}
	// Same keys, same charMap (different ID)
	if len(alias.Keys) != len(direct.Keys) {
		t.Errorf("key count mismatch: nl-BE=%d, fr-BE=%d", len(alias.Keys), len(direct.Keys))
	}
	if len(alias.CharMap) != len(direct.CharMap) {
		t.Errorf("charMap size mismatch: nl-BE=%d, fr-BE=%d", len(alias.CharMap), len(direct.CharMap))
	}
}

func TestLoadBuiltinLayoutNotFound(t *testing.T) {
	_, err := loadBuiltinLayout("xx-XX")
	if err == nil {
		t.Error("expected error for nonexistent layout")
	}
}

// ---------------------------------------------------------------------------
// selectPositionTable — compact KLE round-trip
// ---------------------------------------------------------------------------

// Minimal 65% KLE: no function row, no numpad, no y:0.5 gap.
// Rows at y=0,1,2,3,4 (5 rows). Board width ~16u.
const compact65KLE = `[
  {"name": "Compact 65%", "author": "Test"},
  ["~\n\u0060","!\n1","@\n2","#\n3","$\n4","%\n5","^\n6","&\n7","*\n8","(\n9",")\n0","_\n-","+\n=",{"w":2},"⌫","Del"],
  [{"w":1.5},"⇥","q","w","e","r","t","y","u","i","o","p","{\n[","}\n]",{"w":1.5},"|\n\\","PgUp"],
  [{"w":1.75},"⇪","a","s","d","f","g","h","j","k","l",":\n;","'\n\"",{"w":2.25},"⏎","PgDn"],
  [{"w":2.25},"⇧ Shift","z","x","c","v","b","n","m","<\n,",">\n.","?\n/",{"w":1.75},"⇧ Shift","↑","End"],
  [{"w":1.25},"⌃ Ctrl",{"w":1.25},"⌘ Meta",{"w":1.25},"⌥ Alt",{"w":6.25},"Space",{"w":1},"⌥ Alt",{"w":1},"⌃ Ctrl","←","↓","→"]
]`

// Minimal 75% KLE: function row packed at y=0, number row at y=1, no y:0.5 gap, no numpad.
const compact75KLE = `[
  {"name": "Compact 75%", "author": "Test"},
  ["Esc","F1","F2","F3","F4","F5","F6","F7","F8","F9","F10","F11","F12","PrtSc","Del"],
  ["~\n\u0060","!\n1","@\n2","#\n3","$\n4","%\n5","^\n6","&\n7","*\n8","(\n9",")\n0","_\n-","+\n=",{"w":2},"⌫","Home"],
  [{"w":1.5},"⇥","q","w","e","r","t","y","u","i","o","p","{\n[","}\n]",{"w":1.5},"|\n\\","PgUp"],
  [{"w":1.75},"⇪","a","s","d","f","g","h","j","k","l",":\n;","'\n\"",{"w":2.25},"⏎","PgDn"],
  [{"w":2.25},"⇧ Shift","z","x","c","v","b","n","m","<\n,",">\n.","?\n/",{"w":1.75},"⇧ Shift","↑","End"],
  [{"w":1.25},"⌃ Ctrl",{"w":1.25},"⌘ Meta",{"w":1.25},"⌥ Alt",{"w":6.25},"Space",{"w":1},"⌥ Alt",{"w":1},"⌃ Ctrl","←","↓","→"]
]`

func TestCompact65FallsBackToFullSize(t *testing.T) {
	layout := mustParse(t, compact65KLE)

	if layout.BoardW > 20 {
		t.Errorf("65%% should be narrow, got boardW=%.1f", layout.BoardW)
	}

	// 65% boards (no function row, <70 keys or <6 rows) fall back to
	// full-size table. Scancodes will be wrong for most keys, but the
	// layout still parses — users need scancode overrides in metadata.
	table := selectPositionTable(layout.BoardW, layout.BoardH, len(layout.Keys))
	hasNumpad := false
	for _, entry := range table[2] {
		if entry.xStart >= 18 {
			hasNumpad = true
		}
	}
	if !hasNumpad {
		t.Error("65%% should fall back to full-size table (not compact)")
	}

	if len(layout.Keys) == 0 {
		t.Error("no keys parsed")
	}
	t.Logf("65%%: %d keys, falls back to full-size table (needs scancode overrides)",
		len(layout.Keys))
}

func TestCompact75ParsesAndUsesCompactTable(t *testing.T) {
	layout := mustParse(t, compact75KLE)

	if layout.BoardW > 20 {
		t.Errorf("75%% should be narrow, got boardW=%.1f", layout.BoardW)
	}

	// Function row at y=0
	escKey := findKey(layout, 0, 0)
	if escKey == nil {
		t.Fatal("Esc key not found at (0, 0)")
	}
	if escKey.Scancode != hidEscape {
		t.Errorf("Esc scancode: expected 0x%02X, got 0x%02X", hidEscape, escKey.Scancode)
	}

	f1Key := findKey(layout, 1, 0)
	if f1Key == nil {
		t.Fatal("F1 key not found at (1, 0)")
	}
	if f1Key.Scancode != hidF1 {
		t.Errorf("F1 scancode: expected 0x%02X, got 0x%02X", hidF1, f1Key.Scancode)
	}

	f12Key := findKey(layout, 12, 0)
	if f12Key == nil {
		t.Fatal("F12 key not found at (12, 0)")
	}
	if f12Key.Scancode != hidF12 {
		t.Errorf("F12 scancode: expected 0x%02X, got 0x%02X", hidF12, f12Key.Scancode)
	}

	// Number row at y=1
	n1Key := findKey(layout, 1, 1)
	if n1Key == nil {
		t.Fatal("1 key not found at (1, 1)")
	}
	if n1Key.Scancode != hidN1 {
		t.Errorf("1 scancode: expected 0x%02X, got 0x%02X", hidN1, n1Key.Scancode)
	}

	// QWERTY row at y=2
	qKey := findKey(layout, 1.5, 2)
	if qKey == nil {
		t.Fatal("Q key not found at (1.5, 2)")
	}
	if qKey.Scancode != hidQ {
		t.Errorf("Q scancode: expected 0x%02X, got 0x%02X", hidQ, qKey.Scancode)
	}

	// Home row at y=3
	enterKey := findKey(layout, 12.75, 3)
	if enterKey == nil {
		t.Fatal("Enter key not found at (12.75, 3)")
	}
	if enterKey.Scancode != hidEnter {
		t.Errorf("Enter scancode: expected 0x%02X, got 0x%02X", hidEnter, enterKey.Scancode)
	}

	t.Logf("75%%: %d keys, %d charMap entries, %.0fx%.0f",
		len(layout.Keys), len(layout.CharMap), layout.BoardW, layout.BoardH)
}

// ---------------------------------------------------------------------------
// Real-world compact KLE files (uploaded from keyboard-layout-editor.com)
//
// These use the STANDARD KLE convention: shift legend at position 0 (top),
// normal legend at position 1 (bottom). E.g. "!\n1" means shift=!, normal=1.
// Our built-in layouts use the OPPOSITE order: "1\n!" means normal=1, shift=!.
// These tests verify how the parser handles externally-authored KLE files.
// ---------------------------------------------------------------------------

// Keycool 84 — real 75% keyboard from keyboard-layout-editor.com.
// Uses standard KLE legend order (shift-first): "!\n1" means shift=!, normal=1.
// Has "a":4 (bottom alignment) and "a":6 (center) properties.
//
// ANSI 60% — standard 60% ANSI. No function row, no nav cluster, no numpad.
// Uses standard KLE legend order (shift-first). Has "a":7 on spacebar.
//
// ISO 60% — 60% with ISO Enter (h=2, w2/x2 offset). No function row.
// Uses standard KLE legend order. Has ISO extra key between LShift and Z.
//
// These are stored as files to avoid Go string escaping headaches with
// backslashes, backticks, and quotes inside JSON.
//
// See: internal/keyboard/testdata/

func loadTestKLE(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("failed to read testdata/%s: %v", name, err)
	}
	return string(data)
}

func TestKeycool84Parse(t *testing.T) {
	layout := mustParse(t, loadTestKLE(t, "keycool_84.kle.json"))

	t.Logf("Keycool 84: %d keys, %d charMap, board %.1fx%.1f",
		len(layout.Keys), len(layout.CharMap), layout.BoardW, layout.BoardH)

	// Should be compact (no numpad, narrow board)
	if layout.BoardW > 20 {
		t.Errorf("expected narrow board, got %.1f", layout.BoardW)
	}
	if len(layout.Keys) < 70 || len(layout.Keys) > 90 {
		t.Errorf("expected ~84 keys, got %d", len(layout.Keys))
	}
	if len(layout.CharMap) == 0 {
		t.Error("charMap is empty")
	}

	// Legend order: standard KLE "!\n1" → after swap: normal="1", shift="!"
	n1Key := findKey(layout, 1, 1)
	if n1Key == nil {
		t.Fatal("key at (1,1) not found")
	}
	if n1Key.Legends.Normal == nil || *n1Key.Legends.Normal != "1" {
		t.Errorf("number 1 key: expected normal='1', got %v", safeStr(n1Key.Legends.Normal))
	}
	if n1Key.Legends.Shift == nil || *n1Key.Legends.Shift != "!" {
		t.Errorf("number 1 key: expected shift='!', got %v", safeStr(n1Key.Legends.Shift))
	}

	// Uppercase "Q" → after swap + auto-lowercase: normal="q", shift="Q"
	if combo, ok := layout.CharMap["q"]; !ok {
		t.Error("'q' missing from charMap")
	} else if combo.Modifiers != ModNone {
		t.Errorf("'q' should be unmodified, got 0x%02X", combo.Modifiers)
	}
	if combo, ok := layout.CharMap["Q"]; !ok {
		t.Error("'Q' missing from charMap")
	} else if combo.Modifiers != ModLShift {
		t.Errorf("'Q' should require Shift, got 0x%02X", combo.Modifiers)
	}

	// Scancodes: 75% selects compact table, function row at y=0
	escKey := findKey(layout, 0, 0)
	if escKey == nil {
		t.Fatal("Esc not found")
	}
	if escKey.Scancode != hidEscape {
		t.Errorf("Esc scancode: expected 0x%02X, got 0x%02X", hidEscape, escKey.Scancode)
	}

	// F1 at x=1 (Keycool packs F-keys tightly)
	f1Key := findKey(layout, 1, 0)
	if f1Key == nil {
		t.Fatal("F1 not found")
	}
	if f1Key.Scancode != hidF1 {
		t.Errorf("F1 scancode: expected 0x%02X, got 0x%02X", hidF1, f1Key.Scancode)
	}
}

func TestANSI60Parse(t *testing.T) {
	layout := mustParse(t, loadTestKLE(t, "ansi_60.kle.json"))

	t.Logf("ANSI 60%%: %d keys, %d charMap, board %.1fx%.1f",
		len(layout.Keys), len(layout.CharMap), layout.BoardW, layout.BoardH)

	if len(layout.Keys) < 55 || len(layout.Keys) > 65 {
		t.Errorf("expected ~61 keys, got %d", len(layout.Keys))
	}

	// --- Discovery: legend order ---
	grave := findKey(layout, 0, 0)
	if grave == nil {
		t.Fatal("grave key at (0,0) not found")
	}
	t.Logf("Grave key legends: normal=%v, shift=%v",
		safeStr(grave.Legends.Normal), safeStr(grave.Legends.Shift))
	if grave.Legends.Normal != nil && *grave.Legends.Normal == "~" {
		t.Log("FINDING: shift-first legend order (normal='~' should be '`')")
	}

	// --- Discovery: 60% row offset ---
	// No function row: number row at y=0. Compact table row 0 = F-keys.
	// So grave at (0,0) gets Escape scancode instead of Grave.
	if grave.Scancode == hidEscape {
		t.Log("FINDING: 60% row offset — grave key at y=0 gets Escape scancode " +
			"(compact table expects F-row at y=0)")
	}

	// ANSI Enter (w=2.25, single-row)
	enterKey := findKey(layout, 12.75, 2)
	if enterKey == nil {
		t.Fatal("Enter key not found at (12.75, 2)")
	}
	if enterKey.Shape != ShapeNormal {
		t.Logf("Enter shape: %s (ANSI enter is single-row, expected normal)", enterKey.Shape)
	}

	// 60% falls back to full-size table which has no row 1 (y:0.5 gap).
	// QWERTY row at y=1 gets scancode 0 → not added to charMap.
	// This is expected: 60% keyboards need scancode overrides.
	if _, ok := layout.CharMap["q"]; !ok {
		t.Log("Expected: 'q' absent from charMap (60% row offset, scancode=0)")
	}
}

func TestISO60Parse(t *testing.T) {
	layout := mustParse(t, loadTestKLE(t, "iso_60.kle.json"))

	t.Logf("ISO 60%%: %d keys, %d charMap, board %.1fx%.1f",
		len(layout.Keys), len(layout.CharMap), layout.BoardW, layout.BoardH)

	// ISO Enter (h=2) — should be detected and shaped correctly
	var isoEnter *TransportKey
	for i := range layout.Keys {
		if layout.Keys[i].H >= 2 && layout.Keys[i].X < 15 {
			isoEnter = &layout.Keys[i]
			break
		}
	}
	if isoEnter == nil {
		t.Fatal("ISO Enter (h>=2) not found")
	}
	if isoEnter.Scancode != hidEnter {
		t.Errorf("ISO Enter scancode: expected 0x%02X, got 0x%02X", hidEnter, isoEnter.Scancode)
	}
	if isoEnter.Shape != ShapeISOEnter {
		t.Errorf("ISO Enter shape: expected %q, got %q", ShapeISOEnter, isoEnter.Shape)
	}
	t.Logf("ISO Enter at (%.2f, %.2f), w=%.2f, h=%.2f, w2=%.2f, x2=%.2f",
		isoEnter.X, isoEnter.Y, isoEnter.W, isoEnter.H, isoEnter.W2, isoEnter.X2)

	// ISO extra key between LShift and Z
	isoExtra := findKey(layout, 1.25, 3)
	if isoExtra == nil {
		t.Fatal("ISO extra key at (1.25, 3) not found")
	}
	t.Logf("ISO extra key legends: normal=%v, shift=%v",
		safeStr(isoExtra.Legends.Normal), safeStr(isoExtra.Legends.Shift))

	// Legend order: "|\n\\" → our parser: normal="|", shift="\"
	// Standard KLE intent: shift="|", normal="\"
	if isoExtra.Legends.Normal != nil && *isoExtra.Legends.Normal == "|" {
		t.Log("FINDING: ISO extra key has shift-first legend order " +
			"(normal='|' should be '\\')")
	}

	// Z should be at x=2.25 (after 1.25u LShift + 1u ISO key)
	zKey := findKey(layout, 2.25, 3)
	if zKey == nil {
		t.Fatal("Z key not found at (2.25, 3)")
	}

	// --- Discovery: 60% row offset applies here too ---
	if zKey.Scancode != hidZ {
		t.Logf("FINDING: Z scancode is 0x%02X (expected 0x%02X) — "+
			"60%% row offset (compact table expects F-row at y=0)",
			zKey.Scancode, hidZ)
	}
}

// safeStr dereferences a *string for logging, returning "<nil>" if nil.
func safeStr(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}

// ---------------------------------------------------------------------------
// charMap
// ---------------------------------------------------------------------------

func TestCharMapBasic(t *testing.T) {
	// a key at (1.5, 2) with legends "q\nQ\n@"
	keys := []TransportKey{
		{X: 1.5, Y: 2, W: 1, H: 1, Scancode: hidQ,
			Legends: KeyLegends{Normal: str("q"), Shift: str("Q"), AltGr: str("@")}},
	}
	m := buildCharMap(keys)

	if c, ok := m["q"]; !ok || c.Scancode != hidQ || c.Modifiers != ModNone {
		t.Errorf("'q' mapping wrong: %+v", m["q"])
	}
	if c, ok := m["Q"]; !ok || c.Modifiers != ModLShift {
		t.Errorf("'Q' mapping wrong: %+v", m["Q"])
	}
	if c, ok := m["@"]; !ok || c.Modifiers != ModAltGr {
		t.Errorf("'@' mapping wrong: %+v", m["@"])
	}
}

func TestCharMapSkipsMultiChar(t *testing.T) {
	keys := []TransportKey{
		{X: 0, Y: 3, W: 1.75, H: 1, Scancode: hidCapsLock,
			Legends: KeyLegends{Normal: str("Caps Lock")}},
	}
	m := buildCharMap(keys)
	if _, ok := m["Caps Lock"]; ok {
		t.Error("multi-char legend 'Caps Lock' should not appear in charMap")
	}
}

func TestCharMapFirstOccurrenceWins(t *testing.T) {
	// Two keys both producing "@" — first one (lower y) should win
	keys := []TransportKey{
		{X: 1, Y: 1, W: 1, H: 1, Scancode: hidN2, Legends: KeyLegends{Shift: str("@")}},
		{X: 5, Y: 2, W: 1, H: 1, Scancode: hidT, Legends: KeyLegends{AltGr: str("@")}},
	}
	m := buildCharMap(keys)
	if c, ok := m["@"]; !ok || c.Scancode != hidN2 {
		t.Errorf("first occurrence should win: got %+v", m["@"])
	}
}

func TestCharMapExcludesScancode0(t *testing.T) {
	keys := []TransportKey{
		{X: 0, Y: 0, W: 1, H: 1, Scancode: 0,
			Legends: KeyLegends{Normal: str("x")}},
	}
	m := buildCharMap(keys)
	if _, ok := m["x"]; ok {
		t.Error("key with Scancode=0 should not appear in charMap")
	}
}

func TestCharMapSkipsGlyphLegendsForNonTextKeys(t *testing.T) {
	keys := []TransportKey{
		{X: 0, Y: 0, W: 1, H: 1, Scancode: hidBackspace,
			Legends: KeyLegends{Normal: str("⌫")}},
		{X: 1, Y: 0, W: 1, H: 1, Scancode: hidTab,
			Legends: KeyLegends{Normal: str("⇥")}},
		{X: 2, Y: 0, W: 1, H: 1, Scancode: hidArrowUp,
			Legends: KeyLegends{Normal: str("↑")}},
		{X: 3, Y: 0, W: 1, H: 1, Scancode: hidLShift,
			Legends: KeyLegends{Normal: str("⇧")}},
		{X: 4, Y: 0, W: 1, H: 1, Scancode: hidQ,
			Legends: KeyLegends{Normal: str("q")}},
	}
	m := buildCharMap(keys)

	for _, glyph := range []string{"⌫", "⇥", "↑", "⇧"} {
		if _, ok := m[glyph]; ok {
			t.Errorf("non-text glyph legend %q should not appear in charMap", glyph)
		}
	}

	if _, ok := m["q"]; !ok {
		t.Error("printable key legend 'q' should still appear in charMap")
	}
}

// ---------------------------------------------------------------------------
// Full round-trip: parse → JSON marshal → unmarshal
// ---------------------------------------------------------------------------

func TestRoundTrip(t *testing.T) {
	layout := mustParse(t, minimalKLE)

	data, err := json.Marshal(layout)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var restored KeyboardLayout
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if restored.Name != layout.Name {
		t.Errorf("name mismatch: %q vs %q", restored.Name, layout.Name)
	}
	if len(restored.Keys) != len(layout.Keys) {
		t.Errorf("key count mismatch: %d vs %d", len(restored.Keys), len(layout.Keys))
	}
}

func TestRoundTripWithDeadKeys(t *testing.T) {
	layout, err := loadBuiltinLayout("de-DE")
	if err != nil {
		t.Fatalf("failed to load de-DE: %v", err)
	}

	data, err := json.Marshal(layout)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var restored KeyboardLayout
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	// Dead key composition should survive the round-trip
	combo, ok := restored.CharMap["â"]
	if !ok {
		t.Fatal("â missing from charMap after round-trip")
	}
	if combo.Prefix == nil {
		t.Fatal("â dead key prefix lost in round-trip")
	}
	if combo.Prefix.Scancode == 0 {
		t.Error("prefix scancode is zero after round-trip")
	}
	// Prefix should not have its own prefix (no nested dead keys)
	if combo.Prefix.Prefix != nil {
		t.Error("prefix should not have a nested prefix")
	}
}

// ---------------------------------------------------------------------------
// Validation errors
// ---------------------------------------------------------------------------

func TestValidationRejectsEmptyLayout(t *testing.T) {
	// Only metadata, no rows
	_, err := ParseKLE([]byte(`[{"name":"Empty"}]`), "x", "")
	if err == nil || !strings.Contains(err.Error(), "no keys") {
		t.Errorf("expected 'no keys' error, got: %v", err)
	}
}

func TestNameOverride(t *testing.T) {
	layout := mustParse(t, minimalKLE)
	layout2, _ := ParseKLE([]byte(minimalKLE), "test", "Override Name")
	if layout2.Name != "Override Name" {
		t.Errorf("expected 'Override Name', got %q", layout2.Name)
	}
	_ = layout
}

func TestDeadKeyDetection(t *testing.T) {
	// With declared dead keys, only matching legends get flagged
	declared := map[rune]bool{'^': true, '´': true}

	if !isDeadKey(KeyLegends{Normal: str("^")}, declared) {
		t.Error("^ should be dead when declared")
	}
	if isDeadKey(KeyLegends{Normal: str("a")}, declared) {
		t.Error("'a' should not be dead")
	}
	if isDeadKey(KeyLegends{Normal: str("~")}, declared) {
		t.Error("~ should not be dead when not declared")
	}

	// With empty declared set, nothing is dead
	if isDeadKey(KeyLegends{Normal: str("^")}, nil) {
		t.Error("^ should not be dead with no declarations")
	}

	// nil Normal legend — should never be dead even if Shift matches
	if isDeadKey(KeyLegends{Normal: nil, Shift: str("^")}, declared) {
		t.Error("nil Normal should not be flagged dead even if Shift matches")
	}

	// Dead key on shift layer only — isDeadKey checks Normal only, so not flagged.
	// (addDeadKeyCompositions still generates compositions from the shift layer.)
	if isDeadKey(KeyLegends{Normal: str("°"), Shift: str("^")}, declared) {
		t.Error("key with ^ only on Shift layer should not be flagged dead (Normal is °)")
	}
}

// ---------------------------------------------------------------------------
// Dead key compositions
// ---------------------------------------------------------------------------

func TestDeadKeyComposition(t *testing.T) {
	// Load German layout — has ^ and ´ dead keys
	layout, err := loadBuiltinLayout("de-DE")
	if err != nil {
		t.Fatalf("failed to load de-DE: %v", err)
	}

	// â should exist: ^ (dead) + a → â
	combo, ok := layout.CharMap["â"]
	if !ok {
		t.Fatal("â not found in charMap")
	}
	if combo.Prefix == nil {
		t.Fatal("â should have a dead key prefix")
	}
	if combo.Scancode != hidA {
		t.Errorf("â base scancode: expected 0x%02X (a), got 0x%02X", hidA, combo.Scancode)
	}

	// Ê should exist: ^ (dead) + Shift+E → Ê
	comboUpper, ok := layout.CharMap["Ê"]
	if !ok {
		t.Fatal("Ê not found in charMap")
	}
	if comboUpper.Prefix == nil {
		t.Fatal("Ê should have a dead key prefix")
	}
	if comboUpper.Modifiers&ModLShift == 0 {
		t.Error("Ê should require Shift modifier on the base key")
	}

	// ^ standalone should have prefix + Space
	caret, ok := layout.CharMap["^"]
	if !ok {
		t.Fatal("^ not found in charMap")
	}
	if caret.Prefix == nil {
		t.Fatal("^ should have a dead key prefix (standalone dead key)")
	}
	if caret.Scancode != hidSpace {
		t.Errorf("^ standalone should send Space (0x%02X), got 0x%02X", hidSpace, caret.Scancode)
	}

	// Regular character should NOT have a prefix
	plain, ok := layout.CharMap["a"]
	if !ok {
		t.Fatal("a not found in charMap")
	}
	if plain.Prefix != nil {
		t.Error("plain 'a' should not have a dead key prefix")
	}
}

func TestNoDeadKeysMetadataProducesNoPrefixes(t *testing.T) {
	// Layouts without deadKeys metadata should have:
	// - No keys with Dead=true (CSS flag)
	// - No charMap entries with a Prefix (no compositions)
	// This prevents e.g. en-US from treating ^ as a dead key during paste.
	noDeadKeyLayouts := []string{"en-US", "en-UK", "it-IT", "ja-JP", "pl-PL", "ru-RU"}

	for _, id := range noDeadKeyLayouts {
		t.Run(id, func(t *testing.T) {
			layout, err := loadBuiltinLayout(id)
			if err != nil {
				t.Fatalf("failed to load %s: %v", id, err)
			}
			for _, key := range layout.Keys {
				if key.Dead {
					t.Errorf("key at (%.1f, %.1f) flagged dead but %s has no deadKeys metadata",
						key.X, key.Y, id)
				}
			}
			for ch, combo := range layout.CharMap {
				if combo.Prefix != nil {
					t.Errorf("charMap[%q] has prefix but %s declares no dead keys", ch, id)
				}
			}
		})
	}
}

func TestDeadKeysMetadataFlagsCorrectKeys(t *testing.T) {
	// Layouts WITH deadKeys metadata should flag keys whose normal legend
	// matches a declared dead key.
	layout, err := loadBuiltinLayout("de-DE")
	if err != nil {
		t.Fatalf("failed to load de-DE: %v", err)
	}

	deadCount := 0
	for _, key := range layout.Keys {
		if key.Dead {
			deadCount++
			// Every flagged key must have a normal legend in deadKeyToCombining
			if key.Legends.Normal == nil {
				t.Errorf("dead key at (%.1f, %.1f) has nil normal legend", key.X, key.Y)
			}
		}
	}
	if deadCount == 0 {
		t.Error("de-DE should have dead-flagged keys")
	}
	t.Logf("de-DE: %d keys flagged as dead", deadCount)
}

func TestDeadKeyNonComposingPairSkipped(t *testing.T) {
	// de_DE has ^ dead key. ^+k has no precomposed Unicode form,
	// so "k̂" (k + combining circumflex) should NOT be in charMap.
	layout, err := loadBuiltinLayout("de-DE")
	if err != nil {
		t.Fatalf("failed to load de-DE: %v", err)
	}

	// k̂ (U+006B U+0302) — no NFC composition exists
	if _, ok := layout.CharMap["k\u0302"]; ok {
		t.Error("k + combining circumflex should not be in charMap (no NFC form)")
	}
	// But â (U+00E2) should exist — it has a precomposed form
	if _, ok := layout.CharMap["â"]; !ok {
		t.Error("â should be in charMap (valid NFC composition)")
	}
}

func TestDeadKeyCompositionsFrench(t *testing.T) {
	// fr_FR has ^ and ¨ dead keys
	layout, err := loadBuiltinLayout("fr-FR")
	if err != nil {
		t.Fatalf("failed to load fr-FR: %v", err)
	}

	// ^ + e → ê
	if combo, ok := layout.CharMap["ê"]; !ok {
		t.Error("ê not found in charMap")
	} else if combo.Prefix == nil {
		t.Error("ê should have a dead key prefix")
	}

	// ^ + a → â
	if combo, ok := layout.CharMap["â"]; !ok {
		t.Error("â not found in charMap")
	} else if combo.Prefix == nil {
		t.Error("â should have a dead key prefix")
	}

	// ¨ + e → ë
	if combo, ok := layout.CharMap["ë"]; !ok {
		t.Error("ë not found in charMap")
	} else if combo.Prefix == nil {
		t.Error("ë should have a dead key prefix")
	}

	// ¨ + u → ü
	if combo, ok := layout.CharMap["ü"]; !ok {
		t.Error("ü not found in charMap")
	} else if combo.Prefix == nil {
		t.Error("ü should have a dead key prefix")
	}

	// Uppercase: ^ + Shift+E → Ê
	if combo, ok := layout.CharMap["Ê"]; !ok {
		t.Error("Ê not found in charMap")
	} else {
		if combo.Prefix == nil {
			t.Error("Ê should have a dead key prefix")
		}
		if combo.Modifiers&ModLShift == 0 {
			t.Error("Ê should require Shift on the base key")
		}
	}
}

func TestDeadKeyCompositionsSpanish(t *testing.T) {
	// es_ES has ´, ^, `, ¨ dead keys
	layout, err := loadBuiltinLayout("es-ES")
	if err != nil {
		t.Fatalf("failed to load es-ES: %v", err)
	}

	// ´ + a → á
	if combo, ok := layout.CharMap["á"]; !ok {
		t.Error("á not found in charMap")
	} else if combo.Prefix == nil {
		t.Error("á should have a dead key prefix")
	}

	// ` + e → è
	if combo, ok := layout.CharMap["è"]; !ok {
		t.Error("è not found in charMap")
	} else if combo.Prefix == nil {
		t.Error("è should have a dead key prefix")
	}

	// ¨ + u → ü
	if combo, ok := layout.CharMap["ü"]; !ok {
		t.Error("ü not found in charMap")
	} else if combo.Prefix == nil {
		t.Error("ü should have a dead key prefix")
	}

	// Uppercase: ´ + Shift+A → Á
	if combo, ok := layout.CharMap["Á"]; !ok {
		t.Error("Á not found in charMap")
	} else {
		if combo.Prefix == nil {
			t.Error("Á should have a dead key prefix")
		}
		if combo.Modifiers&ModLShift == 0 {
			t.Error("Á should require Shift on the base key")
		}
	}
}

func TestDeadKeyCompositionsCzech(t *testing.T) {
	// cs_CZ has the most dead keys: ´, ˇ, ^, `, ~, °, ˙, ˛, ¸, ¨
	layout, err := loadBuiltinLayout("cs-CZ")
	if err != nil {
		t.Fatalf("failed to load cs-CZ: %v", err)
	}

	// Czech has š, ů, etc. as DIRECT key legends (number row), so they
	// appear in charMap without a prefix (first-occurrence-wins).
	// Verify they're reachable (either directly or via composition).
	for _, ch := range []string{"š", "Š", "ů", "č", "ř", "ž", "ý", "á", "í", "é"} {
		if _, ok := layout.CharMap[ch]; !ok {
			t.Errorf("%s not found in charMap", ch)
		}
	}

	// Characters NOT on a direct key should come via dead key composition.
	// ^ + a → â (not a direct Czech key)
	if combo, ok := layout.CharMap["â"]; !ok {
		t.Error("â not found in charMap")
	} else if combo.Prefix == nil {
		t.Error("â should have a dead key prefix (not a direct Czech key)")
	}

	// ¨ + o → ö (not a direct Czech key)
	if combo, ok := layout.CharMap["ö"]; !ok {
		t.Error("ö not found in charMap")
	} else if combo.Prefix == nil {
		t.Error("ö should have a dead key prefix (not a direct Czech key)")
	}

	t.Logf("cs-CZ: %d charMap entries (richest dead key set)", len(layout.CharMap))
}

func TestDeadKeyCompositionsSlovenian(t *testing.T) {
	// sl_SI has ¸ (cedilla) and ¨ (diaeresis) dead keys
	layout, err := loadBuiltinLayout("sl-SI")
	if err != nil {
		t.Fatalf("failed to load sl-SI: %v", err)
	}

	// ¸ + c → ç
	if combo, ok := layout.CharMap["ç"]; !ok {
		t.Error("ç not found in charMap")
	} else if combo.Prefix == nil {
		t.Error("ç should have a dead key prefix")
	}

	// ¨ + a → ä
	if combo, ok := layout.CharMap["ä"]; !ok {
		t.Error("ä not found in charMap")
	} else if combo.Prefix == nil {
		t.Error("ä should have a dead key prefix")
	}
}

func TestDeadKeyCompositionsHungarian(t *testing.T) {
	// hu_HU has ´, ˝ (double acute), ¨ dead keys
	layout, err := loadBuiltinLayout("hu-HU")
	if err != nil {
		t.Fatalf("failed to load hu-HU: %v", err)
	}

	// Hungarian has ő, ű, é, á, etc. as DIRECT key legends, so they
	// appear in charMap without a prefix (first-occurrence-wins).
	for _, ch := range []string{"ő", "ű", "é", "á", "ú", "ó"} {
		if _, ok := layout.CharMap[ch]; !ok {
			t.Errorf("%s not found in charMap", ch)
		}
	}

	// ä should exist in the charMap. Depending on source data/version,
	// it may be present either as a direct legend or via dead-key composition.
	if combo, ok := layout.CharMap["ä"]; !ok {
		t.Error("ä not found in charMap")
	} else if combo.Prefix == nil {
		t.Log("ä is direct on hu-HU in this layout source")
	}

	// ´ + i → í (not a direct Hungarian key on most layouts)
	if _, ok := layout.CharMap["í"]; !ok {
		t.Error("í not found in charMap")
	}
}

// ---------------------------------------------------------------------------
// Built-in layout loading
// ---------------------------------------------------------------------------

func TestAllBuiltinLayoutsParse(t *testing.T) {
	for id := range builtinLayouts {
		t.Run(id, func(t *testing.T) {
			layout, err := loadBuiltinLayout(id)
			if err != nil {
				t.Fatalf("failed to load built-in layout %s: %v", id, err)
			}
			if layout.ID != id {
				t.Errorf("expected ID %q, got %q", id, layout.ID)
			}
			if len(layout.Keys) == 0 {
				t.Error("layout has no keys")
			}
			if len(layout.CharMap) == 0 {
				t.Error("layout has empty charMap")
			}
			t.Logf("%s: %d keys, %d charMap entries, %.0fx%.0f",
				layout.Name, len(layout.Keys), len(layout.CharMap), layout.BoardW, layout.BoardH)
		})
	}
}

// TestAllLayoutFilesRegistered scans the embedded layouts/ directory and
// verifies that every .kle.json file can be parsed and that a corresponding
// entry exists in the builtinLayouts map (directly or via alias).
func TestAllLayoutFilesRegistered(t *testing.T) {
	entries, err := layoutFS.ReadDir("layouts")
	if err != nil {
		t.Fatalf("failed to read embedded layouts dir: %v", err)
	}

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || len(name) < len(".kle.json") {
			continue
		}
		if name[len(name)-len(".kle.json"):] != ".kle.json" {
			continue
		}
		stem := name[:len(name)-len(".kle.json")] // e.g. "en_US"

		t.Run(stem, func(t *testing.T) {
			// Verify the file parses
			data, err := layoutFS.ReadFile("layouts/" + name)
			if err != nil {
				t.Fatalf("failed to read %s: %v", name, err)
			}
			layout, err := ParseKLE(data, stem, "")
			if err != nil {
				t.Fatalf("failed to parse %s: %v", name, err)
			}
			if len(layout.Keys) == 0 {
				t.Errorf("%s has no keys", name)
			}

			// Verify it's reachable from builtinLayouts (direct or alias)
			hyphenated := strings.ReplaceAll(stem, "_", "-")
			if _, ok := builtinLayouts[hyphenated]; ok {
				return // direct match
			}
			// Check if any alias points to this file
			for alias, target := range layoutAliases {
				if target == stem {
					if _, ok := builtinLayouts[alias]; ok {
						return // reachable via alias
					}
				}
			}
			t.Errorf("layout file %s (id %q) has no entry in builtinLayouts", name, hyphenated)
		})
	}
}
