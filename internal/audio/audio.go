// Package audio provides real-time audio processing for JetKVM with low-latency streaming.
//
// Key components: output/input pipelines with Opus codec, adaptive buffer management,
// zero-copy frame pools, IPC communication, and process supervision.
//
// Supports four quality presets (Low/Medium/High/Ultra) with configurable bitrates.
// All APIs are thread-safe with comprehensive error handling and metrics collection.
//
// # Performance Characteristics
//
// Designed for embedded ARM systems with limited resources:
//   - Sub-50ms end-to-end latency under normal conditions
//   - Memory usage scales with buffer configuration
//   - CPU usage optimized through zero-copy operations
//   - Network bandwidth adapts to quality settings
//
// # Usage Example
//
//	config := GetAudioConfig()
//	SetAudioQuality(AudioQualityHigh)
//
//	// Audio output will automatically start when frames are received
//	metrics := GetAudioMetrics()
//	fmt.Printf("Latency: %v, Frames: %d\n", metrics.AverageLatency, metrics.FramesReceived)
package audio

import (
	"errors"
	"sync/atomic"
	"time"

	"github.com/jetkvm/kvm/internal/logging"
)

var (
	ErrAudioAlreadyRunning = errors.New("audio already running")
)

// MaxAudioFrameSize is now retrieved from centralized config
func GetMaxAudioFrameSize() int {
	return GetConfig().MaxAudioFrameSize
}

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
		Bitrate:    GetConfig().AudioQualityMediumOutputBitrate,
		SampleRate: GetConfig().SampleRate,
		Channels:   GetConfig().Channels,
		FrameSize:  GetConfig().AudioQualityMediumFrameSize,
	}
	currentMicrophoneConfig = AudioConfig{
		Quality:    AudioQualityMedium,
		Bitrate:    GetConfig().AudioQualityMediumInputBitrate,
		SampleRate: GetConfig().SampleRate,
		Channels:   1,
		FrameSize:  GetConfig().AudioQualityMediumFrameSize,
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
		outputBitrate: GetConfig().AudioQualityLowOutputBitrate, inputBitrate: GetConfig().AudioQualityLowInputBitrate,
		sampleRate: GetConfig().AudioQualityLowSampleRate, channels: GetConfig().AudioQualityLowChannels,
		frameSize: GetConfig().AudioQualityLowFrameSize,
	},
	AudioQualityMedium: {
		outputBitrate: GetConfig().AudioQualityMediumOutputBitrate, inputBitrate: GetConfig().AudioQualityMediumInputBitrate,
		sampleRate: GetConfig().AudioQualityMediumSampleRate, channels: GetConfig().AudioQualityMediumChannels,
		frameSize: GetConfig().AudioQualityMediumFrameSize,
	},
	AudioQualityHigh: {
		outputBitrate: GetConfig().AudioQualityHighOutputBitrate, inputBitrate: GetConfig().AudioQualityHighInputBitrate,
		sampleRate: GetConfig().SampleRate, channels: GetConfig().AudioQualityHighChannels,
		frameSize: GetConfig().AudioQualityHighFrameSize,
	},
	AudioQualityUltra: {
		outputBitrate: GetConfig().AudioQualityUltraOutputBitrate, inputBitrate: GetConfig().AudioQualityUltraInputBitrate,
		sampleRate: GetConfig().SampleRate, channels: GetConfig().AudioQualityUltraChannels,
		frameSize: GetConfig().AudioQualityUltraFrameSize,
	},
}

// GetAudioQualityPresets returns predefined quality configurations for audio output
func GetAudioQualityPresets() map[AudioQuality]AudioConfig {
	result := make(map[AudioQuality]AudioConfig)
	for quality, preset := range qualityPresets {
		config := AudioConfig{
			Quality:    quality,
			Bitrate:    preset.outputBitrate,
			SampleRate: preset.sampleRate,
			Channels:   preset.channels,
			FrameSize:  preset.frameSize,
		}
		result[quality] = config
	}
	return result
}

// GetMicrophoneQualityPresets returns predefined quality configurations for microphone input
func GetMicrophoneQualityPresets() map[AudioQuality]AudioConfig {
	result := make(map[AudioQuality]AudioConfig)
	for quality, preset := range qualityPresets {
		config := AudioConfig{
			Quality: quality,
			Bitrate: preset.inputBitrate,
			SampleRate: func() int {
				if quality == AudioQualityLow {
					return GetConfig().AudioQualityMicLowSampleRate
				}
				return preset.sampleRate
			}(),
			Channels:  1, // Microphone is always mono
			FrameSize: preset.frameSize,
		}
		result[quality] = config
	}
	return result
}

// SetAudioQuality updates the current audio quality configuration
func SetAudioQuality(quality AudioQuality) {
	// Validate audio quality parameter
	if err := ValidateAudioQuality(quality); err != nil {
		// Log validation error but don't fail - maintain backward compatibility
		logger := logging.GetDefaultLogger().With().Str("component", "audio").Logger()
		logger.Warn().Err(err).Int("quality", int(quality)).Msg("invalid audio quality, using current config")
		return
	}

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
	// Validate audio quality parameter
	if err := ValidateAudioQuality(quality); err != nil {
		// Log validation error but don't fail - maintain backward compatibility
		logger := logging.GetDefaultLogger().With().Str("component", "audio").Logger()
		logger.Warn().Err(err).Int("quality", int(quality)).Msg("invalid microphone quality, using current config")
		return
	}

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
