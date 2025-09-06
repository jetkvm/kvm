package kvm

import (
	"context"
	"net/http"

	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/jetkvm/kvm/internal/audio"
	"github.com/rs/zerolog"
)

var audioControlService *audio.AudioControlService

func initAudioControlService() {
	if audioControlService == nil {
		sessionProvider := &SessionProviderImpl{}
		audioControlService = audio.NewAudioControlService(sessionProvider, logger)
	}
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
	initAudioControlService()

	err := audioControlService.MuteAudio(req.Muted)
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
	initAudioControlService()

	err := audioControlService.StartMicrophone()
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

	initAudioControlService()

	err := audioControlService.MuteMicrophone(req.Muted)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// handleMicrophoneReset handles POST /microphone/reset requests
func handleMicrophoneReset(c *gin.Context) {
	initAudioControlService()

	err := audioControlService.ResetMicrophone()
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
	initAudioControlService()

	presets := audioControlService.GetAudioQualityPresets()
	current := audioControlService.GetCurrentAudioQuality()

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

	initAudioControlService()

	// Convert int to AudioQuality type
	quality := audio.AudioQuality(req.Quality)

	// Set the audio quality
	audioControlService.SetAudioQuality(quality)

	// Return the updated configuration
	current := audioControlService.GetCurrentAudioQuality()
	c.JSON(200, gin.H{
		"success": true,
		"config":  current,
	})
}

// handleMicrophoneQuality handles GET requests for microphone quality presets
func handleMicrophoneQuality(c *gin.Context) {
	initAudioControlService()
	presets := audioControlService.GetMicrophoneQualityPresets()
	current := audioControlService.GetCurrentMicrophoneQuality()
	c.JSON(200, gin.H{
		"presets": presets,
		"current": current,
	})
}

// handleSetMicrophoneQuality handles POST requests to set microphone quality
func handleSetMicrophoneQuality(c *gin.Context) {
	var req struct {
		Quality int `json:"quality"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	initAudioControlService()

	// Convert int to AudioQuality type
	quality := audio.AudioQuality(req.Quality)

	// Set the microphone quality
	audioControlService.SetMicrophoneQuality(quality)

	// Return the updated configuration
	current := audioControlService.GetCurrentMicrophoneQuality()
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"config":  current,
	})
}
