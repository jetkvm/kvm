package audio

// StartAudioStreaming launches the in-process audio stream and delivers Opus frames to the provided callback.
func StartAudioStreaming(send func([]byte)) error {
	return StartCGOAudioStream(send)
}

// StopAudioStreaming stops the in-process audio stream.
func StopAudioStreaming() {
	StopCGOAudioStream()
}
