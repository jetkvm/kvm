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
	audioMuteState.muted = muted
	audioMuteState.mu.Unlock()
}

func IsAudioMuted() bool {
	audioMuteState.mu.RLock()
	defer audioMuteState.mu.RUnlock()
	return audioMuteState.muted
}

func SetMicrophoneMuted(muted bool) {
	microphoneMuteState.mu.Lock()
	microphoneMuteState.muted = muted
	microphoneMuteState.mu.Unlock()
}

func IsMicrophoneMuted() bool {
	microphoneMuteState.mu.RLock()
	defer microphoneMuteState.mu.RUnlock()
	return microphoneMuteState.muted
}
