package audio

import (
	"errors"
	"sync/atomic"
	"time"
)

var (
	ErrAudioAlreadyRunning = errors.New("audio already running")
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
	currentMicrophoneConfig = AudioConfig{
		Quality:    AudioQualityMedium,
		Bitrate:    32,
		SampleRate: 48000,
		Channels:   1,
		FrameSize:  20 * time.Millisecond,
	}
	metrics AudioMetrics
)

// qualityPresets defines the base quality configurations
var qualityPresets = map[AudioQuality]struct {
	outputBitrate, inputBitrate int
	sampleRate, channels        int
	frameSize                   time.Duration
}{
	AudioQualityLow: {
		outputBitrate: 32, inputBitrate: 16,
		sampleRate: 22050, channels: 1,
		frameSize: 40 * time.Millisecond,
	},
	AudioQualityMedium: {
		outputBitrate: 64, inputBitrate: 32,
		sampleRate: 44100, channels: 2,
		frameSize: 20 * time.Millisecond,
	},
	AudioQualityHigh: {
		outputBitrate: 128, inputBitrate: 64,
		sampleRate: 48000, channels: 2,
		frameSize: 20 * time.Millisecond,
	},
	AudioQualityUltra: {
		outputBitrate: 192, inputBitrate: 96,
		sampleRate: 48000, channels: 2,
		frameSize: 10 * time.Millisecond,
	},
}

// GetAudioQualityPresets returns predefined quality configurations for audio output
func GetAudioQualityPresets() map[AudioQuality]AudioConfig {
	result := make(map[AudioQuality]AudioConfig)
	for quality, preset := range qualityPresets {
		result[quality] = AudioConfig{
			Quality:    quality,
			Bitrate:    preset.outputBitrate,
			SampleRate: preset.sampleRate,
			Channels:   preset.channels,
			FrameSize:  preset.frameSize,
		}
	}
	return result
}

// GetMicrophoneQualityPresets returns predefined quality configurations for microphone input
func GetMicrophoneQualityPresets() map[AudioQuality]AudioConfig {
	result := make(map[AudioQuality]AudioConfig)
	for quality, preset := range qualityPresets {
		result[quality] = AudioConfig{
			Quality: quality,
			Bitrate: preset.inputBitrate,
			SampleRate: func() int {
				if quality == AudioQualityLow {
					return 16000
				}
				return preset.sampleRate
			}(),
			Channels:  1, // Microphone is always mono
			FrameSize: preset.frameSize,
		}
	}
	return result
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

// SetMicrophoneQuality updates the current microphone quality configuration
func SetMicrophoneQuality(quality AudioQuality) {
	presets := GetMicrophoneQualityPresets()
	if config, exists := presets[quality]; exists {
		currentMicrophoneConfig = config
	}
}

// GetMicrophoneConfig returns the current microphone configuration
func GetMicrophoneConfig() AudioConfig {
	return currentMicrophoneConfig
}

// GetAudioMetrics returns current audio metrics
func GetAudioMetrics() AudioMetrics {
	// Get base metrics
	framesReceived := atomic.LoadInt64(&metrics.FramesReceived)
	framesDropped := atomic.LoadInt64(&metrics.FramesDropped)

	// If audio relay is running, use relay stats instead
	if IsAudioRelayRunning() {
		relayReceived, relayDropped := GetAudioRelayStats()
		framesReceived = relayReceived
		framesDropped = relayDropped
	}

	return AudioMetrics{
		FramesReceived:  framesReceived,
		FramesDropped:   framesDropped,
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
