package audio

import (
	"sync"
)

var audioMuteState struct {
	muted bool
	mu    sync.RWMutex
}

var microphoneMuteState struct {
	muted bool
	mu    sync.RWMutex
}

func SetAudioMuted(muted bool) {
	audioMuteState.mu.Lock()
	defer audioMuteState.mu.Unlock()
	audioMuteState.muted = muted
}

func IsAudioMuted() bool {
	audioMuteState.mu.RLock()
	defer audioMuteState.mu.RUnlock()
	return audioMuteState.muted
}

func SetMicrophoneMuted(muted bool) {
	microphoneMuteState.mu.Lock()
	defer microphoneMuteState.mu.Unlock()
	microphoneMuteState.muted = muted
}

func IsMicrophoneMuted() bool {
	microphoneMuteState.mu.RLock()
	defer microphoneMuteState.mu.RUnlock()
	return microphoneMuteState.muted
}
