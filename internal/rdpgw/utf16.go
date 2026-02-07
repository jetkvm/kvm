package rdpgw

import (
	"encoding/binary"
	"unicode/utf16"
)

// decodeUTF16LE decodes a UTF-16 little-endian byte slice to a Go string.
// Trailing null characters are stripped.
func decodeUTF16LE(b []byte) string {
	if len(b) < 2 {
		return ""
	}
	// Pair up bytes into uint16 code units
	n := len(b) / 2
	u16 := make([]uint16, n)
	for i := 0; i < n; i++ {
		u16[i] = binary.LittleEndian.Uint16(b[i*2:])
	}
	// Strip trailing nulls
	for len(u16) > 0 && u16[len(u16)-1] == 0 {
		u16 = u16[:len(u16)-1]
	}
	return string(utf16.Decode(u16))
}

// encodeUTF16LE encodes a Go string as UTF-16 little-endian bytes.
// A null terminator is NOT appended; callers should add one if needed.
func encodeUTF16LE(s string) []byte {
	u16 := utf16.Encode([]rune(s))
	b := make([]byte, len(u16)*2)
	for i, v := range u16 {
		binary.LittleEndian.PutUint16(b[i*2:], v)
	}
	return b
}
