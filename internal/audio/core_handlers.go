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

// MuteAudio sets the audio mute state by controlling the audio output subprocess
func (s *AudioControlService) MuteAudio(muted bool) error {
	if muted {
		// Mute: Stop audio output subprocess and relay
		supervisor := GetAudioOutputSupervisor()
		if supervisor != nil {
			supervisor.Stop()
			s.logger.Info().Msg("audio output supervisor stopped")
		}
		StopAudioRelay()
		SetAudioMuted(true)
		s.logger.Info().Msg("audio output muted (subprocess and relay stopped)")
	} else {
		// Unmute: Start audio output subprocess and relay
		if !s.sessionProvider.IsSessionActive() {
			return errors.New("no active session for audio unmute")
		}

		supervisor := GetAudioOutputSupervisor()
		if supervisor != nil {
			err := supervisor.Start()
			if err != nil {
				s.logger.Error().Err(err).Msg("failed to start audio output supervisor during unmute")
				return err
			}
			s.logger.Info().Msg("audio output supervisor started")
		}

		// Start audio relay
		err := StartAudioRelay(nil)
		if err != nil {
			s.logger.Error().Err(err).Msg("failed to start audio relay during unmute")
			return err
		}

		// Connect the relay to the current WebRTC session's audio track
		// This is needed because UpdateAudioRelayTrack is normally only called during session creation
		if err := connectRelayToCurrentSession(); err != nil {
			s.logger.Warn().Err(err).Msg("failed to connect relay to current session, audio may not work")
		}
		SetAudioMuted(false)
		s.logger.Info().Msg("audio output unmuted (subprocess and relay started)")
	}

	// Broadcast audio mute state change via WebSocket
	broadcaster := GetAudioEventBroadcaster()
	broadcaster.BroadcastAudioMuteChanged(muted)

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

// StopMicrophone stops the microphone input
func (s *AudioControlService) StopMicrophone() error {
	if !s.sessionProvider.IsSessionActive() {
		return errors.New("no active session")
	}

	audioInputManager := s.sessionProvider.GetAudioInputManager()
	if audioInputManager == nil {
		return errors.New("audio input manager not available")
	}

	if !audioInputManager.IsRunning() {
		s.logger.Info().Msg("microphone already stopped")
		return nil
	}

	audioInputManager.Stop()
	s.logger.Info().Msg("microphone stopped successfully")
	return nil
}

// MuteMicrophone sets the microphone mute state by controlling the microphone process
func (s *AudioControlService) MuteMicrophone(muted bool) error {
	if muted {
		// Mute: Stop microphone process
		err := s.StopMicrophone()
		if err != nil {
			s.logger.Error().Err(err).Msg("failed to stop microphone during mute")
			return err
		}
		s.logger.Info().Msg("microphone muted (process stopped)")
	} else {
		// Unmute: Start microphone process
		err := s.StartMicrophone()
		if err != nil {
			s.logger.Error().Err(err).Msg("failed to start microphone during unmute")
			return err
		}
		s.logger.Info().Msg("microphone unmuted (process started)")
	}

	// Broadcast microphone mute state change via WebSocket
	broadcaster := GetAudioEventBroadcaster()
	broadcaster.BroadcastAudioDeviceChanged(!muted, "microphone_mute_changed")

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

// GetAudioStatus returns the current audio output status
func (s *AudioControlService) GetAudioStatus() map[string]interface{} {
	return map[string]interface{}{
		"muted": IsAudioMuted(),
	}
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

// GetCurrentAudioQuality returns the current audio quality configuration
func (s *AudioControlService) GetCurrentAudioQuality() AudioConfig {
	return GetAudioConfig()
}

// GetCurrentMicrophoneQuality returns the current microphone quality configuration
func (s *AudioControlService) GetCurrentMicrophoneQuality() AudioConfig {
	return GetMicrophoneConfig()
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

// IsAudioOutputActive returns whether the audio output subprocess is running
func (s *AudioControlService) IsAudioOutputActive() bool {
	return !IsAudioMuted() && IsAudioRelayRunning()
}

// IsMicrophoneActive returns whether the microphone subprocess is running
func (s *AudioControlService) IsMicrophoneActive() bool {
	if !s.sessionProvider.IsSessionActive() {
		return false
	}

	audioInputManager := s.sessionProvider.GetAudioInputManager()
	if audioInputManager == nil {
		return false
	}

	return audioInputManager.IsRunning()
}
