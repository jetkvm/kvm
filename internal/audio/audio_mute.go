package audio

import (
	"sync"
)

// AudioState holds all audio-related state with a single mutex
type AudioState struct {
	mu              sync.RWMutex
	audioMuted      bool
	microphoneMuted bool
}

var globalAudioState = &AudioState{}

func SetAudioMuted(muted bool) {
	globalAudioState.mu.Lock()
	defer globalAudioState.mu.Unlock()
	globalAudioState.audioMuted = muted
}

func IsAudioMuted() bool {
	globalAudioState.mu.RLock()
	defer globalAudioState.mu.RUnlock()
	return globalAudioState.audioMuted
}

func SetMicrophoneMuted(muted bool) {
	globalAudioState.mu.Lock()
	defer globalAudioState.mu.Unlock()
	globalAudioState.microphoneMuted = muted
}

func IsMicrophoneMuted() bool {
	globalAudioState.mu.RLock()
	defer globalAudioState.mu.RUnlock()
	return globalAudioState.microphoneMuted
}
