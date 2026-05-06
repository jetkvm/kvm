package rfb

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
	got := SplitAnnexB(frame)
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
	got := SplitAnnexB(frame)
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
	got := SplitAnnexB(frame)
	if len(got) != 3 {
		t.Fatalf("got %d NALs, want 3", len(got))
	}
}

func TestSplitAnnexBNoStartCode(t *testing.T) {
	frame := []byte{0x67, 0x42, 0x00}
	got := SplitAnnexB(frame)
	if len(got) != 1 || !bytes.Equal(got[0], frame) {
		t.Errorf("expected single NAL with whole frame, got %v", got)
	}
}

func TestSplitAnnexBEmpty(t *testing.T) {
	if got := SplitAnnexB(nil); len(got) != 0 {
		t.Errorf("expected no NALs for nil, got %d", len(got))
	}
}
