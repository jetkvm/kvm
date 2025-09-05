package kvm

import (
	"context"
	"time"

	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/jetkvm/kvm/internal/audio"
	"github.com/rs/zerolog"
)

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
	audio.SetAudioMuted(req.Muted)
	// Also set relay mute state if in main process
	audio.SetAudioRelayMuted(req.Muted)

	// Broadcast audio mute state change via WebSocket
	broadcaster := audio.GetAudioEventBroadcaster()
	broadcaster.BroadcastAudioDeviceChanged(!req.Muted, "audio_mute_changed")

	c.JSON(200, gin.H{
		"status": "audio mute state updated",
		"muted":  req.Muted,
	})
}

// handleMicrophoneStart handles POST /microphone/start requests
func handleMicrophoneStart(c *gin.Context) {
	if currentSession == nil {
		c.JSON(400, gin.H{"error": "no active session"})
		return
	}

	if currentSession.AudioInputManager == nil {
		c.JSON(500, gin.H{"error": "audio input manager not available"})
		return
	}

	// Check cooldown using atomic operations
	// Note: Cooldown check would be implemented in audio package if needed

	logger.Info().Msg("starting microphone via HTTP request")

	err := currentSession.AudioInputManager.Start()
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	// Broadcast microphone state change via WebSocket
	broadcaster := audio.GetAudioEventBroadcaster()
	broadcaster.BroadcastAudioDeviceChanged(true, "microphone_started")

	c.JSON(200, gin.H{
		"status":     "microphone started",
		"is_running": currentSession.AudioInputManager.IsRunning(),
	})
}

// handleMicrophoneMute handles POST /microphone/mute requests
func handleMicrophoneMute(c *gin.Context) {
	var req struct {
		Muted bool `json:"muted"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "invalid request body"})
		return
	}

	// Note: Microphone muting is typically handled at the frontend level
	// This endpoint is provided for consistency but doesn't affect backend processing
	c.JSON(200, gin.H{
		"status": "mute state updated",
		"muted":  req.Muted,
	})
}



// handleMicrophoneReset handles POST /microphone/reset requests
func handleMicrophoneReset(c *gin.Context) {
	if currentSession == nil {
		c.JSON(400, gin.H{"error": "no active session"})
		return
	}

	if currentSession.AudioInputManager == nil {
		c.JSON(500, gin.H{"error": "audio input manager not available"})
		return
	}

	// Check cooldown using atomic operations
	// Note: Cooldown check would be implemented in audio package if needed

	logger.Info().Msg("forcing microphone state reset")

	// Force stop the AudioInputManager
	currentSession.AudioInputManager.Stop()

	// Wait a bit to ensure everything is stopped
	time.Sleep(100 * time.Millisecond)

	// Broadcast microphone state change via WebSocket
	broadcaster := audio.GetAudioEventBroadcaster()
	broadcaster.BroadcastAudioDeviceChanged(false, "microphone_reset")

	c.JSON(200, gin.H{
		"status":     "microphone reset completed",
		"is_running": currentSession.AudioInputManager.IsRunning(),
	})
}

// handleSubscribeAudioEvents handles WebSocket audio event subscription
func handleSubscribeAudioEvents(connectionID string, wsCon *websocket.Conn, runCtx context.Context, l *zerolog.Logger) {
	l.Info().Msg("client subscribing to audio events")
	broadcaster := audio.GetAudioEventBroadcaster()
	broadcaster.Subscribe(connectionID, wsCon, runCtx, l)
}

// handleUnsubscribeAudioEvents handles WebSocket audio event unsubscription
func handleUnsubscribeAudioEvents(connectionID string, l *zerolog.Logger) {
	l.Info().Msg("client unsubscribing from audio events")
	broadcaster := audio.GetAudioEventBroadcaster()
	broadcaster.Unsubscribe(connectionID)
}
