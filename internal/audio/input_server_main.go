//go:build cgo
// +build cgo

package audio

/*
#cgo pkg-config: alsa
#cgo LDFLAGS: -lopus
*/
import "C"

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jetkvm/kvm/internal/logging"
)

// Global audio input server instance
var globalAudioInputServer *AudioInputServer

// GetGlobalAudioInputServer returns the global audio input server instance
func GetGlobalAudioInputServer() *AudioInputServer {
	return globalAudioInputServer
}

// ResetGlobalAudioInputServerStats resets the global audio input server stats
func ResetGlobalAudioInputServerStats() {
	if globalAudioInputServer != nil {
		globalAudioInputServer.ResetServerStats()
	}
}

// RecoverGlobalAudioInputServer attempts to recover from dropped frames
func RecoverGlobalAudioInputServer() {
	if globalAudioInputServer != nil {
		globalAudioInputServer.RecoverFromDroppedFrames()
	}
}

// getEnvInt reads an integer from environment variable with a default value

// RunAudioInputServer runs the audio input server subprocess
// This should be called from main() when the subprocess is detected
func RunAudioInputServer() error {
	logger := logging.GetDefaultLogger().With().Str("component", "audio-input-server").Logger()

	// Parse OPUS configuration from environment variables
	bitrate, complexity, vbr, signalType, bandwidth, dtx := parseOpusConfig()
	applyOpusConfig(bitrate, complexity, vbr, signalType, bandwidth, dtx, "audio-input-server", false)

	// Initialize validation cache for optimal performance
	InitValidationCache()

	// Start adaptive buffer management for optimal performance
	StartAdaptiveBuffering()
	defer StopAdaptiveBuffering()

	// Initialize CGO audio playback (optional for input server)
	// This is used for audio loopback/monitoring features
	err := CGOAudioPlaybackInit()
	if err != nil {
		logger.Warn().Err(err).Msg("failed to initialize CGO audio playback - audio monitoring disabled")
		// Continue without playback - input functionality doesn't require it
	} else {
		defer CGOAudioPlaybackClose()
		logger.Info().Msg("CGO audio playback initialized successfully")
	}

	// Create and start the IPC server
	server, err := NewAudioInputServer()
	if err != nil {
		logger.Error().Err(err).Msg("failed to create audio input server")
		return err
	}
	defer server.Close()

	// Store globally for access by other functions
	globalAudioInputServer = server

	err = server.Start()
	if err != nil {
		logger.Error().Err(err).Msg("failed to start audio input server")
		return err
	}

	logger.Info().Msg("audio input server started, waiting for connections")

	// Set up signal handling for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Wait for shutdown signal
	select {
	case sig := <-sigChan:
		logger.Info().Str("signal", sig.String()).Msg("received shutdown signal")
	case <-ctx.Done():
	}

	// Graceful shutdown
	server.Stop()

	// Give some time for cleanup
	time.Sleep(Config.DefaultSleepDuration)

	return nil
}
