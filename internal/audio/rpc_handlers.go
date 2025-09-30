package audio

import (
	"fmt"
)

// RPC wrapper functions for audio control
// These functions bridge the RPC layer to the AudioControlService

// This variable will be set by the main package to provide access to the global service
var (
	getAudioControlServiceFunc func() *AudioControlService
)

// SetRPCCallbacks sets the callback function for RPC operations
func SetRPCCallbacks(getService func() *AudioControlService) {
	getAudioControlServiceFunc = getService
}

// RPCAudioMute handles audio mute/unmute RPC requests
func RPCAudioMute(muted bool) error {
	if getAudioControlServiceFunc == nil {
		return fmt.Errorf("audio control service not available")
	}
	service := getAudioControlServiceFunc()
	if service == nil {
		return fmt.Errorf("audio control service not initialized")
	}
	return service.MuteAudio(muted)
}

// RPCAudioQuality is deprecated - quality is now fixed at optimal settings
// Returns current config for backward compatibility
func RPCAudioQuality(quality int) (map[string]any, error) {
	// Quality is now fixed - return current optimal configuration
	currentConfig := GetAudioConfig()
	return map[string]any{"config": currentConfig}, nil
}

// RPCMicrophoneStart handles microphone start RPC requests
func RPCMicrophoneStart() error {
	if getAudioControlServiceFunc == nil {
		return fmt.Errorf("audio control service not available")
	}
	service := getAudioControlServiceFunc()
	if service == nil {
		return fmt.Errorf("audio control service not initialized")
	}
	return service.StartMicrophone()
}

// RPCMicrophoneStop handles microphone stop RPC requests
func RPCMicrophoneStop() error {
	if getAudioControlServiceFunc == nil {
		return fmt.Errorf("audio control service not available")
	}
	service := getAudioControlServiceFunc()
	if service == nil {
		return fmt.Errorf("audio control service not initialized")
	}
	return service.StopMicrophone()
}

// RPCAudioStatus handles audio status RPC requests (read-only)
func RPCAudioStatus() (map[string]interface{}, error) {
	if getAudioControlServiceFunc == nil {
		return nil, fmt.Errorf("audio control service not available")
	}
	service := getAudioControlServiceFunc()
	if service == nil {
		return nil, fmt.Errorf("audio control service not initialized")
	}
	return service.GetAudioStatus(), nil
}

// RPCAudioQualityPresets is deprecated - returns single optimal configuration
// Kept for backward compatibility with UI
func RPCAudioQualityPresets() (map[string]any, error) {
	// Return single optimal configuration as both preset and current
	current := GetAudioConfig()

	// Return empty presets map (UI will handle this gracefully)
	return map[string]any{
		"presets": map[string]any{},
		"current": current,
	}, nil
}

// RPCMicrophoneStatus handles microphone status RPC requests (read-only)
func RPCMicrophoneStatus() (map[string]interface{}, error) {
	if getAudioControlServiceFunc == nil {
		return nil, fmt.Errorf("audio control service not available")
	}
	service := getAudioControlServiceFunc()
	if service == nil {
		return nil, fmt.Errorf("audio control service not initialized")
	}
	return service.GetMicrophoneStatus(), nil
}

// RPCMicrophoneReset handles microphone reset RPC requests
func RPCMicrophoneReset() error {
	if getAudioControlServiceFunc == nil {
		return fmt.Errorf("audio control service not available")
	}
	service := getAudioControlServiceFunc()
	if service == nil {
		return fmt.Errorf("audio control service not initialized")
	}
	return service.ResetMicrophone()
}

// RPCMicrophoneMute handles microphone mute RPC requests
func RPCMicrophoneMute(muted bool) error {
	if getAudioControlServiceFunc == nil {
		return fmt.Errorf("audio control service not available")
	}
	service := getAudioControlServiceFunc()
	if service == nil {
		return fmt.Errorf("audio control service not initialized")
	}
	return service.MuteMicrophone(muted)
}
