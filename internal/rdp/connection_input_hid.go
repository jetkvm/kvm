package rdp

// RDP scancode to USB HID code conversion.
// This file contains the keyboard mapping table for RDP input handling.

// scancodeToHID converts an RDP scancode to HID code.
// This is a simplified version - a full implementation would handle all scancodes.
func scancodeToHID(scancode uint16, flags uint16) uint8 {
	// Extended key flag
	extended := flags&0x0100 != 0

	// Basic scancode to HID mapping
	// This is the US keyboard layout mapping
	var hidCode uint8

	switch scancode {
	case 0x01:
		hidCode = 0x29 // Escape
	case 0x02:
		hidCode = 0x1E // 1
	case 0x03:
		hidCode = 0x1F // 2
	case 0x04:
		hidCode = 0x20 // 3
	case 0x05:
		hidCode = 0x21 // 4
	case 0x06:
		hidCode = 0x22 // 5
	case 0x07:
		hidCode = 0x23 // 6
	case 0x08:
		hidCode = 0x24 // 7
	case 0x09:
		hidCode = 0x25 // 8
	case 0x0A:
		hidCode = 0x26 // 9
	case 0x0B:
		hidCode = 0x27 // 0
	case 0x0C:
		hidCode = 0x2D // -
	case 0x0D:
		hidCode = 0x2E // =
	case 0x0E:
		hidCode = 0x2A // Backspace
	case 0x0F:
		hidCode = 0x2B // Tab
	case 0x10:
		hidCode = 0x14 // Q
	case 0x11:
		hidCode = 0x1A // W
	case 0x12:
		hidCode = 0x08 // E
	case 0x13:
		hidCode = 0x15 // R
	case 0x14:
		hidCode = 0x17 // T
	case 0x15:
		hidCode = 0x1C // Y
	case 0x16:
		hidCode = 0x18 // U
	case 0x17:
		hidCode = 0x0C // I
	case 0x18:
		hidCode = 0x12 // O
	case 0x19:
		hidCode = 0x13 // P
	case 0x1A:
		hidCode = 0x2F // [
	case 0x1B:
		hidCode = 0x30 // ]
	case 0x1C:
		if extended {
			hidCode = 0x58 // Numpad Enter
		} else {
			hidCode = 0x28 // Enter
		}
	case 0x1D:
		if extended {
			hidCode = 0xE4 // Right Control
		} else {
			hidCode = 0xE0 // Left Control
		}
	case 0x1E:
		hidCode = 0x04 // A
	case 0x1F:
		hidCode = 0x16 // S
	case 0x20:
		hidCode = 0x07 // D
	case 0x21:
		hidCode = 0x09 // F
	case 0x22:
		hidCode = 0x0A // G
	case 0x23:
		hidCode = 0x0B // H
	case 0x24:
		hidCode = 0x0D // J
	case 0x25:
		hidCode = 0x0E // K
	case 0x26:
		hidCode = 0x0F // L
	case 0x27:
		hidCode = 0x33 // ;
	case 0x28:
		hidCode = 0x34 // '
	case 0x29:
		hidCode = 0x35 // `
	case 0x2A:
		hidCode = 0xE1 // Left Shift
	case 0x2B:
		hidCode = 0x31 // backslash
	case 0x2C:
		hidCode = 0x1D // Z
	case 0x2D:
		hidCode = 0x1B // X
	case 0x2E:
		hidCode = 0x06 // C
	case 0x2F:
		hidCode = 0x19 // V
	case 0x30:
		hidCode = 0x05 // B
	case 0x31:
		hidCode = 0x11 // N
	case 0x32:
		hidCode = 0x10 // M
	case 0x33:
		hidCode = 0x36 // ,
	case 0x34:
		hidCode = 0x37 // .
	case 0x35:
		if extended {
			hidCode = 0x54 // Numpad /
		} else {
			hidCode = 0x38 // /
		}
	case 0x36:
		hidCode = 0xE5 // Right Shift
	case 0x37:
		hidCode = 0x55 // Numpad *
	case 0x38:
		if extended {
			hidCode = 0xE6 // Right Alt
		} else {
			hidCode = 0xE2 // Left Alt
		}
	case 0x39:
		hidCode = 0x2C // Space
	case 0x3A:
		hidCode = 0x39 // Caps Lock
	case 0x3B:
		hidCode = 0x3A // F1
	case 0x3C:
		hidCode = 0x3B // F2
	case 0x3D:
		hidCode = 0x3C // F3
	case 0x3E:
		hidCode = 0x3D // F4
	case 0x3F:
		hidCode = 0x3E // F5
	case 0x40:
		hidCode = 0x3F // F6
	case 0x41:
		hidCode = 0x40 // F7
	case 0x42:
		hidCode = 0x41 // F8
	case 0x43:
		hidCode = 0x42 // F9
	case 0x44:
		hidCode = 0x43 // F10
	case 0x45:
		hidCode = 0x53 // Num Lock
	case 0x46:
		hidCode = 0x47 // Scroll Lock
	case 0x47:
		if extended {
			hidCode = 0x4A // Home
		} else {
			hidCode = 0x5F // Numpad 7
		}
	case 0x48:
		if extended {
			hidCode = 0x52 // Up Arrow
		} else {
			hidCode = 0x60 // Numpad 8
		}
	case 0x49:
		if extended {
			hidCode = 0x4B // Page Up
		} else {
			hidCode = 0x61 // Numpad 9
		}
	case 0x4A:
		hidCode = 0x56 // Numpad -
	case 0x4B:
		if extended {
			hidCode = 0x50 // Left Arrow
		} else {
			hidCode = 0x5C // Numpad 4
		}
	case 0x4C:
		hidCode = 0x5D // Numpad 5
	case 0x4D:
		if extended {
			hidCode = 0x4F // Right Arrow
		} else {
			hidCode = 0x5E // Numpad 6
		}
	case 0x4E:
		hidCode = 0x57 // Numpad +
	case 0x4F:
		if extended {
			hidCode = 0x4D // End
		} else {
			hidCode = 0x59 // Numpad 1
		}
	case 0x50:
		if extended {
			hidCode = 0x51 // Down Arrow
		} else {
			hidCode = 0x5A // Numpad 2
		}
	case 0x51:
		if extended {
			hidCode = 0x4E // Page Down
		} else {
			hidCode = 0x5B // Numpad 3
		}
	case 0x52:
		if extended {
			hidCode = 0x49 // Insert
		} else {
			hidCode = 0x62 // Numpad 0
		}
	case 0x53:
		if extended {
			hidCode = 0x4C // Delete
		} else {
			hidCode = 0x63 // Numpad .
		}
	case 0x57:
		hidCode = 0x44 // F11
	case 0x58:
		hidCode = 0x45 // F12
	case 0x5B:
		hidCode = 0xE3 // Left GUI (Windows)
	case 0x5C:
		hidCode = 0xE7 // Right GUI (Windows)
	case 0x5D:
		hidCode = 0x65 // Application (Menu)
	}

	return hidCode
}
