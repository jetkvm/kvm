//go:build cgo
// +build cgo

package audio

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/jetkvm/kvm/internal/logging"
	"github.com/rs/zerolog"
)

// Removed unused AudioOutputStreamer struct - actual streaming uses direct functions

var (
	outputStreamingRunning int32
	outputStreamingCancel  context.CancelFunc
	outputStreamingLogger  *zerolog.Logger
)

func getOutputStreamingLogger() *zerolog.Logger {
	if outputStreamingLogger == nil {
		logger := logging.GetDefaultLogger().With().Str("component", "audio-output-streaming").Logger()
		outputStreamingLogger = &logger
	}
	return outputStreamingLogger
}

// StartAudioOutputStreaming starts audio output streaming (capturing system audio)
func StartAudioOutputStreaming(send func([]byte)) error {
	if !atomic.CompareAndSwapInt32(&outputStreamingRunning, 0, 1) {
		return ErrAudioAlreadyRunning
	}

	// Initialize CGO audio capture with retry logic
	var initErr error
	for attempt := 0; attempt < 3; attempt++ {
		if initErr = CGOAudioInit(); initErr == nil {
			break
		}
		getOutputStreamingLogger().Warn().Err(initErr).Int("attempt", attempt+1).Msg("Audio initialization failed, retrying")
		time.Sleep(time.Duration(attempt+1) * 100 * time.Millisecond)
	}
	if initErr != nil {
		atomic.StoreInt32(&outputStreamingRunning, 0)
		return fmt.Errorf("failed to initialize audio after 3 attempts: %w", initErr)
	}

	ctx, cancel := context.WithCancel(context.Background())
	outputStreamingCancel = cancel

	// Start audio capture loop
	go func() {
		defer func() {
			CGOAudioClose()
			atomic.StoreInt32(&outputStreamingRunning, 0)
			getOutputStreamingLogger().Info().Msg("Audio output streaming stopped")
		}()

		getOutputStreamingLogger().Info().Str("socket_path", getOutputSocketPath()).Msg("Audio output streaming started, connected to output server")
		buffer := make([]byte, GetMaxAudioFrameSize())

		consecutiveErrors := 0
		maxConsecutiveErrors := Config.MaxConsecutiveErrors
		errorBackoffDelay := Config.RetryDelay
		maxErrorBackoff := Config.MaxRetryDelay
		var frameCount int64

		for {
			select {
			case <-ctx.Done():
				return
			default:
				// Capture audio frame with enhanced error handling and initialization checking
				n, err := CGOAudioReadEncode(buffer)
				if err != nil {
					consecutiveErrors++
					getOutputStreamingLogger().Warn().
						Err(err).
						Int("consecutive_errors", consecutiveErrors).
						Msg("Failed to read/encode audio")

					// Check if this is an initialization error (C error code -1)
					if strings.Contains(err.Error(), "C error code -1") {
						getOutputStreamingLogger().Error().Msg("Audio system not initialized properly, forcing reinitialization")
						// Force immediate reinitialization for init errors
						consecutiveErrors = maxConsecutiveErrors
					}

					// Implement progressive backoff for consecutive errors
					if consecutiveErrors >= maxConsecutiveErrors {
						getOutputStreamingLogger().Error().
							Int("consecutive_errors", consecutiveErrors).
							Msg("Too many consecutive audio errors, attempting recovery")

						// Try to reinitialize audio system
						CGOAudioClose()
						time.Sleep(errorBackoffDelay)
						if initErr := CGOAudioInit(); initErr != nil {
							getOutputStreamingLogger().Error().
								Err(initErr).
								Msg("Failed to reinitialize audio system")
							// Exponential backoff for reinitialization failures
							errorBackoffDelay = time.Duration(float64(errorBackoffDelay) * Config.BackoffMultiplier)
							if errorBackoffDelay > maxErrorBackoff {
								errorBackoffDelay = maxErrorBackoff
							}
						} else {
							getOutputStreamingLogger().Info().Msg("Audio system reinitialized successfully")
							consecutiveErrors = 0
							errorBackoffDelay = Config.RetryDelay // Reset backoff
						}
					} else {
						// Brief delay for transient errors
						time.Sleep(Config.ShortSleepDuration)
					}
					continue
				}

				// Success - reset error counters
				if consecutiveErrors > 0 {
					consecutiveErrors = 0
					errorBackoffDelay = Config.RetryDelay
				}

				if n > 0 {
					frameCount++

					// Get frame buffer from pool to reduce allocations
					frame := GetAudioFrameBuffer()
					frame = frame[:n] // Resize to actual frame size
					copy(frame, buffer[:n])

					// Zero-cost trace logging for output frame processing
					logger := getOutputStreamingLogger()
					if logger.GetLevel() <= zerolog.TraceLevel {
						if frameCount <= 5 || frameCount%1000 == 1 {
							logger.Trace().
								Int("frame_size", n).
								Int("buffer_capacity", cap(frame)).
								Int64("total_frames_sent", frameCount).
								Msg("Audio output frame captured and buffered")
						}
					}

					// Validate frame before sending
					if err := ValidateAudioFrame(frame); err != nil {
						getOutputStreamingLogger().Warn().Err(err).Msg("Frame validation failed, dropping frame")
						PutAudioFrameBuffer(frame)
						continue
					}

					send(frame)
					// Return buffer to pool after sending
					PutAudioFrameBuffer(frame)
					RecordFrameReceived(n)

					// Zero-cost trace logging for successful frame transmission
					if logger.GetLevel() <= zerolog.TraceLevel {
						if frameCount <= 5 || frameCount%1000 == 1 {
							logger.Trace().
								Int("frame_size", n).
								Int64("total_frames_sent", frameCount).
								Msg("Audio output frame sent successfully")
						}
					}
				}
				// Small delay to prevent busy waiting
				time.Sleep(Config.ShortSleepDuration)
			}
		}
	}()

	return nil
}

// StopAudioOutputStreaming stops audio output streaming
func StopAudioOutputStreaming() {
	if atomic.LoadInt32(&outputStreamingRunning) == 0 {
		return
	}

	if outputStreamingCancel != nil {
		outputStreamingCancel()
		outputStreamingCancel = nil
	}

	// Wait for streaming to stop
	for atomic.LoadInt32(&outputStreamingRunning) == 1 {
		time.Sleep(Config.ShortSleepDuration)
	}
}
