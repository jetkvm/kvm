package kvm

import (
	"context"

	"github.com/coder/websocket"
	"github.com/jetkvm/kvm/internal/audio"
	"github.com/pion/webrtc/v4"
	"github.com/rs/zerolog"
)

var audioControlService *audio.AudioControlService

func ensureAudioControlService() *audio.AudioControlService {
	if audioControlService == nil {
		sessionProvider := &SessionProviderImpl{}
		audioControlService = audio.NewAudioControlService(sessionProvider, logger)

		// Set up callback for audio relay to get current session's audio track
		audio.SetCurrentSessionCallback(func() audio.AudioTrackWriter {
			return GetCurrentSessionAudioTrack()
		})

		// Set up callback for audio relay to replace WebRTC audio track
		audio.SetTrackReplacementCallback(func(newTrack audio.AudioTrackWriter) error {
			if track, ok := newTrack.(*webrtc.TrackLocalStaticSample); ok {
				return ReplaceCurrentSessionAudioTrack(track)
			}
			return nil
		})

		// Set up RPC callback functions for the audio package
		audio.SetRPCCallbacks(
			func() *audio.AudioControlService { return audioControlService },
			func() audio.AudioConfig { return audioControlService.GetCurrentAudioQuality() },
			func(quality audio.AudioQuality) error {
				audioControlService.SetAudioQuality(quality)
				return nil
			},
		)
	}
	return audioControlService
}

// --- Global Convenience Functions for Audio Control ---

// MuteAudioOutput is a global helper to mute audio output
func MuteAudioOutput() error {
	return ensureAudioControlService().MuteAudio(true)
}

// UnmuteAudioOutput is a global helper to unmute audio output
func UnmuteAudioOutput() error {
	return ensureAudioControlService().MuteAudio(false)
}

// StopMicrophone is a global helper to stop microphone subprocess
func StopMicrophone() error {
	return ensureAudioControlService().StopMicrophone()
}

// StartMicrophone is a global helper to start microphone subprocess
func StartMicrophone() error {
	return ensureAudioControlService().StartMicrophone()
}

// IsAudioOutputActive is a global helper to check if audio output subprocess is running
func IsAudioOutputActive() bool {
	return ensureAudioControlService().IsAudioOutputActive()
}

// IsMicrophoneActive is a global helper to check if microphone subprocess is running
func IsMicrophoneActive() bool {
	return ensureAudioControlService().IsMicrophoneActive()
}

// ResetMicrophone is a global helper to reset the microphone
func ResetMicrophone() error {
	return ensureAudioControlService().ResetMicrophone()
}

// GetCurrentSessionAudioTrack returns the current session's audio track for audio relay
func GetCurrentSessionAudioTrack() *webrtc.TrackLocalStaticSample {
	if currentSession != nil {
		return currentSession.AudioTrack
	}
	return nil
}

// ConnectRelayToCurrentSession connects the audio relay to the current WebRTC session
func ConnectRelayToCurrentSession() error {
	if currentTrack := GetCurrentSessionAudioTrack(); currentTrack != nil {
		err := audio.UpdateAudioRelayTrack(currentTrack)
		if err != nil {
			logger.Error().Err(err).Msg("failed to connect current session's audio track to relay")
			return err
		}
		logger.Info().Msg("connected current session's audio track to relay")
		return nil
	}
	logger.Warn().Msg("no current session audio track found")
	return nil
}

// ReplaceCurrentSessionAudioTrack replaces the audio track in the current WebRTC session
func ReplaceCurrentSessionAudioTrack(newTrack *webrtc.TrackLocalStaticSample) error {
	if currentSession == nil {
		return nil // No session to update
	}

	err := currentSession.ReplaceAudioTrack(newTrack)
	if err != nil {
		logger.Error().Err(err).Msg("failed to replace audio track in current session")
		return err
	}

	logger.Info().Msg("successfully replaced audio track in current session")
	return nil
}

// SetAudioQuality is a global helper to set audio output quality
func SetAudioQuality(quality audio.AudioQuality) error {
	ensureAudioControlService()
	audioControlService.SetAudioQuality(quality)
	return nil
}

// GetAudioQualityPresets is a global helper to get available audio quality presets
func GetAudioQualityPresets() map[audio.AudioQuality]audio.AudioConfig {
	ensureAudioControlService()
	return audioControlService.GetAudioQualityPresets()
}

// GetCurrentAudioQuality is a global helper to get current audio quality configuration
func GetCurrentAudioQuality() audio.AudioConfig {
	ensureAudioControlService()
	return audioControlService.GetCurrentAudioQuality()
}

// handleSubscribeAudioEvents handles WebSocket audio event subscription
func handleSubscribeAudioEvents(connectionID string, wsCon *websocket.Conn, runCtx context.Context, l *zerolog.Logger) {
	ensureAudioControlService()
	audioControlService.SubscribeToAudioEvents(connectionID, wsCon, runCtx, l)
}

// handleUnsubscribeAudioEvents handles WebSocket audio event unsubscription
func handleUnsubscribeAudioEvents(connectionID string, l *zerolog.Logger) {
	ensureAudioControlService()
	audioControlService.UnsubscribeFromAudioEvents(connectionID, l)
}
