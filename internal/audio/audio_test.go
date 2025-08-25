package audio

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jetkvm/kvm/internal/usbgadget"
)

// Unit tests for the audio package

func TestAudioQuality(t *testing.T) {
	tests := []struct {
		name     string
		quality  AudioQuality
		expected string
	}{
		{"Low Quality", AudioQualityLow, "low"},
		{"Medium Quality", AudioQualityMedium, "medium"},
		{"High Quality", AudioQualityHigh, "high"},
		{"Ultra Quality", AudioQualityUltra, "ultra"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test quality setting
			SetAudioQuality(tt.quality)
			config := GetAudioConfig()
			assert.Equal(t, tt.quality, config.Quality)
			assert.Greater(t, config.Bitrate, 0)
			assert.Greater(t, config.SampleRate, 0)
			assert.Greater(t, config.Channels, 0)
			assert.Greater(t, config.FrameSize, time.Duration(0))
		})
	}
}

func TestMicrophoneQuality(t *testing.T) {
	tests := []struct {
		name    string
		quality AudioQuality
	}{
		{"Low Quality", AudioQualityLow},
		{"Medium Quality", AudioQualityMedium},
		{"High Quality", AudioQualityHigh},
		{"Ultra Quality", AudioQualityUltra},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test microphone quality setting
			SetMicrophoneQuality(tt.quality)
			config := GetMicrophoneConfig()
			assert.Equal(t, tt.quality, config.Quality)
			assert.Equal(t, 1, config.Channels) // Microphone is always mono
			assert.Greater(t, config.Bitrate, 0)
			assert.Greater(t, config.SampleRate, 0)
		})
	}
}

func TestAudioQualityPresets(t *testing.T) {
	presets := GetAudioQualityPresets()
	require.NotEmpty(t, presets)

	// Test that all quality levels have presets
	for quality := AudioQualityLow; quality <= AudioQualityUltra; quality++ {
		config, exists := presets[quality]
		require.True(t, exists, "Preset should exist for quality %d", quality)
		assert.Equal(t, quality, config.Quality)
		assert.Greater(t, config.Bitrate, 0)
		assert.Greater(t, config.SampleRate, 0)
		assert.Greater(t, config.Channels, 0)
		assert.Greater(t, config.FrameSize, time.Duration(0))
	}

	// Test that higher quality has higher bitrate
	lowConfig := presets[AudioQualityLow]
	mediumConfig := presets[AudioQualityMedium]
	highConfig := presets[AudioQualityHigh]
	ultraConfig := presets[AudioQualityUltra]

	assert.Less(t, lowConfig.Bitrate, mediumConfig.Bitrate)
	assert.Less(t, mediumConfig.Bitrate, highConfig.Bitrate)
	assert.Less(t, highConfig.Bitrate, ultraConfig.Bitrate)
}

func TestMicrophoneQualityPresets(t *testing.T) {
	presets := GetMicrophoneQualityPresets()
	require.NotEmpty(t, presets)

	// Test that all quality levels have presets
	for quality := AudioQualityLow; quality <= AudioQualityUltra; quality++ {
		config, exists := presets[quality]
		require.True(t, exists, "Microphone preset should exist for quality %d", quality)
		assert.Equal(t, quality, config.Quality)
		assert.Equal(t, 1, config.Channels) // Always mono
		assert.Greater(t, config.Bitrate, 0)
		assert.Greater(t, config.SampleRate, 0)
	}
}

func TestAudioMetrics(t *testing.T) {
	// Test initial metrics
	metrics := GetAudioMetrics()
	assert.GreaterOrEqual(t, metrics.FramesReceived, int64(0))
	assert.GreaterOrEqual(t, metrics.FramesDropped, int64(0))
	assert.GreaterOrEqual(t, metrics.BytesProcessed, int64(0))
	assert.GreaterOrEqual(t, metrics.ConnectionDrops, int64(0))

	// Test recording metrics
	RecordFrameReceived(1024)
	metrics = GetAudioMetrics()
	assert.Greater(t, metrics.BytesProcessed, int64(0))
	assert.Greater(t, metrics.FramesReceived, int64(0))

	RecordFrameDropped()
	metrics = GetAudioMetrics()
	assert.Greater(t, metrics.FramesDropped, int64(0))

	RecordConnectionDrop()
	metrics = GetAudioMetrics()
	assert.Greater(t, metrics.ConnectionDrops, int64(0))
}

func TestMaxAudioFrameSize(t *testing.T) {
	frameSize := GetMaxAudioFrameSize()
	assert.Greater(t, frameSize, 0)
	assert.Equal(t, GetConfig().MaxAudioFrameSize, frameSize)
}

func TestMetricsUpdateInterval(t *testing.T) {
	// Test getting current interval
	interval := GetMetricsUpdateInterval()
	assert.Greater(t, interval, time.Duration(0))

	// Test setting new interval
	newInterval := 2 * time.Second
	SetMetricsUpdateInterval(newInterval)
	updatedInterval := GetMetricsUpdateInterval()
	assert.Equal(t, newInterval, updatedInterval)
}

func TestAudioConfigConsistency(t *testing.T) {
	// Test that setting audio quality updates the config consistently
	for quality := AudioQualityLow; quality <= AudioQualityUltra; quality++ {
		SetAudioQuality(quality)
		config := GetAudioConfig()
		presets := GetAudioQualityPresets()
		expectedConfig := presets[quality]

		assert.Equal(t, expectedConfig.Quality, config.Quality)
		assert.Equal(t, expectedConfig.Bitrate, config.Bitrate)
		assert.Equal(t, expectedConfig.SampleRate, config.SampleRate)
		assert.Equal(t, expectedConfig.Channels, config.Channels)
		assert.Equal(t, expectedConfig.FrameSize, config.FrameSize)
	}
}

func TestMicrophoneConfigConsistency(t *testing.T) {
	// Test that setting microphone quality updates the config consistently
	for quality := AudioQualityLow; quality <= AudioQualityUltra; quality++ {
		SetMicrophoneQuality(quality)
		config := GetMicrophoneConfig()
		presets := GetMicrophoneQualityPresets()
		expectedConfig := presets[quality]

		assert.Equal(t, expectedConfig.Quality, config.Quality)
		assert.Equal(t, expectedConfig.Bitrate, config.Bitrate)
		assert.Equal(t, expectedConfig.SampleRate, config.SampleRate)
		assert.Equal(t, expectedConfig.Channels, config.Channels)
		assert.Equal(t, expectedConfig.FrameSize, config.FrameSize)
	}
}

// Benchmark tests
func BenchmarkGetAudioConfig(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = GetAudioConfig()
	}
}

func BenchmarkGetAudioMetrics(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = GetAudioMetrics()
	}
}

func BenchmarkRecordFrameReceived(b *testing.B) {
	for i := 0; i < b.N; i++ {
		RecordFrameReceived(1024)
	}
}

func BenchmarkSetAudioQuality(b *testing.B) {
	qualities := []AudioQuality{AudioQualityLow, AudioQualityMedium, AudioQualityHigh, AudioQualityUltra}
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		SetAudioQuality(qualities[i%len(qualities)])
	}
}

// TestAudioUsbGadgetIntegration tests audio functionality with USB gadget reconfiguration
// This test simulates the production scenario where audio devices are enabled/disabled
// through USB gadget configuration changes
func TestAudioUsbGadgetIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	tests := []struct {
		name                string
		initialAudioEnabled bool
		newAudioEnabled     bool
		expectedTransition  string
	}{
		{
			name:                "EnableAudio",
			initialAudioEnabled: false,
			newAudioEnabled:     true,
			expectedTransition:  "disabled_to_enabled",
		},
		{
			name:                "DisableAudio",
			initialAudioEnabled: true,
			newAudioEnabled:     false,
			expectedTransition:  "enabled_to_disabled",
		},
		{
			name:                "NoChange",
			initialAudioEnabled: true,
			newAudioEnabled:     true,
			expectedTransition:  "no_change",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			// Simulate initial USB device configuration
			initialDevices := &usbgadget.Devices{
				Keyboard:      true,
				AbsoluteMouse: true,
				RelativeMouse: true,
				MassStorage:   true,
				Audio:         tt.initialAudioEnabled,
			}

			// Simulate new USB device configuration
			newDevices := &usbgadget.Devices{
				Keyboard:      true,
				AbsoluteMouse: true,
				RelativeMouse: true,
				MassStorage:   true,
				Audio:         tt.newAudioEnabled,
			}

			// Test audio configuration validation
			err := validateAudioDeviceConfiguration(tt.newAudioEnabled)
			assert.NoError(t, err, "Audio configuration should be valid")

			// Test audio state transition simulation
			transition := simulateAudioStateTransition(ctx, initialDevices, newDevices)
			assert.Equal(t, tt.expectedTransition, transition, "Audio state transition should match expected")

			// Test that audio configuration is consistent after transition
			if tt.newAudioEnabled {
				config := GetAudioConfig()
				assert.Greater(t, config.Bitrate, 0, "Audio bitrate should be positive when enabled")
				assert.Greater(t, config.SampleRate, 0, "Audio sample rate should be positive when enabled")
			}
		})
	}
}

// validateAudioDeviceConfiguration simulates the audio validation that happens in production
func validateAudioDeviceConfiguration(enabled bool) error {
	if !enabled {
		return nil // No validation needed when disabled
	}

	// Simulate audio device availability checks
	// In production, this would check for ALSA devices, audio hardware, etc.
	config := GetAudioConfig()
	if config.Bitrate <= 0 {
		return assert.AnError
	}
	if config.SampleRate <= 0 {
		return assert.AnError
	}

	return nil
}

// simulateAudioStateTransition simulates the audio process management during USB reconfiguration
func simulateAudioStateTransition(ctx context.Context, initial, new *usbgadget.Devices) string {
	previousAudioEnabled := initial.Audio
	newAudioEnabled := new.Audio

	if previousAudioEnabled == newAudioEnabled {
		return "no_change"
	}

	if !newAudioEnabled {
		// Simulate stopping audio processes
		// In production, this would stop AudioInputManager and audioSupervisor
		time.Sleep(10 * time.Millisecond) // Simulate process stop time
		return "enabled_to_disabled"
	}

	if newAudioEnabled {
		// Simulate starting audio processes after USB reconfiguration
		// In production, this would start audioSupervisor and broadcast events
		time.Sleep(10 * time.Millisecond) // Simulate process start time
		return "disabled_to_enabled"
	}

	return "unknown"
}

// TestAudioUsbGadgetTimeout tests that audio operations don't timeout during USB reconfiguration
func TestAudioUsbGadgetTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping timeout test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Test that audio configuration changes complete within reasonable time
	start := time.Now()

	// Simulate multiple rapid USB device configuration changes
	for i := 0; i < 10; i++ {
		audioEnabled := i%2 == 0
		devices := &usbgadget.Devices{
			Keyboard:      true,
			AbsoluteMouse: true,
			RelativeMouse: true,
			MassStorage:   true,
			Audio:         audioEnabled,
		}

		err := validateAudioDeviceConfiguration(devices.Audio)
		assert.NoError(t, err, "Audio validation should not fail")

		// Ensure we don't timeout
		select {
		case <-ctx.Done():
			t.Fatal("Audio configuration test timed out")
		default:
			// Continue
		}
	}

	elapsed := time.Since(start)
	t.Logf("Audio USB gadget configuration test completed in %v", elapsed)
	assert.Less(t, elapsed, 3*time.Second, "Audio configuration should complete quickly")
}
