package osservices

import (
	"crypto/sha256"
	"fmt"
	"testing"
)

// Ported from test_wire_helpers in test_os_services_session.c, with the
// H.265 rules of the video.frame section.

func TestUTF8Valid(t *testing.T) {
	cases := []struct {
		text  string
		valid bool
	}{
		{"", true},
		{"\xf0\x9f\x98\x80", true},
		{"\xed\xa0\x80", false},     // a surrogate
		{"\xe0\x80\xaf", false},     // overlong
		{"\xf4\x90\x80\x80", false}, // past U+10FFFF
		{"\xc3", false},             // cut short
		{"\xc0\x80", false},         // overlong NUL
	}
	for _, c := range cases {
		if UTF8Valid([]byte(c.text)) != c.valid {
			t.Errorf("UTF8Valid(%q) != %v", c.text, c.valid)
		}
	}
}

func TestFormatFingerprint(t *testing.T) {
	key := [32]byte{1, 2, 3}
	digest := sha256.Sum256(key[:])
	want := fmt.Sprintf("%02X:%02X:%02X:%02X:%02X:%02X:%02X:%02X",
		digest[0], digest[1], digest[2], digest[3], digest[4], digest[5], digest[6], digest[7])
	if got := FormatFingerprint(digest[:]); got != want {
		t.Fatalf("FormatFingerprint: %q, want %q", got, want)
	}
}

func TestAnnexBH264(t *testing.T) {
	// SPS, PPS, IDR with 3- and 4-byte start codes.
	idr := []byte{0, 0, 0, 1, 0x67, 1, 2, 0, 0, 1, 0x68, 3, 0, 0, 1, 0x65, 0x88, 0x84}
	if !AnnexBIsKeyframe(CodecH264, idr) || !AnnexBHasParameterSets(CodecH264, idr) {
		t.Fatalf("H.264 IDR with SPS and PPS")
	}
	delta := []byte{0, 0, 0, 1, 0x09, 0xf0, 0, 0, 1, 0x41, 0x9a}
	if AnnexBIsKeyframe(CodecH264, delta) || AnnexBHasParameterSets(CodecH264, delta) {
		t.Fatalf("H.264 delta")
	}
	// The first VCL NAL unit decides: an IDR after a non-IDR slice is no
	// keyframe.
	if AnnexBIsKeyframe(CodecH264, []byte{0, 0, 1, 0x41, 1, 0, 0, 1, 0x65, 1}) {
		t.Fatalf("a non-IDR slice first")
	}
	// SPS alone, or PPS alone, are not the parameter sets.
	if AnnexBHasParameterSets(CodecH264, []byte{0, 0, 1, 0x67, 1, 0, 0, 1, 0x65, 1}) ||
		AnnexBHasParameterSets(CodecH264, []byte{0, 0, 1, 0x68, 1, 0, 0, 1, 0x65, 1}) {
		t.Fatalf("one parameter set counted as both")
	}
}

// h265NAL is a two-byte H.265 NAL header of the type, after a start code.
func h265NAL(nalType byte, body ...byte) []byte {
	return append([]byte{0, 0, 0, 1, nalType << 1, 1}, body...)
}

func TestAnnexBH265(t *testing.T) {
	// VPS 32, SPS 33, PPS 34, IDR_W_RADL 19.
	hevc := []byte{0, 0, 0, 1, 0x40, 1, 0, 0, 1, 0x42, 1, 0, 0, 1, 0x44, 1, 0, 0, 1, 0x26, 1, 0xaf}
	if !AnnexBIsKeyframe(CodecH265, hevc) || !AnnexBHasParameterSets(CodecH265, hevc) {
		t.Fatalf("H.265 IDR with VPS, SPS and PPS")
	}
	if trail := []byte{0, 0, 1, 0x02, 1, 0xd0}; AnnexBIsKeyframe(CodecH265, trail) {
		t.Fatalf("TRAIL_R taken for a keyframe")
	}
	// Keyframes are IDR_W_RADL, IDR_N_LP and CRA; other VCL types (0 to 31)
	// are not.
	for nalType := byte(0); nalType < 64; nalType++ {
		unit := h265NAL(nalType, 0xaf)
		key := nalType >= 19 && nalType <= 21
		if AnnexBIsKeyframe(CodecH265, unit) != key {
			t.Errorf("H.265 type %d: keyframe %v", nalType, !key)
		}
	}
	// A non-VCL unit before the slice does not decide; type 31 is VCL.
	if AnnexBIsKeyframe(CodecH265, cat(h265NAL(35), h265NAL(31), h265NAL(19))) {
		t.Fatalf("type 31 is a VCL NAL unit that is no keyframe")
	}
	if !AnnexBIsKeyframe(CodecH265, cat(h265NAL(35), h265NAL(39), h265NAL(21))) {
		t.Fatalf("AUD and SEI before a CRA")
	}
	// H.265 needs all three parameter sets.
	if AnnexBHasParameterSets(CodecH265, cat(h265NAL(33), h265NAL(34), h265NAL(19))) {
		t.Fatalf("SPS and PPS without VPS")
	}
	if AnnexBHasParameterSets(CodecH265, cat(h265NAL(32), h265NAL(34), h265NAL(19))) {
		t.Fatalf("VPS and PPS without SPS")
	}
	// H.264 rules do not apply to an H.265 stream, nor the other way round:
	// an H.264 IDR with SPS and PPS has no H.265 keyframe or parameter sets.
	h264 := []byte{0, 0, 0, 1, 0x67, 1, 0, 0, 1, 0x68, 1, 0, 0, 1, 0x65, 1}
	if AnnexBIsKeyframe(CodecH265, h264) || AnnexBHasParameterSets(CodecH265, h264) {
		t.Fatalf("an H.264 unit read as H.265")
	}
	if AnnexBIsKeyframe(CodecH264, hevc) {
		t.Fatalf("an H.265 unit read as an H.264 keyframe")
	}
}

func TestExtractParameterSets(t *testing.T) {
	// Each set comes out behind a 4-byte start code, in the order of the
	// unit, and only those ahead of the first slice.
	unit := cat([]byte{0, 0, 1, 0x09, 0xf0}, []byte{0, 0, 1, 0x67, 0x42, 0x00}, []byte{0, 0, 0, 1, 0x68, 0xce},
		[]byte{0, 0, 1, 0x65, 0x88}, []byte{0, 0, 1, 0x67, 0x99})
	sets, complete := extractParameterSets(CodecH264, unit)
	want := []byte{0, 0, 0, 1, 0x67, 0x42, 0x00, 0, 0, 0, 1, 0x68, 0xce}
	if !complete || string(sets) != string(want) {
		t.Fatalf("sets % x complete %v", sets, complete)
	}
	// Without a slice after them, the last set may be cut off: incomplete.
	if _, complete := extractParameterSets(CodecH264, unit[:13]); complete {
		t.Fatalf("sets without a slice are complete")
	}
	hevc := cat(h265NAL(32, 0x0c), h265NAL(33, 0x01), h265NAL(34, 0xc1), h265NAL(19, 0xaf))
	sets, complete = extractParameterSets(CodecH265, hevc)
	if !complete || string(sets) != string(hevc[:21]) {
		t.Fatalf("H.265 sets % x complete %v", sets, complete)
	}
	if _, complete := extractParameterSets(CodecH265, hevc[8:]); complete {
		t.Fatalf("H.265 sets without VPS are complete")
	}
}
