package audio

import (
	"context"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jetkvm/kvm/internal/logging"
	"github.com/rs/zerolog"
)

// getEnvInt reads an integer from environment variable with a default value

// RunAudioOutputServer runs the audio output server subprocess
// This should be called from main() when the subprocess is detected
func RunAudioOutputServer() error {
	logger := logging.GetSubsystemLogger("audio").With().Str("component", "audio-output-server").Logger()

	// Parse OPUS configuration from environment variables
	bitrate, complexity, vbr, signalType, bandwidth, dtx := parseOpusConfig()
	applyOpusConfig(bitrate, complexity, vbr, signalType, bandwidth, dtx, "audio-output-server", true)

	// Initialize validation cache for optimal performance
	InitValidationCache()

	// Create audio server
	server, err := NewAudioOutputServer()
	if err != nil {
		logger.Error().Err(err).Msg("failed to create audio server")
		return err
	}
	defer server.Stop()

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

	logger.Info().Msg("audio output server started, waiting for connections")

	// Update C trace logging based on current audio scope log level (after environment variables are processed)
	loggerTraceEnabled := logger.GetLevel() <= zerolog.TraceLevel

	// Manual check for audio scope in PION_LOG_TRACE (workaround for logging system bug)
	manualTraceEnabled := false
	pionTrace := os.Getenv("PION_LOG_TRACE")
	if pionTrace != "" {
		scopes := strings.Split(strings.ToLower(pionTrace), ",")
		for _, scope := range scopes {
			if strings.TrimSpace(scope) == "audio" {
				manualTraceEnabled = true
				break
			}
		}
	}

	// Use manual check as fallback if logging system fails
	traceEnabled := loggerTraceEnabled || manualTraceEnabled

	CGOSetTraceLogging(traceEnabled)

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
	StopNonBlockingAudioStreaming()

	// Give some time for cleanup
	time.Sleep(Config.DefaultSleepDuration)

	return nil
}
