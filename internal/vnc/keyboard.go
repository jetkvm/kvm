package vnc

// keysymToHID converts X11 keysym codes (used by VNC/RFB protocol) to USB HID usage codes.
//
// References:
//   - X11 Keysyms: https://www.x.org/releases/current/doc/xproto/x11protocol.html#keysym_encoding
//     Source header: /usr/include/X11/keysymdef.h
//   - USB HID Usage Tables: https://usb.org/document-library/hid-usage-tables-15
//     Section 10: Keyboard/Keypad Page (0x07)
//
// Supported keyboard layouts:
//   - US ANSI (104-key)
//   - UK/British ISO (105-key with § ± keys)
//   - German QWERTZ (ä ö ü ß)
//   - French AZERTY (é è ç à ù)
//   - Nordic (Swedish, Norwegian, Danish, Finnish - å ø æ)
//   - Spanish (ñ ¿ ¡)
//   - Portuguese (ã õ)
//   - Italian (ì ò)
//   - Japanese JIS (変換, 無変換, ひらがな)
//   - Korean (한/영, 한자)
//   - Mac extended (F13-F24, Command/Meta keys)
//
// Also supports:
//   - Dead keys for composing accented characters
//   - Numpad with and without NumLock
//   - Currency symbols (€ £ ¥ ¢)
//   - Power key (Mac)
//
// NOT supported (requires USB Consumer HID page, not keyboard page):
//   - Media keys (volume, play/pause, etc.)
//   - Browser keys (back, forward, refresh, etc.)
//
// Returns 0 for unsupported keysyms.
func keysymToHID(keysym uint32) uint8 {
	// Modifier keys
	switch keysym {
	case 0xFFE1:
		return 0xE1 // Left Shift
	case 0xFFE2:
		return 0xE5 // Right Shift
	case 0xFFE3:
		return 0xE0 // Left Control
	case 0xFFE4:
		return 0xE4 // Right Control
	case 0xFFE7:
		return 0xE3 // Left Meta (macOS Command key)
	case 0xFFE8:
		return 0xE7 // Right Meta (macOS Command key)
	case 0xFFE9:
		return 0xE2 // Left Alt
	case 0xFFEA:
		return 0xE6 // Right Alt
	case 0xFFEB:
		return 0xE3 // Left Super (Windows/Linux GUI key)
	case 0xFFEC:
		return 0xE7 // Right Super (Windows/Linux GUI key)
	case 0xFFED:
		return 0xE3 // Left Hyper -> map to GUI
	case 0xFFEE:
		return 0xE7 // Right Hyper -> map to GUI
	}

	// Function keys F1-F12 (0xFFBE-0xFFC9)
	if keysym >= 0xFFBE && keysym <= 0xFFC9 {
		return uint8(0x3A + (keysym - 0xFFBE))
	}

	// Function keys F13-F24 (0xFFCA-0xFFD5) - Mac extended keyboards
	if keysym >= 0xFFCA && keysym <= 0xFFD5 {
		return uint8(0x68 + (keysym - 0xFFCA)) // F13=0x68, F14=0x69, ... F24=0x73
	}

	// Navigation and special keys
	switch keysym {
	case 0xFF08:
		return 0x2A // Backspace
	case 0xFF09:
		return 0x2B // Tab
	case 0xFF0D:
		return 0x28 // Enter/Return
	case 0xFF8D:
		return 0x58 // KP_Enter
	case 0xFF0A:
		return 0x28 // Linefeed -> Enter
	case 0xFF1B:
		return 0x29 // Escape
	case 0xFFFF:
		return 0x4C // Delete (forward)
	case 0xFF50:
		return 0x4A // Home
	case 0xFF51:
		return 0x50 // Left Arrow
	case 0xFF52:
		return 0x52 // Up Arrow
	case 0xFF53:
		return 0x4F // Right Arrow
	case 0xFF54:
		return 0x51 // Down Arrow
	case 0xFF55:
		return 0x4B // Page Up
	case 0xFF56:
		return 0x4E // Page Down
	case 0xFF57:
		return 0x4D // End
	case 0xFF58:
		return 0x4A // Begin -> Home
	case 0xFF63:
		return 0x49 // Insert
	case 0xFF14:
		return 0x47 // Scroll Lock
	case 0xFF7F:
		return 0x53 // Num Lock
	case 0xFF13:
		return 0x48 // Pause
	case 0xFF6B:
		return 0x48 // Break -> Pause
	case 0xFF61:
		return 0x46 // Print Screen
	case 0xFF15:
		return 0x46 // Sys_Req -> Print Screen
	case 0xFF20:
		return 0x65 // Multi_key -> Menu/Application
	case 0xFF67:
		return 0x65 // Menu
	case 0xFFE5:
		return 0x39 // Caps Lock
	case 0xFFE6:
		return 0x39 // Shift_Lock -> Caps Lock
	case 0x0020:
		return 0x2C // Space
	case 0xFF80:
		return 0x2C // KP_Space -> Space
	}

	// Lowercase letters a-z (0x0061-0x007A)
	if keysym >= 0x0061 && keysym <= 0x007A {
		return uint8(0x04 + (keysym - 0x0061))
	}

	// Uppercase letters A-Z (0x0041-0x005A) - same HID codes as lowercase
	if keysym >= 0x0041 && keysym <= 0x005A {
		return uint8(0x04 + (keysym - 0x0041))
	}

	// Numbers 0-9 (0x0030-0x0039)
	if keysym >= 0x0030 && keysym <= 0x0039 {
		if keysym == 0x0030 {
			return 0x27 // 0
		}
		return uint8(0x1E + (keysym - 0x0031)) // 1-9
	}

	// Basic punctuation and symbols
	switch keysym {
	case 0x002D:
		return 0x2D // - (minus/hyphen)
	case 0x003D:
		return 0x2E // =
	case 0x005B:
		return 0x2F // [
	case 0x005D:
		return 0x30 // ]
	case 0x005C:
		return 0x31 // \ (backslash)
	case 0x003B:
		return 0x33 // ;
	case 0x0027:
		return 0x34 // ' (apostrophe)
	case 0x0060:
		return 0x35 // ` (grave/backtick)
	case 0x002C:
		return 0x36 // ,
	case 0x002E:
		return 0x37 // .
	case 0x002F:
		return 0x38 // /
	}

	// Shifted symbols - map to base key (shift state handled separately by VNC client)
	switch keysym {
	case 0x0021:
		return 0x1E // ! -> 1
	case 0x0040:
		return 0x1F // @ -> 2
	case 0x0023:
		return 0x20 // # -> 3
	case 0x0024:
		return 0x21 // $ -> 4
	case 0x0025:
		return 0x22 // % -> 5
	case 0x005E:
		return 0x23 // ^ -> 6
	case 0x0026:
		return 0x24 // & -> 7
	case 0x002A:
		return 0x25 // * -> 8
	case 0x0028:
		return 0x26 // ( -> 9
	case 0x0029:
		return 0x27 // ) -> 0
	case 0x005F:
		return 0x2D // _ -> -
	case 0x002B:
		return 0x2E // + -> =
	case 0x007B:
		return 0x2F // { -> [
	case 0x007D:
		return 0x30 // } -> ]
	case 0x007C:
		return 0x31 // | -> \
	case 0x003A:
		return 0x33 // : -> ;
	case 0x0022:
		return 0x34 // " -> '
	case 0x007E:
		return 0x35 // ~ -> `
	case 0x003C:
		return 0x36 // < -> ,
	case 0x003E:
		return 0x37 // > -> .
	case 0x003F:
		return 0x38 // ? -> /
	}

	// Numpad keys
	switch keysym {
	case 0xFFAA:
		return 0x55 // KP_Multiply
	case 0xFFAB:
		return 0x57 // KP_Add
	case 0xFFAC:
		return 0x85 // KP_Separator (comma on some layouts)
	case 0xFFAD:
		return 0x56 // KP_Subtract
	case 0xFFAE:
		return 0x63 // KP_Decimal
	case 0xFFAF:
		return 0x54 // KP_Divide
	case 0xFFB0:
		return 0x62 // KP_0
	case 0xFFB1:
		return 0x59 // KP_1
	case 0xFFB2:
		return 0x5A // KP_2
	case 0xFFB3:
		return 0x5B // KP_3
	case 0xFFB4:
		return 0x5C // KP_4
	case 0xFFB5:
		return 0x5D // KP_5
	case 0xFFB6:
		return 0x5E // KP_6
	case 0xFFB7:
		return 0x5F // KP_7
	case 0xFFB8:
		return 0x60 // KP_8
	case 0xFFB9:
		return 0x61 // KP_9
	case 0xFFBD:
		return 0x67 // KP_Equal (Mac keyboards)
	// Numpad navigation (when NumLock is off)
	case 0xFF95:
		return 0x5C // KP_Home -> KP_7
	case 0xFF96:
		return 0x50 // KP_Left -> Left
	case 0xFF97:
		return 0x52 // KP_Up -> Up
	case 0xFF98:
		return 0x4F // KP_Right -> Right
	case 0xFF99:
		return 0x51 // KP_Down -> Down
	case 0xFF9A:
		return 0x60 // KP_Page_Up -> KP_9
	case 0xFF9B:
		return 0x5A // KP_Page_Down -> KP_3
	case 0xFF9C:
		return 0x62 // KP_End -> KP_1
	case 0xFF9D:
		return 0x5D // KP_Begin -> KP_5
	case 0xFF9E:
		return 0x62 // KP_Insert -> KP_0
	case 0xFF9F:
		return 0x63 // KP_Delete -> KP_Decimal
	}

	// ISO keyboard: extra key between left shift and Z (non-US backslash)
	switch keysym {
	case 0x00A6:
		return 0x64 // ¦ (broken bar) - ISO key
	case 0x00A7:
		return 0x35 // § (section) - UK/German, left of 1
	case 0x00B0:
		return 0x35 // ° (degree) - German shift+^
	case 0x00B1:
		return 0x35 // ± (plus-minus) - UK shift+§
	case 0x00B2:
		return 0x1F // ² (superscript 2) - German
	case 0x00B3:
		return 0x20 // ³ (superscript 3) - German
	case 0x00B4:
		return 0x34 // ´ (acute accent) - dead key result
	case 0x00B5:
		return 0x10 // µ (micro) - AltGr+M on some layouts
	case 0x00AC:
		return 0x35 // ¬ (not sign) - UK
	}

	// German keyboard specific
	switch keysym {
	case 0x00E4:
		return 0x34 // ä -> ' key position (German layout)
	case 0x00C4:
		return 0x34 // Ä -> ' key position
	case 0x00F6:
		return 0x33 // ö -> ; key position
	case 0x00D6:
		return 0x33 // Ö -> ; key position
	case 0x00FC:
		return 0x2F // ü -> [ key position
	case 0x00DC:
		return 0x2F // Ü -> [ key position
	case 0x00DF:
		return 0x2D // ß -> - key position
	case 0x1E9E:
		return 0x2D // ẞ (capital ß) -> - key position
	}

	// French keyboard specific (AZERTY)
	switch keysym {
	case 0x00E0:
		return 0x27 // à -> 0 key position
	case 0x00C0:
		return 0x27 // À
	case 0x00E7:
		return 0x26 // ç -> 9 key position
	case 0x00C7:
		return 0x26 // Ç
	case 0x00E8:
		return 0x24 // è -> 7 key position
	case 0x00C8:
		return 0x24 // È
	case 0x00E9:
		return 0x1F // é -> 2 key position
	case 0x00C9:
		return 0x1F // É
	case 0x00F9:
		return 0x34 // ù -> ' key position
	case 0x00D9:
		return 0x34 // Ù
	}

	// Nordic keyboard specific (Swedish, Norwegian, Danish, Finnish)
	switch keysym {
	case 0x00E5:
		return 0x2F // å -> [ key position
	case 0x00C5:
		return 0x2F // Å
	case 0x00E6:
		return 0x34 // æ -> ' key position
	case 0x00C6:
		return 0x34 // Æ
	case 0x00F8:
		return 0x33 // ø -> ; key position
	case 0x00D8:
		return 0x33 // Ø
	}

	// Spanish keyboard specific
	switch keysym {
	case 0x00F1:
		return 0x33 // ñ -> ; key position
	case 0x00D1:
		return 0x33 // Ñ
	case 0x00BF:
		return 0x2E // ¿ -> = key position
	case 0x00A1:
		return 0x2E // ¡ -> = key position
	}

	// Portuguese keyboard specific
	switch keysym {
	case 0x00E3:
		return 0x34 // ã -> ' key position
	case 0x00C3:
		return 0x34 // Ã
	case 0x00F5:
		return 0x33 // õ -> ; key position
	case 0x00D5:
		return 0x33 // Õ
	}

	// Italian keyboard specific
	switch keysym {
	case 0x00EC:
		return 0x2E // ì -> = key position
	case 0x00CC:
		return 0x2E // Ì
	case 0x00F2:
		return 0x33 // ò -> ; key position
	case 0x00D2:
		return 0x33 // Ò
	}

	// Common European characters (Latin-1 Supplement: 0x00C0-0x00FF)
	switch keysym {
	case 0x00E2:
		return 0x04 // â -> a
	case 0x00C2:
		return 0x04 // Â
	case 0x00EA:
		return 0x08 // ê -> e
	case 0x00CA:
		return 0x08 // Ê
	case 0x00EB:
		return 0x08 // ë -> e
	case 0x00CB:
		return 0x08 // Ë
	case 0x00EE:
		return 0x0C // î -> i
	case 0x00CE:
		return 0x0C // Î
	case 0x00EF:
		return 0x0C // ï -> i
	case 0x00CF:
		return 0x0C // Ï
	case 0x00F4:
		return 0x12 // ô -> o
	case 0x00D4:
		return 0x12 // Ô
	case 0x00FB:
		return 0x18 // û -> u
	case 0x00DB:
		return 0x18 // Û
	case 0x00FF:
		return 0x1C // ÿ -> y
	case 0x0178:
		return 0x1C // Ÿ
	}

	// Romanian specific (Latin Extended-A and -B)
	switch keysym {
	case 0x0103, 0x01E3: // ă (a-breve) - both Unicode and legacy keysym
		return 0x04 // -> a
	case 0x0102, 0x01C3: // Ă
		return 0x04
	case 0x0219, 0x015F: // ș (s-comma-below) and ş (s-cedilla, older)
		return 0x16 // -> s
	case 0x0218, 0x015E: // Ș and Ş
		return 0x16
	case 0x021B, 0x0163: // ț (t-comma-below) and ţ (t-cedilla, older)
		return 0x17 // -> t
	case 0x021A, 0x0162: // Ț and Ţ
		return 0x17
	}

	// Turkish specific
	switch keysym {
	case 0x011F: // ğ (g-breve)
		return 0x0A // -> g
	case 0x011E: // Ğ
		return 0x0A
	case 0x0131: // ı (dotless i)
		return 0x0C // -> i
	case 0x0130: // İ (I with dot)
		return 0x0C
	case 0x015F: // ş (s-cedilla) - also used in Turkish
		return 0x16 // -> s
	case 0x015E: // Ş
		return 0x16
	}

	// Polish specific
	switch keysym {
	case 0x0105: // ą (a-ogonek)
		return 0x04 // -> a
	case 0x0104: // Ą
		return 0x04
	case 0x0107: // ć (c-acute)
		return 0x06 // -> c
	case 0x0106: // Ć
		return 0x06
	case 0x0119: // ę (e-ogonek)
		return 0x08 // -> e
	case 0x0118: // Ę
		return 0x08
	case 0x0142: // ł (l-stroke)
		return 0x0F // -> l
	case 0x0141: // Ł
		return 0x0F
	case 0x0144: // ń (n-acute)
		return 0x11 // -> n
	case 0x0143: // Ń
		return 0x11
	case 0x00F3: // ó (o-acute) - also Latin-1
		return 0x12 // -> o
	case 0x00D3: // Ó
		return 0x12
	case 0x015B: // ś (s-acute)
		return 0x16 // -> s
	case 0x015A: // Ś
		return 0x16
	case 0x017A: // ź (z-acute)
		return 0x1D // -> z
	case 0x0179: // Ź
		return 0x1D
	case 0x017C: // ż (z-dot-above)
		return 0x1D // -> z
	case 0x017B: // Ż
		return 0x1D
	}

	// Czech/Slovak specific
	switch keysym {
	case 0x010D: // č (c-caron)
		return 0x06 // -> c
	case 0x010C: // Č
		return 0x06
	case 0x010F: // ď (d-caron)
		return 0x07 // -> d
	case 0x010E: // Ď
		return 0x07
	case 0x011B: // ě (e-caron)
		return 0x08 // -> e
	case 0x011A: // Ě
		return 0x08
	case 0x0148: // ň (n-caron)
		return 0x11 // -> n
	case 0x0147: // Ň
		return 0x11
	case 0x0159: // ř (r-caron)
		return 0x15 // -> r
	case 0x0158: // Ř
		return 0x15
	case 0x0161: // š (s-caron)
		return 0x16 // -> s
	case 0x0160: // Š
		return 0x16
	case 0x0165: // ť (t-caron)
		return 0x17 // -> t
	case 0x0164: // Ť
		return 0x17
	case 0x016F: // ů (u-ring)
		return 0x18 // -> u
	case 0x016E: // Ů
		return 0x18
	case 0x017E: // ž (z-caron)
		return 0x1D // -> z
	case 0x017D: // Ž
		return 0x1D
	}

	// Hungarian specific
	switch keysym {
	case 0x0151: // ő (o-double-acute)
		return 0x12 // -> o
	case 0x0150: // Ő
		return 0x12
	case 0x0171: // ű (u-double-acute)
		return 0x18 // -> u
	case 0x0170: // Ű
		return 0x18
	}

	// Croatian/Slovenian specific
	switch keysym {
	case 0x0111: // đ (d-stroke)
		return 0x07 // -> d
	case 0x0110: // Đ
		return 0x07
	}

	// Icelandic specific
	switch keysym {
	case 0x00FE: // þ (thorn)
		return 0x17 // -> t (closest approximation)
	case 0x00DE: // Þ
		return 0x17
	case 0x00F0: // ð (eth)
		return 0x07 // -> d
	case 0x00D0: // Ð
		return 0x07
	}

	// Latvian/Lithuanian specific
	switch keysym {
	case 0x0100, 0x0101: // Ā/ā (A macron)
		return 0x04 // -> a
	case 0x010A, 0x010B: // Ċ/ċ (C dot above)
		return 0x06 // -> c
	case 0x0112, 0x0113: // Ē/ē (E macron)
		return 0x08 // -> e
	case 0x0116, 0x0117: // Ė/ė (E dot above)
		return 0x08 // -> e
	case 0x0122, 0x0123: // Ģ/ģ (G cedilla)
		return 0x0A // -> g
	case 0x012A, 0x012B: // Ī/ī (I macron)
		return 0x0C // -> i
	case 0x012E, 0x012F: // Į/į (I ogonek)
		return 0x0C // -> i
	case 0x0136, 0x0137: // Ķ/ķ (K cedilla)
		return 0x0E // -> k
	case 0x013B, 0x013C: // Ļ/ļ (L cedilla)
		return 0x0F // -> l
	case 0x0145, 0x0146: // Ņ/ņ (N cedilla)
		return 0x11 // -> n
	case 0x014C, 0x014D: // Ō/ō (O macron)
		return 0x12 // -> o
	case 0x0156, 0x0157: // Ŗ/ŗ (R cedilla)
		return 0x15 // -> r
	case 0x016A, 0x016B: // Ū/ū (U macron)
		return 0x18 // -> u
	case 0x0172, 0x0173: // Ų/ų (U ogonek)
		return 0x18 // -> u
	case 0x0174, 0x0175: // Ŵ/ŵ (W circumflex)
		return 0x1A // -> w
	case 0x0176, 0x0177: // Ŷ/ŷ (Y circumflex)
		return 0x1C // -> y
	}

	// Vietnamese diacritics (Latin Extended Additional: 0x1E00-0x1EFF)
	// Map to base letters - host OS handles composition
	switch {
	// A with various diacritics
	case keysym >= 0x1EA0 && keysym <= 0x1EB7:
		return 0x04 // -> a (ạ ả ấ ầ ẩ ẫ ậ ắ ằ ẳ ẵ ặ)
	// E with various diacritics
	case keysym >= 0x1EB8 && keysym <= 0x1EC7:
		return 0x08 // -> e (ẹ ẻ ẽ ế ề ể ễ ệ)
	// I with various diacritics
	case keysym >= 0x1EC8 && keysym <= 0x1ECB:
		return 0x0C // -> i (ỉ ị)
	// O with various diacritics
	case keysym >= 0x1ECC && keysym <= 0x1EE3:
		return 0x12 // -> o (ọ ỏ ố ồ ổ ỗ ộ ớ ờ ở ỡ ợ)
	// U with various diacritics
	case keysym >= 0x1EE4 && keysym <= 0x1EF1:
		return 0x18 // -> u (ụ ủ ứ ừ ử ữ ự)
	// Y with various diacritics
	case keysym >= 0x1EF2 && keysym <= 0x1EF9:
		return 0x1C // -> y (ỳ ỵ ỷ ỹ)
	}

	// Additional Latin-1 Supplement characters (0x00C0-0x00FF)
	switch keysym {
	case 0x00E1, 0x00C1: // á/Á (a acute)
		return 0x04 // -> a
	case 0x00ED, 0x00CD: // í/Í (i acute)
		return 0x0C // -> i
	case 0x00FA, 0x00DA: // ú/Ú (u acute)
		return 0x18 // -> u
	case 0x00FD, 0x00DD: // ý/Ý (y acute)
		return 0x1C // -> y
	}

	// Maltese specific
	switch keysym {
	case 0x0120, 0x0121: // Ġ/ġ (G dot above)
		return 0x0A // -> g
	case 0x0126, 0x0127: // Ħ/ħ (H stroke)
		return 0x0B // -> h
	}

	// Welsh specific
	switch keysym {
	case 0x1E80, 0x1E81: // Ẁ/ẁ (W grave)
		return 0x1A // -> w
	case 0x1E82, 0x1E83: // Ẃ/ẃ (W acute)
		return 0x1A // -> w
	case 0x1E84, 0x1E85: // Ẅ/ẅ (W diaeresis)
		return 0x1A // -> w
	case 0x1EF2, 0x1EF3: // Ỳ/ỳ (Y grave)
		return 0x1C // -> y
	}

	// Esperanto specific
	switch keysym {
	case 0x0108, 0x0109: // Ĉ/ĉ (C circumflex)
		return 0x06 // -> c
	case 0x011C, 0x011D: // Ĝ/ĝ (G circumflex)
		return 0x0A // -> g
	case 0x0124, 0x0125: // Ĥ/ĥ (H circumflex)
		return 0x0B // -> h
	case 0x0134, 0x0135: // Ĵ/ĵ (J circumflex)
		return 0x0D // -> j
	case 0x015C, 0x015D: // Ŝ/ŝ (S circumflex)
		return 0x16 // -> s
	case 0x016C, 0x016D: // Ŭ/ŭ (U breve)
		return 0x18 // -> u
	}

	// Catalan specific
	switch keysym {
	case 0x00B7: // · (middle dot/interpunct) - Catalan L·L
		return 0x37 // -> . (period)
	}

	// Pinyin/Chinese romanization diacritics
	switch keysym {
	case 0x01D5, 0x01D6: // Ǖ/ǖ (U diaeresis macron)
		return 0x18 // -> u
	case 0x01D7, 0x01D8: // Ǘ/ǘ (U diaeresis acute)
		return 0x18 // -> u
	case 0x01D9, 0x01DA: // Ǚ/ǚ (U diaeresis caron)
		return 0x18 // -> u
	case 0x01DB, 0x01DC: // Ǜ/ǜ (U diaeresis grave)
		return 0x18 // -> u
	}

	// Additional commonly typed diacritics from iOS
	switch keysym {
	case 0x00AA: // ª (feminine ordinal)
		return 0x04 // -> a
	case 0x00BA: // º (masculine ordinal)
		return 0x12 // -> o
	}

	// Currency symbols
	switch keysym {
	case 0x00A3:
		return 0x20 // £ (pound) - UK Shift+3
	case 0x20AC:
		return 0x08 // € (euro) - AltGr+E on many layouts
	case 0x00A5:
		return 0x89 // ¥ (yen) - JIS International3 key
	case 0x00A2:
		return 0x06 // ¢ (cent) -> c key (AltGr+c on some layouts)
	}

	// Japanese keyboard specific (JIS layout)
	switch keysym {
	case 0xFF22:
		return 0x8B // Muhenkan (無変換) -> International5
	case 0xFF23:
		return 0x8A // Henkan (変換) -> International4
	case 0xFF27:
		return 0x88 // Hiragana_Katakana -> International2
	case 0xFF24:
		return 0x88 // Romaji -> International2
	case 0xFF26:
		return 0x88 // Eisu_toggle -> International2
	case 0xFF2D:
		return 0x87 // Zenkaku_Hankaku -> International1 (Ro key position)
	}

	// Korean keyboard specific
	switch keysym {
	case 0xFF31:
		return 0x90 // Hangul
	case 0xFF34:
		return 0x91 // Hangul_Hanja
	}

	// Dead keys (for composing accented characters)
	switch keysym {
	case 0xFE50:
		return 0x35 // dead_grave
	case 0xFE51:
		return 0x34 // dead_acute
	case 0xFE52:
		return 0x23 // dead_circumflex -> 6 key
	case 0xFE53:
		return 0x35 // dead_tilde
	case 0xFE54:
		return 0x34 // dead_macron
	case 0xFE55:
		return 0x34 // dead_breve
	case 0xFE56:
		return 0x37 // dead_abovedot
	case 0xFE57:
		return 0x34 // dead_diaeresis (umlaut)
	case 0xFE58:
		return 0x34 // dead_abovering
	case 0xFE59:
		return 0x34 // dead_doubleacute
	case 0xFE5A:
		return 0x36 // dead_caron (háček)
	case 0xFE5B:
		return 0x36 // dead_cedilla
	case 0xFE5C:
		return 0x34 // dead_ogonek
	case 0xFE5D:
		return 0x34 // dead_iota
	case 0xFE5E:
		return 0x34 // dead_voiced_sound
	case 0xFE5F:
		return 0x34 // dead_semivoiced_sound
	case 0xFE60:
		return 0x34 // dead_belowdot
	case 0xFE61:
		return 0x34 // dead_hook
	case 0xFE62:
		return 0x34 // dead_horn
	case 0xFE63:
		return 0x34 // dead_stroke
	case 0xFE64:
		return 0x34 // dead_abovecomma
	case 0xFE65:
		return 0x34 // dead_abovereversedcomma
	case 0xFE66:
		return 0x34 // dead_doublegrave
	case 0xFE67:
		return 0x34 // dead_belowring
	case 0xFE68:
		return 0x34 // dead_belowmacron
	case 0xFE69:
		return 0x34 // dead_belowcircumflex
	case 0xFE6A:
		return 0x34 // dead_belowtilde
	case 0xFE6B:
		return 0x34 // dead_belowbreve
	case 0xFE6C:
		return 0x34 // dead_belowdiaeresis
	case 0xFE6D:
		return 0x34 // dead_invertedbreve
	case 0xFE6E:
		return 0x34 // dead_belowcomma
	case 0xFE6F:
		return 0x34 // dead_currency
	case 0xFE90:
		return 0x35 // dead_lowline
	case 0xFE91:
		return 0x34 // dead_aboveverticalline
	case 0xFE92:
		return 0x34 // dead_belowverticalline
	case 0xFE93:
		return 0x34 // dead_longsolidusoverlay
	}

	// Power key - 0x66 is valid HID code for Power
	if keysym == 0xFF7E {
		return 0x66 // Power (Mac power key)
	}

	return 0 // Unknown keysym
}

// keysymNeedsShift returns true if the keysym represents a character that
// requires the Shift modifier on a US QWERTY keyboard.
// Some VNC clients (e.g., Jump Desktop on iOS) send shifted keysyms
// (like 0x003A for ':') without separate Shift key-down/key-up events.
// The caller uses this to synthesize Shift when not already pressed.
func keysymNeedsShift(keysym uint32) bool {
	// Uppercase letters A-Z
	if keysym >= 0x0041 && keysym <= 0x005A {
		return true
	}

	// Shifted punctuation/symbols on US QWERTY
	switch keysym {
	case 0x0021, // !
		0x0040, // @
		0x0023, // #
		0x0024, // $
		0x0025, // %
		0x005E, // ^
		0x0026, // &
		0x002A, // *
		0x0028, // (
		0x0029, // )
		0x005F, // _
		0x002B, // +
		0x007B, // {
		0x007D, // }
		0x007C, // |
		0x003A, // :
		0x0022, // "
		0x007E, // ~
		0x003C, // <
		0x003E, // >
		0x003F: // ?
		return true
	}
	return false
}
