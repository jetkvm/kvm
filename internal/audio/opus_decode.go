package audio

import (
	"fmt"

	"github.com/pion/opus"
)

// OpusPlaybackDecoder decodes a browser Opus microphone track directly to the
// USB gadget's 48 kHz, 16-bit stereo PCM format. It is owned by one WebRTC
// microphone session and must not be shared between concurrent sessions.
const (
	opusPlaybackRate     = 48000
	opusPlaybackChannels = 2
)

type OpusPlaybackDecoder struct {
	decoder opus.Decoder
	pcm16   []int16
}

func NewOpusPlaybackDecoder() (*OpusPlaybackDecoder, error) {
	decoder, err := opus.NewDecoderWithOutput(opusPlaybackRate, opusPlaybackChannels)
	if err != nil {
		return nil, fmt.Errorf("create Opus decoder: %w", err)
	}
	// The maximum legal Opus packet spans 120 ms. The browser normally
	// transmits 20 ms packets, but allocate enough for all valid durations.
	return &OpusPlaybackDecoder{
		decoder: decoder,
		pcm16:   make([]int16, opusPlaybackRate*120/1000*opusPlaybackChannels),
	}, nil
}

func (d *OpusPlaybackDecoder) Decode(packet []byte) ([]int16, error) {
	if len(packet) == 0 {
		return nil, nil
	}
	frames, err := d.decoder.DecodeToInt16(packet, d.pcm16)
	if err != nil {
		return nil, fmt.Errorf("decode Opus microphone packet: %w", err)
	}
	if frames < 0 || frames*opusPlaybackChannels > len(d.pcm16) {
		return nil, fmt.Errorf("invalid decoded Opus frame count: %d", frames)
	}
	return d.pcm16[:frames*opusPlaybackChannels], nil
}
