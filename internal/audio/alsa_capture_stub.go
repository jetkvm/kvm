//go:build !linux || !cgo

package audio

import "fmt"

type ALSACapture struct{}

func OpenALSACapture(device string) (*ALSACapture, error) {
	return nil, fmt.Errorf("ALSA audio capture is not available for this build: %s", device)
}

func (*ALSACapture) ReadEncoded(Codec) ([]byte, error) { return nil, ErrNoAudioData }

func (*ALSACapture) Close() error { return nil }
