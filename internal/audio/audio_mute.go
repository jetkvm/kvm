package audio

import (
	"sync"

	"github.com/jetkvm/kvm/internal/logging"
)

var audioMuteState struct {
	muted bool
	mu    sync.RWMutex
}

func SetAudioMuted(muted bool) {
	audioMuteState.mu.Lock()
	prev := audioMuteState.muted
	audioMuteState.muted = muted
	logging.GetDefaultLogger().Info().Str("component", "audio").Msgf("SetAudioMuted: prev=%v, new=%v", prev, muted)
	audioMuteState.mu.Unlock()
}

func IsAudioMuted() bool {
	audioMuteState.mu.RLock()
	defer audioMuteState.mu.RUnlock()
	return audioMuteState.muted
}
