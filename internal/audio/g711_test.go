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

func TestApplyPCM16Gain(t *testing.T) {
	tests := []struct {
		name   string
		sample int16
		gain   int
		want   int16
	}{
		{name: "positive", sample: 1000, gain: 6, want: 6000},
		{name: "negative", sample: -1000, gain: 6, want: -6000},
		{name: "positive clip", sample: 12000, gain: 6, want: 32767},
		{name: "negative clip", sample: -12000, gain: 6, want: -32768},
		{name: "unity for invalid gain", sample: 1234, gain: 0, want: 1234},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ApplyPCM16Gain(tt.sample, tt.gain); got != tt.want {
				t.Fatalf("ApplyPCM16Gain(%d, %d) = %d, want %d", tt.sample, tt.gain, got, tt.want)
			}
		})
	}
}
