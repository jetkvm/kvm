// Package audio provides a comprehensive real-time audio processing system for JetKVM.
//
// # Architecture Overview
//
// The audio package implements a multi-component architecture designed for low-latency,
// high-quality audio streaming in embedded ARM environments. The system consists of:
//
//   - Audio Output Pipeline: Receives compressed audio frames, decodes via Opus, and
//     outputs to ALSA-compatible audio devices
//   - Audio Input Pipeline: Captures microphone input, encodes via Opus, and streams
//     to connected clients
//   - Adaptive Buffer Management: Dynamically adjusts buffer sizes based on system
//     load and latency requirements
//   - Zero-Copy Frame Pool: Minimizes memory allocations through frame reuse
//   - IPC Communication: Unix domain sockets for inter-process communication
//   - Process Supervision: Automatic restart and health monitoring of audio subprocesses
//
// # Key Components
//
// ## Buffer Pool System (buffer_pool.go)
// Implements a two-tier buffer pool with separate pools for audio frames and control
// messages. Uses sync.Pool for efficient memory reuse and tracks allocation statistics.
//
// ## Zero-Copy Frame Management (zero_copy.go)
// Provides reference-counted audio frames that can be shared between components
// without copying data. Includes automatic cleanup and pool-based allocation.
//
// ## Adaptive Buffering Algorithm (adaptive_buffer.go)
// Dynamically adjusts buffer sizes based on:
//   - System CPU and memory usage
//   - Audio latency measurements
//   - Frame drop rates
//   - Network conditions
//
// The algorithm uses exponential smoothing and configurable thresholds to balance
// latency and stability. Buffer sizes are adjusted in discrete steps to prevent
// oscillation.
//
// ## Latency Monitoring (latency_monitor.go)
// Tracks end-to-end audio latency using high-resolution timestamps. Implements
// adaptive optimization that adjusts system parameters when latency exceeds
// configured thresholds.
//
// ## Process Supervision (supervisor.go)
// Manages audio subprocess lifecycle with automatic restart capabilities.
// Monitors process health and implements exponential backoff for restart attempts.
//
// # Quality Levels
//
// The system supports four quality presets optimized for different use cases:
//   - Low: 32kbps output, 16kbps input - minimal bandwidth, voice-optimized
//   - Medium: 96kbps output, 64kbps input - balanced quality and bandwidth
//   - High: 192kbps output, 128kbps input - high quality for music
//   - Ultra: 320kbps output, 256kbps input - maximum quality
//
// # Configuration System
//
// All configuration is centralized in config_constants.go, allowing runtime
// tuning of performance parameters. Key configuration areas include:
//   - Opus codec parameters (bitrate, complexity, VBR settings)
//   - Buffer sizes and pool configurations
//   - Latency thresholds and optimization parameters
//   - Process monitoring and restart policies
//
// # Thread Safety
//
// All public APIs are thread-safe. Internal synchronization uses:
//   - atomic operations for performance counters
//   - sync.RWMutex for configuration updates
//   - sync.Pool for buffer management
//   - channel-based communication for IPC
//
// # Error Handling
//
// The system implements comprehensive error handling with:
//   - Graceful degradation on component failures
//   - Automatic retry with exponential backoff
//   - Detailed error context for debugging
//   - Metrics collection for monitoring
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
					return GetConfig().AudioQualityMicLowSampleRate
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
