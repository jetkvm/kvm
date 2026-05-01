package rfb

import (
	"bytes"
	"testing"
)

func TestDefaultPixelFormatRoundTrip(t *testing.T) {
	in := DefaultPixelFormat()
	var buf [16]byte
	MarshalPixelFormat(in, buf[:])
	out := UnmarshalPixelFormat(buf[:])
	if in != out {
		t.Fatalf("round-trip mismatch:\n in: %+v\nout: %+v", in, out)
	}
}

func TestMarshalPixelFormatLayout(t *testing.T) {
	pf := PixelFormat{
		BitsPerPixel: 32, Depth: 24,
		BigEndian: true, TrueColour: true,
		RedMax: 255, GreenMax: 255, BlueMax: 255,
		RedShift: 16, GreenShift: 8, BlueShift: 0,
	}
	var buf [16]byte
	MarshalPixelFormat(pf, buf[:])

	// bytes 0..1: bpp, depth
	if buf[0] != 32 || buf[1] != 24 {
		t.Errorf("bpp/depth: got %d/%d", buf[0], buf[1])
	}
	// bytes 2..3: big-endian-flag, true-colour-flag
	if buf[2] != 1 || buf[3] != 1 {
		t.Errorf("flags: got be=%d tc=%d", buf[2], buf[3])
	}
	// bytes 4..9: red/green/blue max (BE u16)
	wantMax := []byte{0, 255, 0, 255, 0, 255}
	if !bytes.Equal(buf[4:10], wantMax) {
		t.Errorf("max: got %v, want %v", buf[4:10], wantMax)
	}
	// bytes 10..12: shifts
	if buf[10] != 16 || buf[11] != 8 || buf[12] != 0 {
		t.Errorf("shifts: got %d/%d/%d", buf[10], buf[11], buf[12])
	}
	// bytes 13..15: padding zero
	if buf[13] != 0 || buf[14] != 0 || buf[15] != 0 {
		t.Errorf("padding non-zero: %v", buf[13:16])
	}
}

func TestUnmarshalPixelFormatBigEndianFlag(t *testing.T) {
	// Construct a buffer with BigEndian=1 and verify the round-trip.
	pf := DefaultPixelFormat()
	pf.BigEndian = true
	var buf [16]byte
	MarshalPixelFormat(pf, buf[:])
	got := UnmarshalPixelFormat(buf[:])
	if !got.BigEndian {
		t.Fatalf("BigEndian flag lost")
	}
}
