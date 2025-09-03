//go:build !cgo

package audio

import "errors"

// Stub implementations for linting (no CGO dependencies)

func cgoAudioInit() error {
	return errors.New("audio not available in lint mode")
}

func cgoAudioClose() {
	// No-op
}

func cgoAudioReadEncode(buf []byte) (int, error) {
	return 0, errors.New("audio not available in lint mode")
}

func cgoAudioPlaybackInit() error {
	return errors.New("audio not available in lint mode")
}

func cgoAudioPlaybackClose() {
	// No-op
}

func cgoAudioDecodeWrite(buf []byte) (int, error) {
	return 0, errors.New("audio not available in lint mode")
}

// cgoAudioDecodeWriteWithBuffers is a stub implementation for the optimized decode-write function
func cgoAudioDecodeWriteWithBuffers(opusData []byte, pcmBuffer []byte) (int, error) {
	return 0, errors.New("audio not available in lint mode")
}

// Uppercase aliases for external API compatibility

var (
	CGOAudioInit           = cgoAudioInit
	CGOAudioClose          = cgoAudioClose
	CGOAudioReadEncode     = cgoAudioReadEncode
	CGOAudioPlaybackInit   = cgoAudioPlaybackInit
	CGOAudioPlaybackClose  = cgoAudioPlaybackClose
	CGOAudioDecodeWriteLegacy = cgoAudioDecodeWrite
	CGOAudioDecodeWrite    = cgoAudioDecodeWriteWithBuffers
)
