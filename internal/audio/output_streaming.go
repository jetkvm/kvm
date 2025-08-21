package audio

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/jetkvm/kvm/internal/logging"
	"github.com/rs/zerolog"
)

var (
	outputStreamingRunning int32
	outputStreamingCancel  context.CancelFunc
	outputStreamingLogger  *zerolog.Logger
)

func init() {
	logger := logging.GetDefaultLogger().With().Str("component", "audio-output").Logger()
	outputStreamingLogger = &logger
}

// StartAudioOutputStreaming starts audio output streaming (capturing system audio)
func StartAudioOutputStreaming(send func([]byte)) error {
	if !atomic.CompareAndSwapInt32(&outputStreamingRunning, 0, 1) {
		return ErrAudioAlreadyRunning
	}

	// Initialize CGO audio capture
	if err := CGOAudioInit(); err != nil {
		atomic.StoreInt32(&outputStreamingRunning, 0)
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	outputStreamingCancel = cancel

	// Start audio capture loop
	go func() {
		defer func() {
			CGOAudioClose()
			atomic.StoreInt32(&outputStreamingRunning, 0)
			outputStreamingLogger.Info().Msg("Audio output streaming stopped")
		}()

		outputStreamingLogger.Info().Msg("Audio output streaming started")
		buffer := make([]byte, MaxAudioFrameSize)

		for {
			select {
			case <-ctx.Done():
				return
			default:
				// Capture audio frame
				n, err := CGOAudioReadEncode(buffer)
				if err != nil {
					outputStreamingLogger.Warn().Err(err).Msg("Failed to read/encode audio")
					continue
				}
				if n > 0 {
					// Send frame to callback
					frame := make([]byte, n)
					copy(frame, buffer[:n])
					send(frame)
					RecordFrameReceived(n)
				}
				// Small delay to prevent busy waiting
				time.Sleep(10 * time.Millisecond)
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
		time.Sleep(10 * time.Millisecond)
	}
}