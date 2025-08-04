package kvm

import "github.com/jetkvm/kvm/internal/audio"

// KVMSessionProvider implements the audio.SessionProvider interface
type KVMSessionProvider struct{}

// IsSessionActive returns whether there's an active session
func (k *KVMSessionProvider) IsSessionActive() bool {
	return currentSession != nil
}

// GetAudioInputManager returns the current session's audio input manager
func (k *KVMSessionProvider) GetAudioInputManager() *audio.AudioInputManager {
	if currentSession == nil {
		return nil
	}
	return currentSession.AudioInputManager
}

// initializeAudioSessionProvider sets up the session provider for the audio package
func initializeAudioSessionProvider() {
	audio.SetSessionProvider(&KVMSessionProvider{})
}
