package audio

import (
	"sync/atomic"
	"unsafe"
)

var (
	// Use unsafe.Pointer for atomic operations instead of mutex
	globalNonBlockingManager unsafe.Pointer // *NonBlockingAudioManager
)

// loadManager atomically loads the global manager
func loadManager() *NonBlockingAudioManager {
	ptr := atomic.LoadPointer(&globalNonBlockingManager)
	if ptr == nil {
		return nil
	}
	return (*NonBlockingAudioManager)(ptr)
}

// storeManager atomically stores the global manager
func storeManager(manager *NonBlockingAudioManager) {
	atomic.StorePointer(&globalNonBlockingManager, unsafe.Pointer(manager))
}

// compareAndSwapManager atomically compares and swaps the global manager
func compareAndSwapManager(old, new *NonBlockingAudioManager) bool {
	return atomic.CompareAndSwapPointer(&globalNonBlockingManager, 
		unsafe.Pointer(old), unsafe.Pointer(new))
}

// StartNonBlockingAudioStreaming starts the non-blocking audio streaming system
func StartNonBlockingAudioStreaming(send func([]byte)) error {
	manager := loadManager()
	if manager != nil && manager.IsOutputRunning() {
		return nil // Already running, this is not an error
	}

	if manager == nil {
		newManager := NewNonBlockingAudioManager()
		if !compareAndSwapManager(nil, newManager) {
			// Another goroutine created manager, use it
			manager = loadManager()
		} else {
			manager = newManager
		}
	}

	return manager.StartAudioOutput(send)
}

// StartNonBlockingAudioInput starts the non-blocking audio input system
func StartNonBlockingAudioInput(receiveChan <-chan []byte) error {
	manager := loadManager()
	if manager == nil {
		newManager := NewNonBlockingAudioManager()
		if !compareAndSwapManager(nil, newManager) {
			// Another goroutine created manager, use it
			manager = loadManager()
		} else {
			manager = newManager
		}
	}

	// Check if input is already running to avoid unnecessary operations
	if manager.IsInputRunning() {
		return nil // Already running, this is not an error
	}

	return manager.StartAudioInput(receiveChan)
}

// StopNonBlockingAudioStreaming stops the non-blocking audio streaming system
func StopNonBlockingAudioStreaming() {
	manager := loadManager()
	if manager != nil {
		manager.Stop()
		storeManager(nil)
	}
}

// StopNonBlockingAudioInput stops only the audio input without affecting output
func StopNonBlockingAudioInput() {
	manager := loadManager()
	if manager != nil && manager.IsInputRunning() {
		manager.StopAudioInput()
		
		// If both input and output are stopped, recreate manager to ensure clean state
		if !manager.IsRunning() {
			storeManager(nil)
		}
	}
}

// GetNonBlockingAudioStats returns statistics from the non-blocking audio system
func GetNonBlockingAudioStats() NonBlockingAudioStats {
	manager := loadManager()
	if manager != nil {
		return manager.GetStats()
	}
	return NonBlockingAudioStats{}
}

// IsNonBlockingAudioRunning returns true if the non-blocking audio system is running
func IsNonBlockingAudioRunning() bool {
	manager := loadManager()
	return manager != nil && manager.IsRunning()
}

// IsNonBlockingAudioInputRunning returns true if the non-blocking audio input is running
func IsNonBlockingAudioInputRunning() bool {
	manager := loadManager()
	return manager != nil && manager.IsInputRunning()
}
