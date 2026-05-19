//go:build !linux || !cgo

package audio

import "fmt"

func OpenALSACapture(device string) (*unavailableCapture, error) {
	return nil, fmt.Errorf("ALSA audio capture is not available for this build: %s", device)
}
