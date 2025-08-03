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

	if globalNonBlockingManager != nil && globalNonBlockingManager.IsRunning() {
		return ErrAudioAlreadyRunning
	}

	globalNonBlockingManager = NewNonBlockingAudioManager()
	return globalNonBlockingManager.StartAudioOutput(send)
}

// StartNonBlockingAudioInput starts the non-blocking audio input system
func StartNonBlockingAudioInput(receiveChan <-chan []byte) error {
	managerMutex.Lock()
	defer managerMutex.Unlock()

	if globalNonBlockingManager == nil {
		globalNonBlockingManager = NewNonBlockingAudioManager()
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