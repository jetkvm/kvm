package audio

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// Global relay instance for the main process
var (
	globalRelay *AudioRelay
	relayMutex  sync.RWMutex
)

// StartAudioRelay starts the audio relay system for the main process
// This replaces the CGO-based audio system when running in main process mode
// audioTrack can be nil initially and updated later via UpdateAudioRelayTrack
func StartAudioRelay(audioTrack AudioTrackWriter) error {
	relayMutex.Lock()
	defer relayMutex.Unlock()

	if globalRelay != nil {
		return nil // Already running
	}

	// Create new relay
	relay := NewAudioRelay()

	// Retry starting the relay with exponential backoff
	// This handles cases where the subprocess hasn't created its socket yet
	maxAttempts := 5
	baseDelay := 200 * time.Millisecond
	maxDelay := 2 * time.Second

	var lastErr error
	for i := 0; i < maxAttempts; i++ {
		if err := relay.Start(audioTrack); err != nil {
			lastErr = err
			if i < maxAttempts-1 {
				// Calculate exponential backoff delay
				delay := time.Duration(float64(baseDelay) * (1.5 * float64(i+1)))
				if delay > maxDelay {
					delay = maxDelay
				}
				time.Sleep(delay)
				continue
			}
			return fmt.Errorf("failed to start audio relay after %d attempts: %w", maxAttempts, lastErr)
		}

		// Success
		globalRelay = relay
		return nil
	}

	return fmt.Errorf("failed to start audio relay after %d attempts: %w", maxAttempts, lastErr)
}

// StopAudioRelay stops the audio relay system
func StopAudioRelay() {
	relayMutex.Lock()
	defer relayMutex.Unlock()

	if globalRelay != nil {
		globalRelay.Stop()
		globalRelay = nil
	}
}

// SetAudioRelayMuted sets the mute state for the audio relay
func SetAudioRelayMuted(muted bool) {
	relayMutex.RLock()
	defer relayMutex.RUnlock()

	if globalRelay != nil {
		globalRelay.SetMuted(muted)
	}
}

// IsAudioRelayMuted returns the current mute state of the audio relay
func IsAudioRelayMuted() bool {
	relayMutex.RLock()
	defer relayMutex.RUnlock()

	if globalRelay != nil {
		return globalRelay.IsMuted()
	}
	return false
}

// GetAudioRelayStats returns statistics from the audio relay
func GetAudioRelayStats() (framesRelayed, framesDropped int64) {
	relayMutex.RLock()
	defer relayMutex.RUnlock()

	if globalRelay != nil {
		return globalRelay.GetStats()
	}
	return 0, 0
}

// IsAudioRelayRunning returns whether the audio relay is currently running
func IsAudioRelayRunning() bool {
	relayMutex.RLock()
	defer relayMutex.RUnlock()

	return globalRelay != nil
}

// UpdateAudioRelayTrack updates the WebRTC audio track for the relay
// This function is refactored to prevent mutex deadlocks during quality changes
func UpdateAudioRelayTrack(audioTrack AudioTrackWriter) error {
	var needsCallback bool
	var callbackFunc TrackReplacementCallback

	// Critical section: minimize time holding the mutex
	relayMutex.Lock()
	if globalRelay == nil {
		// No relay running, start one with the provided track
		relay := NewAudioRelay()
		if err := relay.Start(audioTrack); err != nil {
			relayMutex.Unlock()
			return err
		}
		globalRelay = relay
	} else {
		// Update the track in the existing relay
		globalRelay.UpdateTrack(audioTrack)
	}

	// Capture callback state while holding mutex
	needsCallback = trackReplacementCallback != nil
	if needsCallback {
		callbackFunc = trackReplacementCallback
	}
	relayMutex.Unlock()

	// Execute callback outside of mutex to prevent deadlock
	if needsCallback && callbackFunc != nil {
		// Use goroutine with timeout to prevent blocking
		done := make(chan error, 1)
		go func() {
			done <- callbackFunc(audioTrack)
		}()

		// Wait for callback with timeout
		select {
		case err := <-done:
			if err != nil {
				// Log error but don't fail the relay operation
				// The relay can still work even if WebRTC track replacement fails
				_ = err // Suppress linter warning
			}
		case <-time.After(5 * time.Second):
			// Timeout: log warning but continue
			// This prevents indefinite blocking during quality changes
			_ = fmt.Errorf("track replacement callback timed out")
		}
	}

	return nil
}

// CurrentSessionCallback is a function type for getting the current session's audio track
type CurrentSessionCallback func() AudioTrackWriter

// TrackReplacementCallback is a function type for replacing the WebRTC audio track
type TrackReplacementCallback func(AudioTrackWriter) error

// currentSessionCallback holds the callback function to get the current session's audio track
var currentSessionCallback CurrentSessionCallback

// trackReplacementCallback holds the callback function to replace the WebRTC audio track
var trackReplacementCallback TrackReplacementCallback

// SetCurrentSessionCallback sets the callback function to get the current session's audio track
func SetCurrentSessionCallback(callback CurrentSessionCallback) {
	currentSessionCallback = callback
}

// SetTrackReplacementCallback sets the callback function to replace the WebRTC audio track
func SetTrackReplacementCallback(callback TrackReplacementCallback) {
	trackReplacementCallback = callback
}

// UpdateAudioRelayTrackAsync performs async track update to prevent blocking
// This is used during WebRTC session creation to avoid deadlocks
func UpdateAudioRelayTrackAsync(audioTrack AudioTrackWriter) {
	go func() {
		if err := UpdateAudioRelayTrack(audioTrack); err != nil {
			// Log error but don't block session creation
			_ = err // Suppress linter warning
		}
	}()
}

// connectRelayToCurrentSession connects the audio relay to the current WebRTC session's audio track
// This is used when restarting the relay during unmute operations
func connectRelayToCurrentSession() error {
	if currentSessionCallback == nil {
		return errors.New("no current session callback set")
	}

	track := currentSessionCallback()
	if track == nil {
		return errors.New("no current session audio track available")
	}

	relayMutex.Lock()
	defer relayMutex.Unlock()

	if globalRelay != nil {
		globalRelay.UpdateTrack(track)
		return nil
	}

	return errors.New("no global relay running")
}
