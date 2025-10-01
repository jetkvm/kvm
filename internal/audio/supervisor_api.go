package audio

import (
	"sync/atomic"
	"unsafe"
)

var (
	globalOutputSupervisor unsafe.Pointer // *AudioOutputSupervisor
	globalInputSupervisor  unsafe.Pointer // *AudioInputSupervisor
)

// SetAudioOutputSupervisor sets the global audio output supervisor
func SetAudioOutputSupervisor(supervisor *AudioOutputSupervisor) {
	atomic.StorePointer(&globalOutputSupervisor, unsafe.Pointer(supervisor))
}

// GetAudioOutputSupervisor returns the global audio output supervisor
func GetAudioOutputSupervisor() *AudioOutputSupervisor {
	ptr := atomic.LoadPointer(&globalOutputSupervisor)
	if ptr == nil {
		return nil
	}
	return (*AudioOutputSupervisor)(ptr)
}

// SetAudioInputSupervisor sets the global audio input supervisor
func SetAudioInputSupervisor(supervisor *AudioInputSupervisor) {
	atomic.StorePointer(&globalInputSupervisor, unsafe.Pointer(supervisor))
}

// GetAudioInputSupervisor returns the global audio input supervisor
func GetAudioInputSupervisor() *AudioInputSupervisor {
	ptr := atomic.LoadPointer(&globalInputSupervisor)
	if ptr == nil {
		return nil
	}
	return (*AudioInputSupervisor)(ptr)
}
