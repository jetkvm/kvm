package audio

import (
	"context"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/jetkvm/kvm/internal/logging"
)

// getEnvInt reads an integer from environment variable with a default value
func getEnvIntInput(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

// parseOpusConfigInput reads OPUS configuration from environment variables
// with fallback to default config values for input server
func parseOpusConfigInput() (bitrate, complexity, vbr, signalType, bandwidth, dtx int) {
	// Read configuration from environment variables with config defaults
	bitrate = getEnvIntInput("JETKVM_OPUS_BITRATE", GetConfig().CGOOpusBitrate)
	complexity = getEnvIntInput("JETKVM_OPUS_COMPLEXITY", GetConfig().CGOOpusComplexity)
	vbr = getEnvIntInput("JETKVM_OPUS_VBR", GetConfig().CGOOpusVBR)
	signalType = getEnvIntInput("JETKVM_OPUS_SIGNAL_TYPE", GetConfig().CGOOpusSignalType)
	bandwidth = getEnvIntInput("JETKVM_OPUS_BANDWIDTH", GetConfig().CGOOpusBandwidth)
	dtx = getEnvIntInput("JETKVM_OPUS_DTX", GetConfig().CGOOpusDTX)

	return bitrate, complexity, vbr, signalType, bandwidth, dtx
}

// applyOpusConfigInput applies OPUS configuration to the global config for input server
func applyOpusConfigInput(bitrate, complexity, vbr, signalType, bandwidth, dtx int) {
	config := GetConfig()
	config.CGOOpusBitrate = bitrate
	config.CGOOpusComplexity = complexity
	config.CGOOpusVBR = vbr
	config.CGOOpusSignalType = signalType
	config.CGOOpusBandwidth = bandwidth
	config.CGOOpusDTX = dtx
}

// RunAudioInputServer runs the audio input server subprocess
// This should be called from main() when the subprocess is detected
func RunAudioInputServer() error {
	logger := logging.GetDefaultLogger().With().Str("component", "audio-input-server").Logger()
	logger.Debug().Msg("audio input server subprocess starting")

	// Parse OPUS configuration from environment variables
	bitrate, complexity, vbr, signalType, bandwidth, dtx := parseOpusConfigInput()
	applyOpusConfigInput(bitrate, complexity, vbr, signalType, bandwidth, dtx)

	// Initialize validation cache for optimal performance
	InitValidationCache()

	// Start adaptive buffer management for optimal performance
	StartAdaptiveBuffering()
	defer StopAdaptiveBuffering()

	// Initialize CGO audio system
	err := CGOAudioPlaybackInit()
	if err != nil {
		logger.Error().Err(err).Msg("failed to initialize CGO audio playback")
		return err
	}
	defer CGOAudioPlaybackClose()

	// Create and start the IPC server
	server, err := NewAudioInputServer()
	if err != nil {
		logger.Error().Err(err).Msg("failed to create audio input server")
		return err
	}
	defer server.Close()

	err = server.Start()
	if err != nil {
		logger.Error().Err(err).Msg("failed to start audio input server")
		return err
	}

	logger.Debug().Msg("audio input server started, waiting for connections")

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
		logger.Debug().Msg("context cancelled")
	}

	// Graceful shutdown
	logger.Debug().Msg("shutting down audio input server")
	server.Stop()

	// Give some time for cleanup
	time.Sleep(GetConfig().DefaultSleepDuration)

	logger.Debug().Msg("audio input server subprocess stopped")
	return nil
}
