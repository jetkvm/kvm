package audio

import (
	"sync"
)

var (
	globalNonBlockingManager *NonBlockingAudioManager
	managerMutex             sync.Mutex
)

// StartNonBlockingAudioStreaming starts the non-blocking audio streaming system
func StartNonBlockingAudioStreaming(send func([]byte)) error {
	managerMutex.Lock()
	defer managerMutex.Unlock()

	if globalNonBlockingManager != nil && globalNonBlockingManager.IsOutputRunning() {
		return nil // Already running, this is not an error
	}

	if globalNonBlockingManager == nil {
		globalNonBlockingManager = NewNonBlockingAudioManager()
	}

	return globalNonBlockingManager.StartAudioOutput(send)
}

// StartNonBlockingAudioInput starts the non-blocking audio input system
func StartNonBlockingAudioInput(receiveChan <-chan []byte) error {
	managerMutex.Lock()
	defer managerMutex.Unlock()

	if globalNonBlockingManager == nil {
		globalNonBlockingManager = NewNonBlockingAudioManager()
	}

	// Check if input is already running to avoid unnecessary operations
	if globalNonBlockingManager.IsInputRunning() {
		return nil // Already running, this is not an error
	}

	return globalNonBlockingManager.StartAudioInput(receiveChan)
}

// StopNonBlockingAudioStreaming stops the non-blocking audio streaming system
func StopNonBlockingAudioStreaming() {
	managerMutex.Lock()
	defer managerMutex.Unlock()

	if globalNonBlockingManager != nil {
		globalNonBlockingManager.Stop()
		globalNonBlockingManager = nil
	}
}

// StopNonBlockingAudioInput stops only the audio input without affecting output
func StopNonBlockingAudioInput() {
	managerMutex.Lock()
	defer managerMutex.Unlock()

	if globalNonBlockingManager != nil && globalNonBlockingManager.IsInputRunning() {
		globalNonBlockingManager.StopAudioInput()
		
		// If both input and output are stopped, recreate manager to ensure clean state
		if !globalNonBlockingManager.IsRunning() {
			globalNonBlockingManager = nil
		}
	}
}

// GetNonBlockingAudioStats returns statistics from the non-blocking audio system
func GetNonBlockingAudioStats() NonBlockingAudioStats {
	managerMutex.Lock()
	defer managerMutex.Unlock()

	if globalNonBlockingManager != nil {
		return globalNonBlockingManager.GetStats()
	}
	return NonBlockingAudioStats{}
}

// IsNonBlockingAudioRunning returns true if the non-blocking audio system is running
func IsNonBlockingAudioRunning() bool {
	managerMutex.Lock()
	defer managerMutex.Unlock()

	return globalNonBlockingManager != nil && globalNonBlockingManager.IsRunning()
}

// IsNonBlockingAudioInputRunning returns true if the non-blocking audio input is running
func IsNonBlockingAudioInputRunning() bool {
	managerMutex.Lock()
	defer managerMutex.Unlock()

	return globalNonBlockingManager != nil && globalNonBlockingManager.IsInputRunning()
}
