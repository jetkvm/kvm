package kvm

import "github.com/jetkvm/kvm/internal/audio"

// SessionProviderImpl implements the audio.SessionProvider interface
type SessionProviderImpl struct{}

// NewSessionProvider creates a new session provider
func NewSessionProvider() *SessionProviderImpl {
	return &SessionProviderImpl{}
}

// IsSessionActive returns whether there's an active session
func (sp *SessionProviderImpl) IsSessionActive() bool {
	return currentSession != nil
}

// GetAudioInputManager returns the current session's audio input manager
func (sp *SessionProviderImpl) GetAudioInputManager() *audio.AudioInputManager {
	if currentSession == nil {
		return nil
	}
	return currentSession.AudioInputManager
}
