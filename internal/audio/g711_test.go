package audio

import "testing"

func TestPCMUToLinearSilence(t *testing.T) {
	for _, encoded := range []byte{0xff, 0x7f} {
		if got := PCMUToLinear(encoded); got != 0 {
			t.Fatalf("PCMUToLinear(%#x) = %d, want 0", encoded, got)
		}
	}
}

func TestPCMURoundTripApproximation(t *testing.T) {
	for _, sample := range []int16{-28000, -12000, -1000, 0, 1000, 12000, 28000} {
		decoded := PCMUToLinear(LinearToPCMU(sample))
		diff := int(decoded) - int(sample)
		if diff < 0 {
			diff = -diff
		}
		if diff > 1200 {
			t.Fatalf("round trip for %d decoded to %d (diff %d)", sample, decoded, diff)
		}
	}
}

func TestPCMUToLinearReferenceExtremes(t *testing.T) {
	for _, tt := range []struct {
		encoded byte
		want    int16
	}{
		{0x00, -32124}, {0x80, 32124}, {0x7f, 0}, {0xff, 0},
	} {
		if got := PCMUToLinear(tt.encoded); got != tt.want {
			t.Errorf("PCMUToLinear(%#x) = %d, want %d", tt.encoded, got, tt.want)
		}
	}
}
