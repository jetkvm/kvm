//go:build cgo
// +build cgo

package audio

import (
	_ "embed"
	"fmt"
	"os"
)

// Embedded C audio binaries (built during compilation)
//
//go:embed bin/jetkvm_audio_output
var audioOutputBinary []byte

//go:embed bin/jetkvm_audio_input
var audioInputBinary []byte

const (
	audioBinDir         = "/userdata/jetkvm/bin"
	audioOutputBinPath  = audioBinDir + "/jetkvm_audio_output"
	audioInputBinPath   = audioBinDir + "/jetkvm_audio_input"
	binaryFileMode      = 0755 // rwxr-xr-x
)

// ExtractEmbeddedBinaries extracts the embedded C audio binaries to disk
// This should be called during application startup before audio supervisors are started
func ExtractEmbeddedBinaries() error {
	// Create bin directory if it doesn't exist
	if err := os.MkdirAll(audioBinDir, 0755); err != nil {
		return fmt.Errorf("failed to create audio bin directory: %w", err)
	}

	// Extract audio output binary
	if err := extractBinary(audioOutputBinary, audioOutputBinPath); err != nil {
		return fmt.Errorf("failed to extract audio output binary: %w", err)
	}

	// Extract audio input binary
	if err := extractBinary(audioInputBinary, audioInputBinPath); err != nil {
		return fmt.Errorf("failed to extract audio input binary: %w", err)
	}

	return nil
}

// extractBinary writes embedded binary data to disk with executable permissions
func extractBinary(data []byte, path string) error {
	// Check if binary already exists and is valid
	if info, err := os.Stat(path); err == nil {
		// File exists - check if size matches
		if info.Size() == int64(len(data)) {
			// Binary already extracted and matches embedded version
			return nil
		}
		// Size mismatch - need to update
	}

	// Write to temporary file first for atomic replacement
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, binaryFileMode); err != nil {
		return fmt.Errorf("failed to write binary to %s: %w", tmpPath, err)
	}

	// Atomically rename to final path
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath) // Clean up on error
		return fmt.Errorf("failed to rename binary to %s: %w", path, err)
	}

	return nil
}

// GetAudioOutputBinaryPath returns the path to the audio output binary
func GetAudioOutputBinaryPath() string {
	return audioOutputBinPath
}

// GetAudioInputBinaryPath returns the path to the audio input binary
func GetAudioInputBinaryPath() string {
	return audioInputBinPath
}

// CleanupBinaries removes extracted audio binaries (useful for cleanup/testing)
func CleanupBinaries() error {
	var errs []error

	if err := os.Remove(audioOutputBinPath); err != nil && !os.IsNotExist(err) {
		errs = append(errs, fmt.Errorf("failed to remove audio output binary: %w", err))
	}

	if err := os.Remove(audioInputBinPath); err != nil && !os.IsNotExist(err) {
		errs = append(errs, fmt.Errorf("failed to remove audio input binary: %w", err))
	}

	// Try to remove directory (will only succeed if empty)
	os.Remove(audioBinDir)

	if len(errs) > 0 {
		return fmt.Errorf("cleanup errors: %v", errs)
	}

	return nil
}

// GetBinaryInfo returns information about embedded binaries
func GetBinaryInfo() map[string]int {
	return map[string]int{
		"audio_output_size": len(audioOutputBinary),
		"audio_input_size":  len(audioInputBinary),
	}
}

// init ensures binaries are extracted when package is imported
func init() {
	// Extract binaries on package initialization
	// This ensures binaries are available before supervisors start
	if err := ExtractEmbeddedBinaries(); err != nil {
		// Log error but don't panic - let caller handle initialization failure
		fmt.Fprintf(os.Stderr, "Warning: Failed to extract embedded audio binaries: %v\n", err)
	}
}
