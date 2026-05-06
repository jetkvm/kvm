package rfb

import "testing"

func TestPointerMaskToHIDButtons(t *testing.T) {
	cases := []struct {
		name string
		in   uint8
		want uint8
	}{
		{"none", 0, 0},
		{"left only", PointerButtonLeft, 0b0000_0001},
		// RFB middle (bit 1) -> HID middle (bit 2)
		{"middle only (RFB bit 1)", PointerButtonMiddle, 0b0000_0100},
		// RFB right (bit 2) -> HID right (bit 1)
		{"right only (RFB bit 2)", PointerButtonRight, 0b0000_0010},
		{"left + right", PointerButtonLeft | PointerButtonRight, 0b0000_0011},
		{"left + middle + right",
			PointerButtonLeft | PointerButtonMiddle | PointerButtonRight,
			0b0000_0111},
		// Wheel bits (3..6) must be dropped, not passed through.
		{"wheel up dropped", PointerButtonUp, 0},
		{"wheel down dropped", PointerButtonDown, 0},
		{"wheel left dropped", PointerButtonLeftWh, 0},
		{"wheel right dropped", PointerButtonRightW, 0},
		{"left + wheel up", PointerButtonLeft | PointerButtonUp, 0b0000_0001},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := PointerMaskToHIDButtons(c.in); got != c.want {
				t.Errorf("PointerMaskToHIDButtons(%#02x) = %#02x, want %#02x",
					c.in, got, c.want)
			}
		})
	}
}
