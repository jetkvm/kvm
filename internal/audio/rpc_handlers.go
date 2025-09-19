package audio

import (
	"fmt"
)

// RPC wrapper functions for audio control
// These functions bridge the RPC layer to the AudioControlService

// These variables will be set by the main package to provide access to the global service
var (
	getAudioControlServiceFunc func() *AudioControlService
	getAudioQualityFunc        func() AudioConfig
	setAudioQualityFunc        func(AudioQuality) error
)

// SetRPCCallbacks sets the callback functions for RPC operations
func SetRPCCallbacks(
	getService func() *AudioControlService,
	getQuality func() AudioConfig,
	setQuality func(AudioQuality) error,
) {
	getAudioControlServiceFunc = getService
	getAudioQualityFunc = getQuality
	setAudioQualityFunc = setQuality
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

// RPCAudioQuality handles audio quality change RPC requests
func RPCAudioQuality(quality int) (map[string]any, error) {
	if getAudioQualityFunc == nil || setAudioQualityFunc == nil {
		return nil, fmt.Errorf("audio quality functions not available")
	}

	// Convert int to AudioQuality type
	audioQuality := AudioQuality(quality)

	// Get current audio quality configuration
	currentConfig := getAudioQualityFunc()

	// Set new quality if different
	if currentConfig.Quality != audioQuality {
		err := setAudioQualityFunc(audioQuality)
		if err != nil {
			return nil, fmt.Errorf("failed to set audio quality: %w", err)
		}
		// Get updated config after setting
		newConfig := getAudioQualityFunc()
		return map[string]any{"config": newConfig}, nil
	}

	// Return current config if no change needed
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

// RPCAudioQualityPresets handles audio quality presets RPC requests (read-only)
func RPCAudioQualityPresets() (map[string]any, error) {
	if getAudioControlServiceFunc == nil || getAudioQualityFunc == nil {
		return nil, fmt.Errorf("audio control service not available")
	}
	service := getAudioControlServiceFunc()
	if service == nil {
		return nil, fmt.Errorf("audio control service not initialized")
	}

	presets := service.GetAudioQualityPresets()
	current := getAudioQualityFunc()

	return map[string]any{
		"presets": presets,
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
