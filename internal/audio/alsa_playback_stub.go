//go:build !linux || !cgo

package audio

import "fmt"

type ALSAPlayback struct{}

func OpenALSAPlayback(device string) (*ALSAPlayback, error) {
	return nil, fmt.Errorf("ALSA audio playback is not available for this build: %s", device)
}

func (*ALSAPlayback) WritePCMU([]byte) error { return ErrNoAudioData }

func (*ALSAPlayback) WritePCM16([]int16) error { return ErrNoAudioData }

func (*ALSAPlayback) Close() error { return nil }
