package audio

import (
	"os"
	"strings"
)

// isAudioServerProcess detects if we're running as the audio server subprocess
func isAudioServerProcess() bool {
	for _, arg := range os.Args {
		if strings.Contains(arg, "--audio-server") {
			return true
		}
	}
	return false
}

// StartAudioStreaming launches the audio stream.
// In audio server subprocess: uses CGO-based audio streaming
// In main process: this should not be called (use StartAudioRelay instead)
func StartAudioStreaming(send func([]byte)) error {
	if isAudioServerProcess() {
		// Audio server subprocess: use CGO audio processing
		return StartAudioOutputStreaming(send)
	} else {
		// Main process: should use relay system instead
		// This is kept for backward compatibility but not recommended
		return StartAudioOutputStreaming(send)
	}
}

// StopAudioStreaming stops the audio stream.
func StopAudioStreaming() {
	if isAudioServerProcess() {
		// Audio server subprocess: stop CGO audio processing
		StopAudioOutputStreaming()
	} else {
		// Main process: stop relay if running
		StopAudioRelay()
	}
}

// StartNonBlockingAudioStreaming is an alias for backward compatibility
func StartNonBlockingAudioStreaming(send func([]byte)) error {
	return StartAudioOutputStreaming(send)
}

// StopNonBlockingAudioStreaming is an alias for backward compatibility
func StopNonBlockingAudioStreaming() {
	StopAudioOutputStreaming()
}
