package rfb

import "encoding/binary"

// PixelFormat describes how a client wants pixels packed in Raw
// rectangles (RFC 6143 §7.4). The default constructor returns the
// 32-bit BGRA layout we always use for our Raw fallback.
type PixelFormat struct {
	BitsPerPixel uint8
	Depth        uint8
	BigEndian    bool
	TrueColour   bool
	RedMax       uint16
	GreenMax     uint16
	BlueMax      uint16
	RedShift     uint8
	GreenShift   uint8
	BlueShift    uint8
}

// DefaultPixelFormat returns 32-bit BGRA, little-endian, true-colour —
// the format used for the placeholder/fallback frame.
func DefaultPixelFormat() PixelFormat {
	return PixelFormat{
		BitsPerPixel: 32,
		Depth:        24,
		BigEndian:    false,
		TrueColour:   true,
		RedMax:       255,
		GreenMax:     255,
		BlueMax:      255,
		RedShift:     16,
		GreenShift:   8,
		BlueShift:    0,
	}
}

// MarshalPixelFormat writes the 16-byte on-the-wire pixel format. The
// final 3 bytes are padding per RFC 6143.
func MarshalPixelFormat(p PixelFormat, buf []byte) {
	_ = buf[15]
	buf[0] = p.BitsPerPixel
	buf[1] = p.Depth
	buf[2] = boolToByte(p.BigEndian)
	buf[3] = boolToByte(p.TrueColour)
	binary.BigEndian.PutUint16(buf[4:6], p.RedMax)
	binary.BigEndian.PutUint16(buf[6:8], p.GreenMax)
	binary.BigEndian.PutUint16(buf[8:10], p.BlueMax)
	buf[10] = p.RedShift
	buf[11] = p.GreenShift
	buf[12] = p.BlueShift
	buf[13] = 0
	buf[14] = 0
	buf[15] = 0
}

// UnmarshalPixelFormat parses the 16-byte on-the-wire pixel format.
func UnmarshalPixelFormat(buf []byte) PixelFormat {
	_ = buf[15]
	return PixelFormat{
		BitsPerPixel: buf[0],
		Depth:        buf[1],
		BigEndian:    buf[2] != 0,
		TrueColour:   buf[3] != 0,
		RedMax:       binary.BigEndian.Uint16(buf[4:6]),
		GreenMax:     binary.BigEndian.Uint16(buf[6:8]),
		BlueMax:      binary.BigEndian.Uint16(buf[8:10]),
		RedShift:     buf[10],
		GreenShift:   buf[11],
		BlueShift:    buf[12],
	}
}

func boolToByte(b bool) byte {
	if b {
		return 1
	}
	return 0
}
