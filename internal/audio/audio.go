package audio

import (
	"sync/atomic"
	"time"
	// Explicit import for CGO audio stream glue
)

const MaxAudioFrameSize = 1500

// AudioQuality represents different audio quality presets
type AudioQuality int

const (
	AudioQualityLow AudioQuality = iota
	AudioQualityMedium
	AudioQualityHigh
	AudioQualityUltra
)

// AudioConfig holds configuration for audio processing
type AudioConfig struct {
	Quality    AudioQuality
	Bitrate    int // kbps
	SampleRate int // Hz
	Channels   int
	FrameSize  time.Duration // ms
}

// AudioMetrics tracks audio performance metrics
// Note: 64-bit fields must be first for proper alignment on 32-bit ARM
type AudioMetrics struct {
	FramesReceived  int64
	FramesDropped   int64
	BytesProcessed  int64
	ConnectionDrops int64
	LastFrameTime   time.Time
	AverageLatency  time.Duration
}

var (
	currentConfig = AudioConfig{
		Quality:    AudioQualityMedium,
		Bitrate:    64,
		SampleRate: 48000,
		Channels:   2,
		FrameSize:  20 * time.Millisecond,
	}
	metrics AudioMetrics
)

// GetAudioQualityPresets returns predefined quality configurations
func GetAudioQualityPresets() map[AudioQuality]AudioConfig {
	return map[AudioQuality]AudioConfig{
		AudioQualityLow: {
			Quality:    AudioQualityLow,
			Bitrate:    32,
			SampleRate: 48000,
			Channels:   2,
			FrameSize:  20 * time.Millisecond,
		},
		AudioQualityMedium: {
			Quality:    AudioQualityMedium,
			Bitrate:    64,
			SampleRate: 48000,
			Channels:   2,
			FrameSize:  20 * time.Millisecond,
		},
		AudioQualityHigh: {
			Quality:    AudioQualityHigh,
			Bitrate:    128,
			SampleRate: 48000,
			Channels:   2,
			FrameSize:  20 * time.Millisecond,
		},
		AudioQualityUltra: {
			Quality:    AudioQualityUltra,
			Bitrate:    256,
			SampleRate: 48000,
			Channels:   2,
			FrameSize:  10 * time.Millisecond,
		},
	}
}

// SetAudioQuality updates the current audio quality configuration
func SetAudioQuality(quality AudioQuality) {
	presets := GetAudioQualityPresets()
	if config, exists := presets[quality]; exists {
		currentConfig = config
	}
}

// GetAudioConfig returns the current audio configuration
func GetAudioConfig() AudioConfig {
	return currentConfig
}

// GetAudioMetrics returns current audio metrics
func GetAudioMetrics() AudioMetrics {
	return AudioMetrics{
		FramesReceived:  atomic.LoadInt64(&metrics.FramesReceived),
		FramesDropped:   atomic.LoadInt64(&metrics.FramesDropped),
		BytesProcessed:  atomic.LoadInt64(&metrics.BytesProcessed),
		LastFrameTime:   metrics.LastFrameTime,
		ConnectionDrops: atomic.LoadInt64(&metrics.ConnectionDrops),
		AverageLatency:  metrics.AverageLatency,
	}
}

// RecordFrameReceived increments the frames received counter
func RecordFrameReceived(bytes int) {
	atomic.AddInt64(&metrics.FramesReceived, 1)
	atomic.AddInt64(&metrics.BytesProcessed, int64(bytes))
	metrics.LastFrameTime = time.Now()
}

// RecordFrameDropped increments the frames dropped counter
func RecordFrameDropped() {
	atomic.AddInt64(&metrics.FramesDropped, 1)
}

// RecordConnectionDrop increments the connection drops counter
func RecordConnectionDrop() {
	atomic.AddInt64(&metrics.ConnectionDrops, 1)
}
