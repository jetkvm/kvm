package audio

// StartAudioStreaming launches the in-process audio stream and delivers Opus frames to the provided callback.
// This is now a wrapper around the non-blocking audio implementation for backward compatibility.
func StartAudioStreaming(send func([]byte)) error {
	return StartNonBlockingAudioStreaming(send)
}

// StopAudioStreaming stops the in-process audio stream.
// This is now a wrapper around the non-blocking audio implementation for backward compatibility.
func StopAudioStreaming() {
	StopNonBlockingAudioStreaming()
}
