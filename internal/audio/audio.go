//go:build cgo
// +build cgo

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

		// Get OPUS encoder parameters based on quality
		var complexity, vbr, signalType, bandwidth, dtx int
		switch quality {
		case AudioQualityLow:
			complexity = GetConfig().AudioQualityLowOpusComplexity
			vbr = GetConfig().AudioQualityLowOpusVBR
			signalType = GetConfig().AudioQualityLowOpusSignalType
			bandwidth = GetConfig().AudioQualityLowOpusBandwidth
			dtx = GetConfig().AudioQualityLowOpusDTX
		case AudioQualityMedium:
			complexity = GetConfig().AudioQualityMediumOpusComplexity
			vbr = GetConfig().AudioQualityMediumOpusVBR
			signalType = GetConfig().AudioQualityMediumOpusSignalType
			bandwidth = GetConfig().AudioQualityMediumOpusBandwidth
			dtx = GetConfig().AudioQualityMediumOpusDTX
		case AudioQualityHigh:
			complexity = GetConfig().AudioQualityHighOpusComplexity
			vbr = GetConfig().AudioQualityHighOpusVBR
			signalType = GetConfig().AudioQualityHighOpusSignalType
			bandwidth = GetConfig().AudioQualityHighOpusBandwidth
			dtx = GetConfig().AudioQualityHighOpusDTX
		case AudioQualityUltra:
			complexity = GetConfig().AudioQualityUltraOpusComplexity
			vbr = GetConfig().AudioQualityUltraOpusVBR
			signalType = GetConfig().AudioQualityUltraOpusSignalType
			bandwidth = GetConfig().AudioQualityUltraOpusBandwidth
			dtx = GetConfig().AudioQualityUltraOpusDTX
		default:
			// Use medium quality as fallback
			complexity = GetConfig().AudioQualityMediumOpusComplexity
			vbr = GetConfig().AudioQualityMediumOpusVBR
			signalType = GetConfig().AudioQualityMediumOpusSignalType
			bandwidth = GetConfig().AudioQualityMediumOpusBandwidth
			dtx = GetConfig().AudioQualityMediumOpusDTX
		}

		// Restart audio output subprocess with new OPUS configuration
		if supervisor := GetAudioOutputSupervisor(); supervisor != nil {
			logger := logging.GetDefaultLogger().With().Str("component", "audio").Logger()
			logger.Info().Int("quality", int(quality)).Msg("restarting audio output subprocess with new quality settings")

			// Set new OPUS configuration
			supervisor.SetOpusConfig(config.Bitrate*1000, complexity, vbr, signalType, bandwidth, dtx)

			// Stop current subprocess
			supervisor.Stop()

			// Start subprocess with new configuration
			if err := supervisor.Start(); err != nil {
				logger.Error().Err(err).Msg("failed to restart audio output subprocess")
			}
		} else {
			// Fallback to dynamic update if supervisor is not available
			vbrConstraint := GetConfig().CGOOpusVBRConstraint
			if err := updateOpusEncoderParams(config.Bitrate*1000, complexity, vbr, vbrConstraint, signalType, bandwidth, dtx); err != nil {
				logging.GetDefaultLogger().Error().Err(err).Msg("Failed to update OPUS encoder parameters")
			}
		}
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

		// Get OPUS parameters for the selected quality
		var complexity, vbr, signalType, bandwidth, dtx int
		switch quality {
		case AudioQualityLow:
			complexity = GetConfig().AudioQualityLowOpusComplexity
			vbr = GetConfig().AudioQualityLowOpusVBR
			signalType = GetConfig().AudioQualityLowOpusSignalType
			bandwidth = GetConfig().AudioQualityLowOpusBandwidth
			dtx = GetConfig().AudioQualityLowOpusDTX
		case AudioQualityMedium:
			complexity = GetConfig().AudioQualityMediumOpusComplexity
			vbr = GetConfig().AudioQualityMediumOpusVBR
			signalType = GetConfig().AudioQualityMediumOpusSignalType
			bandwidth = GetConfig().AudioQualityMediumOpusBandwidth
			dtx = GetConfig().AudioQualityMediumOpusDTX
		case AudioQualityHigh:
			complexity = GetConfig().AudioQualityHighOpusComplexity
			vbr = GetConfig().AudioQualityHighOpusVBR
			signalType = GetConfig().AudioQualityHighOpusSignalType
			bandwidth = GetConfig().AudioQualityHighOpusBandwidth
			dtx = GetConfig().AudioQualityHighOpusDTX
		case AudioQualityUltra:
			complexity = GetConfig().AudioQualityUltraOpusComplexity
			vbr = GetConfig().AudioQualityUltraOpusVBR
			signalType = GetConfig().AudioQualityUltraOpusSignalType
			bandwidth = GetConfig().AudioQualityUltraOpusBandwidth
			dtx = GetConfig().AudioQualityUltraOpusDTX
		default:
			// Use medium quality as fallback
			complexity = GetConfig().AudioQualityMediumOpusComplexity
			vbr = GetConfig().AudioQualityMediumOpusVBR
			signalType = GetConfig().AudioQualityMediumOpusSignalType
			bandwidth = GetConfig().AudioQualityMediumOpusBandwidth
			dtx = GetConfig().AudioQualityMediumOpusDTX
		}

		// Update audio input subprocess configuration dynamically without restart
		if supervisor := GetAudioInputSupervisor(); supervisor != nil {
			logger := logging.GetDefaultLogger().With().Str("component", "audio").Logger()
			logger.Info().Int("quality", int(quality)).Msg("updating audio input subprocess quality settings dynamically")

			// Set new OPUS configuration for future restarts
			supervisor.SetOpusConfig(config.Bitrate*1000, complexity, vbr, signalType, bandwidth, dtx)

			// Send dynamic configuration update to running subprocess
			if supervisor.IsConnected() {
				// Convert AudioConfig to InputIPCOpusConfig with complete Opus parameters
				opusConfig := InputIPCOpusConfig{
					SampleRate: config.SampleRate,
					Channels:   config.Channels,
					FrameSize:  int(config.FrameSize.Milliseconds() * int64(config.SampleRate) / 1000), // Convert ms to samples
					Bitrate:    config.Bitrate * 1000,                                                  // Convert kbps to bps
					Complexity: complexity,
					VBR:        vbr,
					SignalType: signalType,
					Bandwidth:  bandwidth,
					DTX:        dtx,
				}

				logger.Info().Interface("opusConfig", opusConfig).Msg("sending Opus configuration to audio input subprocess")
				if err := supervisor.SendOpusConfig(opusConfig); err != nil {
					logger.Warn().Err(err).Msg("failed to send dynamic Opus config update, subprocess may need restart")
					// Fallback to restart if dynamic update fails
					supervisor.Stop()
					if err := supervisor.Start(); err != nil {
						logger.Error().Err(err).Msg("failed to restart audio input subprocess after config update failure")
					}
				} else {
					logger.Info().Msg("audio input quality updated dynamically with complete Opus configuration")
				}
			} else {
				logger.Info().Bool("supervisor_running", supervisor.IsRunning()).Msg("audio input subprocess not connected, configuration will apply on next start")
			}
		}
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

// RecordFrameReceived increments the frames received counter with simplified tracking
func RecordFrameReceived(bytes int) {
	// Direct atomic updates to avoid sampling complexity in critical path
	atomic.AddInt64(&metrics.FramesReceived, 1)
	atomic.AddInt64(&metrics.BytesProcessed, int64(bytes))

	// Always update timestamp for accurate last frame tracking
	metrics.LastFrameTime = time.Now()
}

// RecordFrameDropped increments the frames dropped counter with simplified tracking
func RecordFrameDropped() {
	// Direct atomic update to avoid sampling complexity in critical path
	atomic.AddInt64(&metrics.FramesDropped, 1)
}

// RecordConnectionDrop increments the connection drops counter with simplified tracking
func RecordConnectionDrop() {
	// Direct atomic update to avoid sampling complexity in critical path
	atomic.AddInt64(&metrics.ConnectionDrops, 1)
}

// FlushPendingMetrics is now a no-op since we use direct atomic updates
func FlushPendingMetrics() {
	// No-op: metrics are now updated directly without local buffering
	// This function is kept for API compatibility
}
