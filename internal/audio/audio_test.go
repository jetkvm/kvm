package audio

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
