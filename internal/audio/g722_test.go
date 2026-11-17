package audio

import (
	"math"
	"testing"
)

func TestG722EncoderFrameSize(t *testing.T) {
	encoder := NewG722Encoder()
	pcm := make([]int16, G722InputFrameSize)
	for i := range pcm {
		pcm[i] = int16(math.Sin(float64(i)*2*math.Pi*997/G722InputSampleRate) * 12000)
	}

	out := make([]byte, G722EncodedFrameSize)
	n := encoder.Encode(out, pcm)
	if n != G722EncodedFrameSize {
		t.Fatalf("encoded frame size = %d, want %d", n, G722EncodedFrameSize)
	}
}

func TestG722EncoderStateful(t *testing.T) {
	encoder := NewG722Encoder()
	pcm := make([]int16, G722InputFrameSize)
	for i := range pcm {
		pcm[i] = int16(math.Sin(float64(i)*2*math.Pi*440/G722InputSampleRate) * 8000)
	}

	first := make([]byte, G722EncodedFrameSize)
	second := make([]byte, G722EncodedFrameSize)
	encoder.Encode(first, pcm)
	encoder.Encode(second, pcm)
	if string(first) == string(second) {
		t.Fatal("consecutive frames unexpectedly matched; encoder predictor state did not change")
	}
}
