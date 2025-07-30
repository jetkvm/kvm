//go:build !linux || !arm
// +build !linux !arm

package audio

// Dummy implementations for non-linux/arm builds
func StartCGOAudioStream(send func([]byte)) error {
	return nil
}

func StopCGOAudioStream() {}
