package audio

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jetkvm/kvm/internal/logging"
)

// RunAudioOutputServer runs the audio output server subprocess
// This should be called from main() when the subprocess is detected
func RunAudioOutputServer() error {
	logger := logging.GetDefaultLogger().With().Str("component", "audio-output-server").Logger()
	logger.Info().Msg("Starting audio output server subprocess")

	// Create audio server
	server, err := NewAudioOutputServer()
	if err != nil {
		logger.Error().Err(err).Msg("failed to create audio server")
		return err
	}
	defer server.Close()

	// Start accepting connections
	if err := server.Start(); err != nil {
		logger.Error().Err(err).Msg("failed to start audio server")
		return err
	}

	// Initialize audio processing
	err = StartNonBlockingAudioStreaming(func(frame []byte) {
		if err := server.SendFrame(frame); err != nil {
			logger.Warn().Err(err).Msg("failed to send audio frame")
			RecordFrameDropped()
		}
	})
	if err != nil {
		logger.Error().Err(err).Msg("failed to start audio processing")
		return err
	}

	logger.Info().Msg("Audio output server started, waiting for connections")

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
	logger.Info().Msg("Shutting down audio output server")
	StopNonBlockingAudioStreaming()

	// Give some time for cleanup
	time.Sleep(GetConfig().DefaultSleepDuration)

	logger.Info().Msg("Audio output server subprocess stopped")
	return nil
}
