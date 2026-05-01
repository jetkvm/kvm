package rfb

import "testing"

func TestHIDFromKeysymASCII(t *testing.T) {
	cases := []struct {
		keysym uint32
		hid    byte
	}{
		// Letters
		{'a', 0x04},
		{'A', 0x04}, // shared scancode with 'a'
		{'z', 0x1d},
		{'Z', 0x1d},
		{'m', 0x10},
		// Digits
		{'1', 0x1e},
		{'9', 0x26},
		{'0', 0x27},
		// Shifted digits
		{'!', 0x1e},
		{'@', 0x1f},
		{'#', 0x20},
		{'$', 0x21},
		{'%', 0x22},
		{'^', 0x23},
		{'&', 0x24},
		{'*', 0x25},
		{'(', 0x26},
		{')', 0x27},
		// Punctuation pairs
		{'-', 0x2d}, {'_', 0x2d},
		{'=', 0x2e}, {'+', 0x2e},
		{'[', 0x2f}, {'{', 0x2f},
		{']', 0x30}, {'}', 0x30},
		{'\\', 0x31}, {'|', 0x31},
		{';', 0x33}, {':', 0x33},
		{'\'', 0x34}, {'"', 0x34},
		{'`', 0x35}, {'~', 0x35},
		{',', 0x36}, {'<', 0x36},
		{'.', 0x37}, {'>', 0x37},
		{'/', 0x38}, {'?', 0x38},
		{' ', 0x2c},
	}
	for _, c := range cases {
		got, ok := HIDFromKeysym(c.keysym)
		if !ok {
			t.Errorf("keysym %#06x (%q): no mapping", c.keysym, rune(c.keysym))
			continue
		}
		if got != c.hid {
			t.Errorf("keysym %#06x (%q): got %#02x, want %#02x", c.keysym, rune(c.keysym), got, c.hid)
		}
	}
}

func TestHIDFromKeysymControl(t *testing.T) {
	cases := []struct {
		keysym uint32
		hid    byte
	}{
		{0xff08, 0x2a}, // BackSpace
		{0xff09, 0x2b}, // Tab
		{0xff0d, 0x28}, // Return
		{0xff1b, 0x29}, // Escape
		{0xffff, 0x4c}, // Delete
		{0xff13, 0x48}, // Pause
		{0xff14, 0x47}, // Scroll_Lock
		{0xff15, 0x46}, // Sys_Req
	}
	for _, c := range cases {
		got, ok := HIDFromKeysym(c.keysym)
		if !ok || got != c.hid {
			t.Errorf("keysym %#06x: got %#02x ok=%v, want %#02x", c.keysym, got, ok, c.hid)
		}
	}
}

func TestHIDFromKeysymNavigation(t *testing.T) {
	cases := []struct {
		keysym uint32
		hid    byte
	}{
		{0xff50, 0x4a}, // Home
		{0xff51, 0x50}, // Left
		{0xff52, 0x52}, // Up
		{0xff53, 0x4f}, // Right
		{0xff54, 0x51}, // Down
		{0xff55, 0x4b}, // Page_Up
		{0xff56, 0x4e}, // Page_Down
		{0xff57, 0x4d}, // End
		{0xff63, 0x49}, // Insert
		{0xff67, 0x65}, // Menu
	}
	for _, c := range cases {
		got, ok := HIDFromKeysym(c.keysym)
		if !ok || got != c.hid {
			t.Errorf("keysym %#06x: got %#02x ok=%v, want %#02x", c.keysym, got, ok, c.hid)
		}
	}
}

func TestHIDFromKeysymFunctionKeys(t *testing.T) {
	// F1=0xffbe -> 0x3a, F12=0xffc9 -> 0x45
	if got, _ := HIDFromKeysym(0xffbe); got != 0x3a {
		t.Errorf("F1: got %#02x, want 0x3a", got)
	}
	if got, _ := HIDFromKeysym(0xffc9); got != 0x45 {
		t.Errorf("F12: got %#02x, want 0x45", got)
	}
	// F13=0xffca -> 0x68, F24=0xffd5 -> 0x73
	if got, _ := HIDFromKeysym(0xffca); got != 0x68 {
		t.Errorf("F13: got %#02x, want 0x68", got)
	}
	if got, _ := HIDFromKeysym(0xffd5); got != 0x73 {
		t.Errorf("F24: got %#02x, want 0x73", got)
	}
}

func TestHIDFromKeysymModifiers(t *testing.T) {
	cases := []struct {
		keysym uint32
		hid    byte
	}{
		{0xffe1, 0xe1}, // Shift_L
		{0xffe2, 0xe5}, // Shift_R
		{0xffe3, 0xe0}, // Control_L
		{0xffe4, 0xe4}, // Control_R
		{0xffe9, 0xe2}, // Alt_L
		{0xffea, 0xe6}, // Alt_R
		{0xffeb, 0xe3}, // Super_L
		{0xffec, 0xe7}, // Super_R
		{0xfe03, 0xe6}, // ISO_Level3_Shift -> Right Alt (AltGr)
		{0xffe5, 0x39}, // Caps_Lock
	}
	for _, c := range cases {
		got, ok := HIDFromKeysym(c.keysym)
		if !ok || got != c.hid {
			t.Errorf("keysym %#06x: got %#02x ok=%v, want %#02x", c.keysym, got, ok, c.hid)
		}
	}
}

func TestHIDFromKeysymKeypad(t *testing.T) {
	cases := []struct {
		keysym uint32
		hid    byte
	}{
		{0xff7f, 0x53}, // Num_Lock
		{0xff8d, 0x58}, // KP_Enter
		{0xffaa, 0x55}, // KP_Multiply
		{0xffab, 0x57}, // KP_Add
		{0xffad, 0x56}, // KP_Subtract
		{0xffae, 0x63}, // KP_Decimal
		{0xffaf, 0x54}, // KP_Divide
		{0xffbd, 0x67}, // KP_Equal
		{0xffb0, 0x62}, // KP_0
		{0xffb1, 0x59}, // KP_1
		{0xffb9, 0x61}, // KP_9
	}
	for _, c := range cases {
		got, ok := HIDFromKeysym(c.keysym)
		if !ok || got != c.hid {
			t.Errorf("keysym %#06x: got %#02x ok=%v, want %#02x", c.keysym, got, ok, c.hid)
		}
	}
}

func TestHIDFromKeysymUnmapped(t *testing.T) {
	// XF86 multimedia keys (e.g. XF86AudioRaiseVolume = 0x1008ff13)
	// are intentionally out of scope for v1.
	if _, ok := HIDFromKeysym(0x1008ff13); ok {
		t.Errorf("expected XF86AudioRaiseVolume to be unmapped")
	}
	// Zero keysym is invalid in RFB.
	if _, ok := HIDFromKeysym(0); ok {
		t.Errorf("expected zero keysym to be unmapped")
	}
}

func TestIsModifierKeysym(t *testing.T) {
	yes := []uint32{0xffe1, 0xffe2, 0xffe3, 0xffe4, 0xffe7, 0xffe8, 0xffe9, 0xffea, 0xffeb, 0xffec, 0xfe03}
	no := []uint32{'a', 'A', '1', 0xff08, 0xff0d, 0xff50, 0xffbe}
	for _, k := range yes {
		if !IsModifierKeysym(k) {
			t.Errorf("%#06x: expected modifier", k)
		}
	}
	for _, k := range no {
		if IsModifierKeysym(k) {
			t.Errorf("%#06x: not a modifier", k)
		}
	}
}
