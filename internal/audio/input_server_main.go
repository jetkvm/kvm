package audio

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jetkvm/kvm/internal/logging"
)

// RunAudioInputServer runs the audio input server subprocess
// This should be called from main() when the subprocess is detected
func RunAudioInputServer() error {
	logger := logging.GetDefaultLogger().With().Str("component", "audio-input-server").Logger()
	logger.Info().Msg("Starting audio input server subprocess")

	// Initialize CGO audio system
	err := CGOAudioPlaybackInit()
	if err != nil {
		logger.Error().Err(err).Msg("Failed to initialize CGO audio playback")
		return err
	}
	defer CGOAudioPlaybackClose()

	// Create and start the IPC server
	server, err := NewAudioInputServer()
	if err != nil {
		logger.Error().Err(err).Msg("Failed to create audio input server")
		return err
	}
	defer server.Close()

	err = server.Start()
	if err != nil {
		logger.Error().Err(err).Msg("Failed to start audio input server")
		return err
	}

	logger.Info().Msg("Audio input server started, waiting for connections")

	// Set up signal handling for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Wait for shutdown signal
	select {
	case sig := <-sigChan:
		logger.Info().Str("signal", sig.String()).Msg("Received shutdown signal")
	case <-ctx.Done():
		logger.Info().Msg("Context cancelled")
	}

	// Graceful shutdown
	logger.Info().Msg("Shutting down audio input server")
	server.Stop()

	// Give some time for cleanup
	time.Sleep(100 * time.Millisecond)

	logger.Info().Msg("Audio input server subprocess stopped")
	return nil
}
