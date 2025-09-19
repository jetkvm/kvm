package kvm

import (
	"context"
	"net/http"

	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"
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

// handleAudioMute handles POST /audio/mute requests
func handleAudioMute(c *gin.Context) {
	type muteReq struct {
		Muted bool `json:"muted"`
	}
	var req muteReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "invalid request"})
		return
	}

	var err error
	if req.Muted {
		err = MuteAudioOutput()
	} else {
		err = UnmuteAudioOutput()
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, gin.H{
		"status": "audio mute state updated",
		"muted":  req.Muted,
	})
}

// handleMicrophoneStart handles POST /microphone/start requests
func handleMicrophoneStart(c *gin.Context) {
	err := StartMicrophone()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// handleMicrophoneStop handles POST /microphone/stop requests
func handleMicrophoneStop(c *gin.Context) {
	err := StopMicrophone()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// handleMicrophoneMute handles POST /microphone/mute requests
func handleMicrophoneMute(c *gin.Context) {
	var req struct {
		Muted bool `json:"muted"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var err error
	if req.Muted {
		err = StopMicrophone()
	} else {
		err = StartMicrophone()
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// handleMicrophoneReset handles POST /microphone/reset requests
func handleMicrophoneReset(c *gin.Context) {
	err := ResetMicrophone()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
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

// handleAudioStatus handles GET requests for audio status
func handleAudioStatus(c *gin.Context) {
	ensureAudioControlService()

	status := audioControlService.GetAudioStatus()
	c.JSON(200, status)
}

// handleAudioQuality handles GET requests for audio quality presets
func handleAudioQuality(c *gin.Context) {
	presets := GetAudioQualityPresets()
	current := GetCurrentAudioQuality()

	c.JSON(200, gin.H{
		"presets": presets,
		"current": current,
	})
}

// handleSetAudioQuality handles POST requests to set audio quality
func handleSetAudioQuality(c *gin.Context) {
	var req struct {
		Quality int `json:"quality"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	// Check if audio output is active before attempting quality change
	// This prevents race conditions where quality changes are attempted before initialization
	if !IsAudioOutputActive() {
		c.JSON(503, gin.H{"error": "audio output not active - please wait for initialization to complete"})
		return
	}

	// Convert int to AudioQuality type
	quality := audio.AudioQuality(req.Quality)

	// Set the audio quality using global convenience function
	if err := SetAudioQuality(quality); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	// Return the updated configuration
	current := GetCurrentAudioQuality()
	c.JSON(200, gin.H{
		"success": true,
		"config":  current,
	})
}
