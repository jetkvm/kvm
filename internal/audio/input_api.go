package audio

import (
	"sync/atomic"
	"unsafe"
)

var (
	// Global audio input manager instance
	globalInputManager unsafe.Pointer // *AudioInputManager
)

// AudioInputInterface defines the common interface for audio input managers
type AudioInputInterface interface {
	Start() error
	Stop()
	WriteOpusFrame(frame []byte) error
	IsRunning() bool
	GetMetrics() AudioInputMetrics
}

// GetSupervisor returns the audio input supervisor for advanced management
func (m *AudioInputManager) GetSupervisor() *AudioInputSupervisor {
	return m.ipcManager.GetSupervisor()
}

// getAudioInputManager returns the audio input manager
func getAudioInputManager() AudioInputInterface {
	ptr := atomic.LoadPointer(&globalInputManager)
	if ptr == nil {
		// Create new manager
		newManager := NewAudioInputManager()
		if atomic.CompareAndSwapPointer(&globalInputManager, nil, unsafe.Pointer(newManager)) {
			return newManager
		}
		// Another goroutine created it, use that one
		ptr = atomic.LoadPointer(&globalInputManager)
	}
	return (*AudioInputManager)(ptr)
}

// StartAudioInput starts the audio input system using the appropriate manager
func StartAudioInput() error {
	manager := getAudioInputManager()
	return manager.Start()
}

// StopAudioInput stops the audio input system
func StopAudioInput() {
	manager := getAudioInputManager()
	manager.Stop()
}

// WriteAudioInputFrame writes an Opus frame to the audio input system
func WriteAudioInputFrame(frame []byte) error {
	manager := getAudioInputManager()
	return manager.WriteOpusFrame(frame)
}

// IsAudioInputRunning returns whether the audio input system is running
func IsAudioInputRunning() bool {
	manager := getAudioInputManager()
	return manager.IsRunning()
}

// GetAudioInputMetrics returns current audio input metrics
func GetAudioInputMetrics() AudioInputMetrics {
	manager := getAudioInputManager()
	return manager.GetMetrics()
}

// GetAudioInputIPCSupervisor returns the IPC supervisor
func GetAudioInputIPCSupervisor() *AudioInputSupervisor {
	ptr := atomic.LoadPointer(&globalInputManager)
	if ptr == nil {
		return nil
	}

	manager := (*AudioInputManager)(ptr)
	return manager.GetSupervisor()
}

// Helper functions

// ResetAudioInputManagers resets the global manager (for testing)
func ResetAudioInputManagers() {
	// Stop existing manager first
	if ptr := atomic.LoadPointer(&globalInputManager); ptr != nil {
		(*AudioInputManager)(ptr).Stop()
	}

	// Reset pointer
	atomic.StorePointer(&globalInputManager, nil)
}
