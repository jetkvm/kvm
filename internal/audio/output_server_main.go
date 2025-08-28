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
func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

// parseOpusConfig reads OPUS configuration from environment variables
// with fallback to default config values
func parseOpusConfig() (bitrate, complexity, vbr, signalType, bandwidth, dtx int) {
	// Read configuration from environment variables with config defaults
	bitrate = getEnvInt("JETKVM_OPUS_BITRATE", GetConfig().CGOOpusBitrate)
	complexity = getEnvInt("JETKVM_OPUS_COMPLEXITY", GetConfig().CGOOpusComplexity)
	vbr = getEnvInt("JETKVM_OPUS_VBR", GetConfig().CGOOpusVBR)
	signalType = getEnvInt("JETKVM_OPUS_SIGNAL_TYPE", GetConfig().CGOOpusSignalType)
	bandwidth = getEnvInt("JETKVM_OPUS_BANDWIDTH", GetConfig().CGOOpusBandwidth)
	dtx = getEnvInt("JETKVM_OPUS_DTX", GetConfig().CGOOpusDTX)

	return bitrate, complexity, vbr, signalType, bandwidth, dtx
}

// applyOpusConfig applies OPUS configuration to the global config
func applyOpusConfig(bitrate, complexity, vbr, signalType, bandwidth, dtx int) {
	logger := logging.GetDefaultLogger().With().Str("component", "audio-output-server").Logger()

	config := GetConfig()
	config.CGOOpusBitrate = bitrate
	config.CGOOpusComplexity = complexity
	config.CGOOpusVBR = vbr
	config.CGOOpusSignalType = signalType
	config.CGOOpusBandwidth = bandwidth
	config.CGOOpusDTX = dtx

	logger.Info().
		Int("bitrate", bitrate).
		Int("complexity", complexity).
		Int("vbr", vbr).
		Int("signal_type", signalType).
		Int("bandwidth", bandwidth).
		Int("dtx", dtx).
		Msg("applied OPUS configuration")
}

// RunAudioOutputServer runs the audio output server subprocess
// This should be called from main() when the subprocess is detected
func RunAudioOutputServer() error {
	logger := logging.GetDefaultLogger().With().Str("component", "audio-output-server").Logger()
	logger.Debug().Msg("audio output server subprocess starting")

	// Parse OPUS configuration from environment variables
	bitrate, complexity, vbr, signalType, bandwidth, dtx := parseOpusConfig()
	applyOpusConfig(bitrate, complexity, vbr, signalType, bandwidth, dtx)

	// Initialize validation cache for optimal performance
	InitValidationCache()

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

	logger.Debug().Msg("audio output server started, waiting for connections")

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
	logger.Debug().Msg("shutting down audio output server")
	StopNonBlockingAudioStreaming()

	// Give some time for cleanup
	time.Sleep(GetConfig().DefaultSleepDuration)

	logger.Debug().Msg("audio output server subprocess stopped")
	return nil
}
