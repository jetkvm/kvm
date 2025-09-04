package audio

// SessionProvider interface abstracts session management for audio events
type SessionProvider interface {
	IsSessionActive() bool
	GetAudioInputManager() *AudioInputManager
}

// DefaultSessionProvider is a no-op implementation
type DefaultSessionProvider struct{}

func (d *DefaultSessionProvider) IsSessionActive() bool {
	return false
}

func (d *DefaultSessionProvider) GetAudioInputManager() *AudioInputManager {
	return nil
}

var sessionProvider SessionProvider = &DefaultSessionProvider{}

// SetSessionProvider allows the main package to inject session management
func SetSessionProvider(provider SessionProvider) {
	sessionProvider = provider
}

// GetSessionProvider returns the current session provider
func GetSessionProvider() SessionProvider {
	return sessionProvider
}
