package audio

import (
	"sync"
)

var audioMuteState struct {
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
