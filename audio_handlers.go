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

func initAudioControlService() {
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
}

// --- Global Convenience Functions for Audio Control ---

// StopAudioOutputAndRemoveTracks is a global helper to stop audio output subprocess and remove WebRTC tracks
func StopAudioOutputAndRemoveTracks() error {
	initAudioControlService()
	return audioControlService.MuteAudio(true)
}

// StartAudioOutputAndAddTracks is a global helper to start audio output subprocess and add WebRTC tracks
func StartAudioOutputAndAddTracks() error {
	initAudioControlService()
	return audioControlService.MuteAudio(false)
}

// StopMicrophoneAndRemoveTracks is a global helper to stop microphone subprocess and remove WebRTC tracks
func StopMicrophoneAndRemoveTracks() error {
	initAudioControlService()
	return audioControlService.StopMicrophone()
}

// StartMicrophoneAndAddTracks is a global helper to start microphone subprocess and add WebRTC tracks
func StartMicrophoneAndAddTracks() error {
	initAudioControlService()
	return audioControlService.StartMicrophone()
}

// IsAudioOutputActive is a global helper to check if audio output subprocess is running
func IsAudioOutputActive() bool {
	initAudioControlService()
	return audioControlService.IsAudioOutputActive()
}

// IsMicrophoneActive is a global helper to check if microphone subprocess is running
func IsMicrophoneActive() bool {
	initAudioControlService()
	return audioControlService.IsMicrophoneActive()
}

// ResetMicrophone is a global helper to reset the microphone
func ResetMicrophone() error {
	initAudioControlService()
	return audioControlService.ResetMicrophone()
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
	initAudioControlService()
	audioControlService.SetAudioQuality(quality)
	return nil
}

// GetAudioQualityPresets is a global helper to get available audio quality presets
func GetAudioQualityPresets() map[audio.AudioQuality]audio.AudioConfig {
	initAudioControlService()
	return audioControlService.GetAudioQualityPresets()
}

// GetCurrentAudioQuality is a global helper to get current audio quality configuration
func GetCurrentAudioQuality() audio.AudioConfig {
	initAudioControlService()
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
		err = StopAudioOutputAndRemoveTracks()
	} else {
		err = StartAudioOutputAndAddTracks()
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
	err := StartMicrophoneAndAddTracks()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// handleMicrophoneStop handles POST /microphone/stop requests
func handleMicrophoneStop(c *gin.Context) {
	err := StopMicrophoneAndRemoveTracks()
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
		err = StopMicrophoneAndRemoveTracks()
	} else {
		err = StartMicrophoneAndAddTracks()
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
	initAudioControlService()
	audioControlService.SubscribeToAudioEvents(connectionID, wsCon, runCtx, l)
}

// handleUnsubscribeAudioEvents handles WebSocket audio event unsubscription
func handleUnsubscribeAudioEvents(connectionID string, l *zerolog.Logger) {
	initAudioControlService()
	audioControlService.UnsubscribeFromAudioEvents(connectionID, l)
}

// handleAudioStatus handles GET requests for audio status
func handleAudioStatus(c *gin.Context) {
	initAudioControlService()

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
