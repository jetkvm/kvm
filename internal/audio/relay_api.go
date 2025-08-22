package audio

import (
	"sync"
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

	// Get current audio config
	config := GetAudioConfig()

	// Start the relay (audioTrack can be nil initially)
	if err := relay.Start(audioTrack, config); err != nil {
		return err
	}

	globalRelay = relay
	return nil
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
func UpdateAudioRelayTrack(audioTrack AudioTrackWriter) error {
	relayMutex.Lock()
	defer relayMutex.Unlock()

	if globalRelay == nil {
		// No relay running, start one with the provided track
		relay := NewAudioRelay()
		config := GetAudioConfig()
		if err := relay.Start(audioTrack, config); err != nil {
			return err
		}
		globalRelay = relay
		return nil
	}

	// Update the track in the existing relay
	globalRelay.UpdateTrack(audioTrack)
	return nil
}
