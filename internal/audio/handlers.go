package audio

import (
	"context"
	"errors"

	"github.com/coder/websocket"
	"github.com/rs/zerolog"
)

// AudioControlService provides core audio control operations
type AudioControlService struct {
	sessionProvider SessionProvider
	logger          *zerolog.Logger
}

// NewAudioControlService creates a new audio control service
func NewAudioControlService(sessionProvider SessionProvider, logger *zerolog.Logger) *AudioControlService {
	return &AudioControlService{
		sessionProvider: sessionProvider,
		logger:          logger,
	}
}

// MuteAudio sets the audio mute state
func (s *AudioControlService) MuteAudio(muted bool) error {
	SetAudioMuted(muted)
	SetAudioRelayMuted(muted)

	// Broadcast audio mute state change via WebSocket
	broadcaster := GetAudioEventBroadcaster()
	broadcaster.BroadcastAudioDeviceChanged(!muted, "audio_mute_changed")

	return nil
}

// StartMicrophone starts the microphone input
func (s *AudioControlService) StartMicrophone() error {
	if !s.sessionProvider.IsSessionActive() {
		return errors.New("no active session")
	}

	audioInputManager := s.sessionProvider.GetAudioInputManager()
	if audioInputManager == nil {
		return errors.New("audio input manager not available")
	}

	if audioInputManager.IsRunning() {
		s.logger.Info().Msg("microphone already running")
		return nil
	}

	if err := audioInputManager.Start(); err != nil {
		s.logger.Error().Err(err).Msg("failed to start microphone")
		return err
	}

	s.logger.Info().Msg("microphone started successfully")
	return nil
}

// MuteMicrophone sets the microphone mute state
func (s *AudioControlService) MuteMicrophone(muted bool) error {
	// Set microphone mute state using the audio relay
	SetAudioRelayMuted(muted)

	// Broadcast microphone mute state change via WebSocket
	broadcaster := GetAudioEventBroadcaster()
	broadcaster.BroadcastAudioDeviceChanged(!muted, "microphone_mute_changed")

	s.logger.Info().Bool("muted", muted).Msg("microphone mute state updated")
	return nil
}

// ResetMicrophone resets the microphone
func (s *AudioControlService) ResetMicrophone() error {
	if !s.sessionProvider.IsSessionActive() {
		return errors.New("no active session")
	}

	audioInputManager := s.sessionProvider.GetAudioInputManager()
	if audioInputManager == nil {
		return errors.New("audio input manager not available")
	}

	if audioInputManager.IsRunning() {
		audioInputManager.Stop()
		s.logger.Info().Msg("stopped microphone for reset")
	}

	if err := audioInputManager.Start(); err != nil {
		s.logger.Error().Err(err).Msg("failed to restart microphone during reset")
		return err
	}

	s.logger.Info().Msg("microphone reset successfully")
	return nil
}

// GetMicrophoneStatus returns the current microphone status
func (s *AudioControlService) GetMicrophoneStatus() map[string]interface{} {
	if s.sessionProvider == nil {
		return map[string]interface{}{
			"error": "no session provider",
		}
	}

	if !s.sessionProvider.IsSessionActive() {
		return map[string]interface{}{
			"error": "no active session",
		}
	}

	audioInputManager := s.sessionProvider.GetAudioInputManager()
	if audioInputManager == nil {
		return map[string]interface{}{
			"error": "no audio input manager",
		}
	}

	return map[string]interface{}{
		"running": audioInputManager.IsRunning(),
		"ready":   audioInputManager.IsReady(),
	}
}

// SetAudioQuality sets the audio output quality
func (s *AudioControlService) SetAudioQuality(quality AudioQuality) {
	SetAudioQuality(quality)
}

// SetMicrophoneQuality sets the microphone input quality
func (s *AudioControlService) SetMicrophoneQuality(quality AudioQuality) {
	SetMicrophoneQuality(quality)
}

// GetAudioQualityPresets returns available audio quality presets
func (s *AudioControlService) GetAudioQualityPresets() map[AudioQuality]AudioConfig {
	return GetAudioQualityPresets()
}

// GetMicrophoneQualityPresets returns available microphone quality presets
func (s *AudioControlService) GetMicrophoneQualityPresets() map[AudioQuality]AudioConfig {
	return GetMicrophoneQualityPresets()
}

// SubscribeToAudioEvents subscribes to audio events via WebSocket
func (s *AudioControlService) SubscribeToAudioEvents(connectionID string, wsCon *websocket.Conn, runCtx context.Context, logger *zerolog.Logger) {
	logger.Info().Msg("client subscribing to audio events")
	broadcaster := GetAudioEventBroadcaster()
	broadcaster.Subscribe(connectionID, wsCon, runCtx, logger)
}

// UnsubscribeFromAudioEvents unsubscribes from audio events
func (s *AudioControlService) UnsubscribeFromAudioEvents(connectionID string, logger *zerolog.Logger) {
	logger.Info().Str("connection_id", connectionID).Msg("client unsubscribing from audio events")
	broadcaster := GetAudioEventBroadcaster()
	broadcaster.Unsubscribe(connectionID)
}
