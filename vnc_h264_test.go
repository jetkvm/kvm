package kvm

import (
	"bytes"
	"testing"
)

func TestSplitAnnexB4ByteStartCode(t *testing.T) {
	// Two NALs separated by 4-byte start codes.
	frame := []byte{
		0x00, 0x00, 0x00, 0x01, 0x67, 0x42, 0x00, 0x1E, // SPS (type 7)
		0x00, 0x00, 0x00, 0x01, 0x68, 0xCE, 0x3C, 0x80, // PPS (type 8)
	}
	got := splitAnnexB(frame)
	if len(got) != 2 {
		t.Fatalf("got %d NALs, want 2", len(got))
	}
	if !bytes.Equal(got[0], []byte{0x67, 0x42, 0x00, 0x1E}) {
		t.Errorf("NAL 0: %x", got[0])
	}
	if !bytes.Equal(got[1], []byte{0x68, 0xCE, 0x3C, 0x80}) {
		t.Errorf("NAL 1: %x", got[1])
	}
	// NAL 0 type
	if got[0][0]&0x1F != 7 {
		t.Errorf("NAL 0 type: %d", got[0][0]&0x1F)
	}
	if got[1][0]&0x1F != 8 {
		t.Errorf("NAL 1 type: %d", got[1][0]&0x1F)
	}
}

func TestSplitAnnexB3ByteStartCode(t *testing.T) {
	frame := []byte{
		0x00, 0x00, 0x01, 0x65, 0xAA, 0xBB, // 3-byte start code, slice
	}
	got := splitAnnexB(frame)
	if len(got) != 1 {
		t.Fatalf("got %d NALs, want 1", len(got))
	}
	if !bytes.Equal(got[0], []byte{0x65, 0xAA, 0xBB}) {
		t.Errorf("NAL: %x", got[0])
	}
}

func TestSplitAnnexBMixedStartCodes(t *testing.T) {
	frame := []byte{
		0x00, 0x00, 0x00, 0x01, 0x67, // 4-byte start
		0x00, 0x00, 0x01, 0x68, // 3-byte start
		0x00, 0x00, 0x00, 0x01, 0x65, 0xAA,
	}
	got := splitAnnexB(frame)
	if len(got) != 3 {
		t.Fatalf("got %d NALs, want 3", len(got))
	}
}

func TestSplitAnnexBNoStartCode(t *testing.T) {
	frame := []byte{0x67, 0x42, 0x00}
	got := splitAnnexB(frame)
	if len(got) != 1 || !bytes.Equal(got[0], frame) {
		t.Errorf("expected single NAL with whole frame, got %v", got)
	}
}

func TestSplitAnnexBEmpty(t *testing.T) {
	if got := splitAnnexB(nil); len(got) != 0 {
		t.Errorf("expected no NALs for nil, got %d", len(got))
	}
}

func TestScaleCoord(t *testing.T) {
	cases := []struct {
		src, srcMax, dstMax, want int
	}{
		{0, 1920, 32767, 0},
		{1919, 1920, 32767, 32767}, // (1920-1) → exactly dstMax
		{960, 1920, 32767, 32767 * 960 / 1919},
		{0, 1, 32767, 0},     // edge: srcMax=1
		{-5, 1920, 32767, 0}, // negative clamps
	}
	for _, c := range cases {
		got := scaleCoord(c.src, c.srcMax, c.dstMax)
		if got != c.want {
			t.Errorf("scaleCoord(%d, %d, %d) = %d, want %d", c.src, c.srcMax, c.dstMax, got, c.want)
		}
	}
}

func TestRising(t *testing.T) {
	if !rising(0b0000, 0b0001, 0b0001) {
		t.Errorf("expected rising edge")
	}
	if rising(0b0001, 0b0000, 0b0001) {
		t.Errorf("falling edge reported as rising")
	}
	if rising(0b0001, 0b0001, 0b0001) {
		t.Errorf("steady-state reported as rising")
	}
}
