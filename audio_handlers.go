package kvm

import (
	"context"

	"github.com/coder/websocket"
	"github.com/jetkvm/kvm/internal/audio"
	"github.com/rs/zerolog"
)

var audioControlService *audio.AudioControlService

func ensureAudioControlService() *audio.AudioControlService {
	if audioControlService == nil {
		sessionProvider := &SessionProviderImpl{}
		audioControlService = audio.NewAudioControlService(sessionProvider, logger)

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
