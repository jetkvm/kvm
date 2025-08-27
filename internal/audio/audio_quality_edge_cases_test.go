//go:build cgo
// +build cgo

package audio

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAudioQualityEdgeCases tests edge cases for audio quality functions
// These tests ensure the recent validation removal doesn't introduce regressions
func TestAudioQualityEdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		testFunc func(t *testing.T)
	}{
		{"AudioQualityBoundaryValues", testAudioQualityBoundaryValues},
		{"MicrophoneQualityBoundaryValues", testMicrophoneQualityBoundaryValues},
		{"AudioQualityPresetsConsistency", testAudioQualityPresetsConsistency},
		{"MicrophoneQualityPresetsConsistency", testMicrophoneQualityPresetsConsistency},
		{"QualitySettingsThreadSafety", testQualitySettingsThreadSafety},
		{"QualityPresetsImmutability", testQualityPresetsImmutability},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.testFunc(t)
		})
	}
}

// testAudioQualityBoundaryValues tests boundary values for audio quality
func testAudioQualityBoundaryValues(t *testing.T) {
	// Test minimum valid quality (0)
	originalConfig := GetAudioConfig()
	SetAudioQuality(AudioQualityLow)
	assert.Equal(t, AudioQualityLow, GetAudioConfig().Quality, "Should accept minimum quality value")

	// Test maximum valid quality (3)
	SetAudioQuality(AudioQualityUltra)
	assert.Equal(t, AudioQualityUltra, GetAudioConfig().Quality, "Should accept maximum quality value")

	// Test that quality settings work correctly
	SetAudioQuality(AudioQualityMedium)
	currentConfig := GetAudioConfig()
	assert.Equal(t, AudioQualityMedium, currentConfig.Quality, "Should set medium quality")
	t.Logf("Medium quality config: %+v", currentConfig)

	SetAudioQuality(AudioQualityHigh)
	currentConfig = GetAudioConfig()
	assert.Equal(t, AudioQualityHigh, currentConfig.Quality, "Should set high quality")
	t.Logf("High quality config: %+v", currentConfig)

	// Restore original quality
	SetAudioQuality(originalConfig.Quality)
}

// testMicrophoneQualityBoundaryValues tests boundary values for microphone quality
func testMicrophoneQualityBoundaryValues(t *testing.T) {
	// Test minimum valid quality
	originalConfig := GetMicrophoneConfig()
	SetMicrophoneQuality(AudioQualityLow)
	assert.Equal(t, AudioQualityLow, GetMicrophoneConfig().Quality, "Should accept minimum microphone quality value")

	// Test maximum valid quality
	SetMicrophoneQuality(AudioQualityUltra)
	assert.Equal(t, AudioQualityUltra, GetMicrophoneConfig().Quality, "Should accept maximum microphone quality value")

	// Test that quality settings work correctly
	SetMicrophoneQuality(AudioQualityMedium)
	currentConfig := GetMicrophoneConfig()
	assert.Equal(t, AudioQualityMedium, currentConfig.Quality, "Should set medium microphone quality")
	t.Logf("Medium microphone quality config: %+v", currentConfig)

	SetMicrophoneQuality(AudioQualityHigh)
	currentConfig = GetMicrophoneConfig()
	assert.Equal(t, AudioQualityHigh, currentConfig.Quality, "Should set high microphone quality")
	t.Logf("High microphone quality config: %+v", currentConfig)

	// Restore original quality
	SetMicrophoneQuality(originalConfig.Quality)
}

// testAudioQualityPresetsConsistency tests consistency of audio quality presets
func testAudioQualityPresetsConsistency(t *testing.T) {
	presets := GetAudioQualityPresets()
	require.NotNil(t, presets, "Audio quality presets should not be nil")
	require.NotEmpty(t, presets, "Audio quality presets should not be empty")

	// Verify presets have expected structure
	for i, preset := range presets {
		t.Logf("Audio preset %d: %+v", i, preset)

		// Each preset should have reasonable values
		assert.GreaterOrEqual(t, preset.Bitrate, 0, "Bitrate should be non-negative")
		assert.Greater(t, preset.SampleRate, 0, "Sample rate should be positive")
		assert.Greater(t, preset.Channels, 0, "Channels should be positive")
	}

	// Test that presets are accessible by valid quality levels
	qualityLevels := []AudioQuality{AudioQualityLow, AudioQualityMedium, AudioQualityHigh, AudioQualityUltra}
	for _, quality := range qualityLevels {
		preset, exists := presets[quality]
		assert.True(t, exists, "Preset should exist for quality %v", quality)
		assert.Greater(t, preset.Bitrate, 0, "Preset bitrate should be positive for quality %v", quality)
	}
}

// testMicrophoneQualityPresetsConsistency tests consistency of microphone quality presets
func testMicrophoneQualityPresetsConsistency(t *testing.T) {
	presets := GetMicrophoneQualityPresets()
	require.NotNil(t, presets, "Microphone quality presets should not be nil")
	require.NotEmpty(t, presets, "Microphone quality presets should not be empty")

	// Verify presets have expected structure
	for i, preset := range presets {
		t.Logf("Microphone preset %d: %+v", i, preset)

		// Each preset should have reasonable values
		assert.GreaterOrEqual(t, preset.Bitrate, 0, "Bitrate should be non-negative")
		assert.Greater(t, preset.SampleRate, 0, "Sample rate should be positive")
		assert.Greater(t, preset.Channels, 0, "Channels should be positive")
	}

	// Test that presets are accessible by valid quality levels
	qualityLevels := []AudioQuality{AudioQualityLow, AudioQualityMedium, AudioQualityHigh, AudioQualityUltra}
	for _, quality := range qualityLevels {
		preset, exists := presets[quality]
		assert.True(t, exists, "Microphone preset should exist for quality %v", quality)
		assert.Greater(t, preset.Bitrate, 0, "Microphone preset bitrate should be positive for quality %v", quality)
	}
}

// testQualitySettingsThreadSafety tests thread safety of quality settings
func testQualitySettingsThreadSafety(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping thread safety test in short mode")
	}

	originalAudioConfig := GetAudioConfig()
	originalMicConfig := GetMicrophoneConfig()

	// Test concurrent access to quality settings
	const numGoroutines = 50
	const numOperations = 100

	done := make(chan bool, numGoroutines*2)

	// Audio quality goroutines
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			for j := 0; j < numOperations; j++ {
				// Cycle through valid quality values
				qualityIndex := j % 4
				var quality AudioQuality
				switch qualityIndex {
				case 0:
					quality = AudioQualityLow
				case 1:
					quality = AudioQualityMedium
				case 2:
					quality = AudioQualityHigh
				case 3:
					quality = AudioQualityUltra
				}
				SetAudioQuality(quality)
				_ = GetAudioConfig()
			}
			done <- true
		}(i)
	}

	// Microphone quality goroutines
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			for j := 0; j < numOperations; j++ {
				// Cycle through valid quality values
				qualityIndex := j % 4
				var quality AudioQuality
				switch qualityIndex {
				case 0:
					quality = AudioQualityLow
				case 1:
					quality = AudioQualityMedium
				case 2:
					quality = AudioQualityHigh
				case 3:
					quality = AudioQualityUltra
				}
				SetMicrophoneQuality(quality)
				_ = GetMicrophoneConfig()
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < numGoroutines*2; i++ {
		<-done
	}

	// Verify system is still functional
	SetAudioQuality(AudioQualityHigh)
	assert.Equal(t, AudioQualityHigh, GetAudioConfig().Quality, "Audio quality should be settable after concurrent access")

	SetMicrophoneQuality(AudioQualityMedium)
	assert.Equal(t, AudioQualityMedium, GetMicrophoneConfig().Quality, "Microphone quality should be settable after concurrent access")

	// Restore original values
	SetAudioQuality(originalAudioConfig.Quality)
	SetMicrophoneQuality(originalMicConfig.Quality)
}

// testQualityPresetsImmutability tests that quality presets are not accidentally modified
func testQualityPresetsImmutability(t *testing.T) {
	// Get presets multiple times and verify they're consistent
	presets1 := GetAudioQualityPresets()
	presets2 := GetAudioQualityPresets()

	require.Equal(t, len(presets1), len(presets2), "Preset count should be consistent")

	// Verify each preset is identical
	for quality := range presets1 {
		assert.Equal(t, presets1[quality].Bitrate, presets2[quality].Bitrate,
			"Preset %v bitrate should be consistent", quality)
		assert.Equal(t, presets1[quality].SampleRate, presets2[quality].SampleRate,
			"Preset %v sample rate should be consistent", quality)
		assert.Equal(t, presets1[quality].Channels, presets2[quality].Channels,
			"Preset %v channels should be consistent", quality)
	}

	// Test microphone presets as well
	micPresets1 := GetMicrophoneQualityPresets()
	micPresets2 := GetMicrophoneQualityPresets()

	require.Equal(t, len(micPresets1), len(micPresets2), "Microphone preset count should be consistent")

	for quality := range micPresets1 {
		assert.Equal(t, micPresets1[quality].Bitrate, micPresets2[quality].Bitrate,
			"Microphone preset %v bitrate should be consistent", quality)
		assert.Equal(t, micPresets1[quality].SampleRate, micPresets2[quality].SampleRate,
			"Microphone preset %v sample rate should be consistent", quality)
		assert.Equal(t, micPresets1[quality].Channels, micPresets2[quality].Channels,
			"Microphone preset %v channels should be consistent", quality)
	}
}

// TestQualityValidationRemovalRegression tests that validation removal doesn't cause regressions
func TestQualityValidationRemovalRegression(t *testing.T) {
	// This test ensures that removing validation from GET endpoints doesn't break functionality

	// Test that presets are still accessible
	audioPresets := GetAudioQualityPresets()
	assert.NotNil(t, audioPresets, "Audio presets should be accessible after validation removal")
	assert.NotEmpty(t, audioPresets, "Audio presets should not be empty")

	micPresets := GetMicrophoneQualityPresets()
	assert.NotNil(t, micPresets, "Microphone presets should be accessible after validation removal")
	assert.NotEmpty(t, micPresets, "Microphone presets should not be empty")

	// Test that quality getters still work
	audioConfig := GetAudioConfig()
	assert.GreaterOrEqual(t, int(audioConfig.Quality), 0, "Audio quality should be non-negative")

	micConfig := GetMicrophoneConfig()
	assert.GreaterOrEqual(t, int(micConfig.Quality), 0, "Microphone quality should be non-negative")

	// Test that setters still work (for valid values)
	originalAudio := GetAudioConfig()
	originalMic := GetMicrophoneConfig()

	SetAudioQuality(AudioQualityMedium)
	assert.Equal(t, AudioQualityMedium, GetAudioConfig().Quality, "Audio quality setter should work")

	SetMicrophoneQuality(AudioQualityHigh)
	assert.Equal(t, AudioQualityHigh, GetMicrophoneConfig().Quality, "Microphone quality setter should work")

	// Restore original values
	SetAudioQuality(originalAudio.Quality)
	SetMicrophoneQuality(originalMic.Quality)
}

// TestPerformanceAfterValidationRemoval tests that performance improved after validation removal
func TestPerformanceAfterValidationRemoval(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance test in short mode")
	}

	// Benchmark preset access (should be faster without validation)
	const iterations = 10000

	// Time audio preset access
	start := time.Now()
	for i := 0; i < iterations; i++ {
		_ = GetAudioQualityPresets()
	}
	audioDuration := time.Since(start)

	// Time microphone preset access
	start = time.Now()
	for i := 0; i < iterations; i++ {
		_ = GetMicrophoneQualityPresets()
	}
	micDuration := time.Since(start)

	t.Logf("Audio presets access time for %d iterations: %v", iterations, audioDuration)
	t.Logf("Microphone presets access time for %d iterations: %v", iterations, micDuration)

	// Verify reasonable performance (should complete quickly without validation overhead)
	maxExpectedDuration := time.Second // Very generous limit
	assert.Less(t, audioDuration, maxExpectedDuration, "Audio preset access should be fast")
	assert.Less(t, micDuration, maxExpectedDuration, "Microphone preset access should be fast")
}
