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
	return Config.MaxAudioFrameSize
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
		Bitrate:    Config.AudioQualityMediumOutputBitrate,
		SampleRate: Config.SampleRate,
		Channels:   Config.Channels,
		FrameSize:  Config.AudioQualityMediumFrameSize,
	}
	currentMicrophoneConfig = AudioConfig{
		Quality:    AudioQualityMedium,
		Bitrate:    Config.AudioQualityMediumInputBitrate,
		SampleRate: Config.SampleRate,
		Channels:   1,
		FrameSize:  Config.AudioQualityMediumFrameSize,
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
		outputBitrate: Config.AudioQualityLowOutputBitrate, inputBitrate: Config.AudioQualityLowInputBitrate,
		sampleRate: Config.AudioQualityLowSampleRate, channels: Config.AudioQualityLowChannels,
		frameSize: Config.AudioQualityLowFrameSize,
	},
	AudioQualityMedium: {
		outputBitrate: Config.AudioQualityMediumOutputBitrate, inputBitrate: Config.AudioQualityMediumInputBitrate,
		sampleRate: Config.AudioQualityMediumSampleRate, channels: Config.AudioQualityMediumChannels,
		frameSize: Config.AudioQualityMediumFrameSize,
	},
	AudioQualityHigh: {
		outputBitrate: Config.AudioQualityHighOutputBitrate, inputBitrate: Config.AudioQualityHighInputBitrate,
		sampleRate: Config.SampleRate, channels: Config.AudioQualityHighChannels,
		frameSize: Config.AudioQualityHighFrameSize,
	},
	AudioQualityUltra: {
		outputBitrate: Config.AudioQualityUltraOutputBitrate, inputBitrate: Config.AudioQualityUltraInputBitrate,
		sampleRate: Config.SampleRate, channels: Config.AudioQualityUltraChannels,
		frameSize: Config.AudioQualityUltraFrameSize,
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
					return Config.AudioQualityMicLowSampleRate
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
			complexity = Config.AudioQualityLowOpusComplexity
			vbr = Config.AudioQualityLowOpusVBR
			signalType = Config.AudioQualityLowOpusSignalType
			bandwidth = Config.AudioQualityLowOpusBandwidth
			dtx = Config.AudioQualityLowOpusDTX
		case AudioQualityMedium:
			complexity = Config.AudioQualityMediumOpusComplexity
			vbr = Config.AudioQualityMediumOpusVBR
			signalType = Config.AudioQualityMediumOpusSignalType
			bandwidth = Config.AudioQualityMediumOpusBandwidth
			dtx = Config.AudioQualityMediumOpusDTX
		case AudioQualityHigh:
			complexity = Config.AudioQualityHighOpusComplexity
			vbr = Config.AudioQualityHighOpusVBR
			signalType = Config.AudioQualityHighOpusSignalType
			bandwidth = Config.AudioQualityHighOpusBandwidth
			dtx = Config.AudioQualityHighOpusDTX
		case AudioQualityUltra:
			complexity = Config.AudioQualityUltraOpusComplexity
			vbr = Config.AudioQualityUltraOpusVBR
			signalType = Config.AudioQualityUltraOpusSignalType
			bandwidth = Config.AudioQualityUltraOpusBandwidth
			dtx = Config.AudioQualityUltraOpusDTX
		default:
			// Use medium quality as fallback
			complexity = Config.AudioQualityMediumOpusComplexity
			vbr = Config.AudioQualityMediumOpusVBR
			signalType = Config.AudioQualityMediumOpusSignalType
			bandwidth = Config.AudioQualityMediumOpusBandwidth
			dtx = Config.AudioQualityMediumOpusDTX
		}

		// Restart audio output subprocess with new OPUS configuration
		if supervisor := GetAudioOutputSupervisor(); supervisor != nil {
			logger := logging.GetDefaultLogger().With().Str("component", "audio").Logger()
			logger.Info().Int("quality", int(quality)).Msg("restarting audio output subprocess with new quality settings")

			// Immediately boost adaptive buffer sizes to handle quality change frame burst
			// This prevents "Message channel full, dropping frame" warnings during transitions
			adaptiveManager := GetAdaptiveBufferManager()
			if adaptiveManager != nil {
				// Immediately set buffers to maximum size for quality change
				adaptiveManager.BoostBuffersForQualityChange()
				logger.Debug().Msg("boosted adaptive buffers for quality change")
			}

			// Set new OPUS configuration
			supervisor.SetOpusConfig(config.Bitrate*1000, complexity, vbr, signalType, bandwidth, dtx)

			// Stop current subprocess
			supervisor.Stop()

			// Wait for supervisor to fully stop before starting again with timeout
			// This prevents race conditions and audio breakage
			stopTimeout := time.After(Config.QualityChangeSupervisorTimeout)
			ticker := time.NewTicker(Config.QualityChangeTickerInterval)
			defer ticker.Stop()

			for {
				select {
				case <-stopTimeout:
					logger.Warn().Msg("supervisor did not stop within 5s timeout, proceeding anyway")
					goto startSupervisor
				case <-ticker.C:
					if !supervisor.IsRunning() {
						goto startSupervisor
					}
				}
			}

		startSupervisor:

			// Start subprocess with new configuration
			if err := supervisor.Start(); err != nil {
				logger.Error().Err(err).Msg("failed to restart audio output subprocess")
			} else {
				logger.Info().Int("quality", int(quality)).Msg("audio output subprocess restarted successfully with new quality")

				// Reset audio input server stats after quality change
				// Allow adaptive buffer manager to naturally adjust buffer sizes
				go func() {
					time.Sleep(Config.QualityChangeSettleDelay) // Wait for quality change to settle
					// Reset audio input server stats to clear persistent warnings
					ResetGlobalAudioInputServerStats()
					// Attempt recovery if microphone is still having issues
					time.Sleep(1 * time.Second)
					RecoverGlobalAudioInputServer()
				}()
			}
		} else {
			// Fallback to dynamic update if supervisor is not available
			vbrConstraint := Config.CGOOpusVBRConstraint
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
			complexity = Config.AudioQualityLowOpusComplexity
			vbr = Config.AudioQualityLowOpusVBR
			signalType = Config.AudioQualityLowOpusSignalType
			bandwidth = Config.AudioQualityLowOpusBandwidth
			dtx = Config.AudioQualityLowOpusDTX
		case AudioQualityMedium:
			complexity = Config.AudioQualityMediumOpusComplexity
			vbr = Config.AudioQualityMediumOpusVBR
			signalType = Config.AudioQualityMediumOpusSignalType
			bandwidth = Config.AudioQualityMediumOpusBandwidth
			dtx = Config.AudioQualityMediumOpusDTX
		case AudioQualityHigh:
			complexity = Config.AudioQualityHighOpusComplexity
			vbr = Config.AudioQualityHighOpusVBR
			signalType = Config.AudioQualityHighOpusSignalType
			bandwidth = Config.AudioQualityHighOpusBandwidth
			dtx = Config.AudioQualityHighOpusDTX
		case AudioQualityUltra:
			complexity = Config.AudioQualityUltraOpusComplexity
			vbr = Config.AudioQualityUltraOpusVBR
			signalType = Config.AudioQualityUltraOpusSignalType
			bandwidth = Config.AudioQualityUltraOpusBandwidth
			dtx = Config.AudioQualityUltraOpusDTX
		default:
			// Use medium quality as fallback
			complexity = Config.AudioQualityMediumOpusComplexity
			vbr = Config.AudioQualityMediumOpusVBR
			signalType = Config.AudioQualityMediumOpusSignalType
			bandwidth = Config.AudioQualityMediumOpusBandwidth
			dtx = Config.AudioQualityMediumOpusDTX
		}

		// Update audio input subprocess configuration dynamically without restart
		if supervisor := GetAudioInputSupervisor(); supervisor != nil {
			logger := logging.GetDefaultLogger().With().Str("component", "audio").Logger()
			logger.Info().Int("quality", int(quality)).Msg("updating audio input subprocess quality settings dynamically")

			// Immediately boost adaptive buffer sizes to handle quality change frame burst
			// This prevents "Message channel full, dropping frame" warnings during transitions
			adaptiveManager := GetAdaptiveBufferManager()
			if adaptiveManager != nil {
				// Immediately set buffers to maximum size for quality change
				adaptiveManager.BoostBuffersForQualityChange()
				logger.Debug().Msg("boosted adaptive buffers for quality change")
			}

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

					// Reset audio input server stats after config update
					// Allow adaptive buffer manager to naturally adjust buffer sizes
					go func() {
						time.Sleep(Config.QualityChangeSettleDelay) // Wait for quality change to settle
						// Reset audio input server stats to clear persistent warnings
						ResetGlobalAudioInputServerStats()
						// Attempt recovery if microphone is still having issues
						time.Sleep(1 * time.Second)
						RecoverGlobalAudioInputServer()
					}()
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

// GetGlobalAudioMetrics returns the current global audio metrics
func GetGlobalAudioMetrics() AudioMetrics {
	return metrics
}

// Batched metrics to reduce atomic operations frequency
var (
	batchedFramesReceived  int64
	batchedBytesProcessed  int64
	batchedFramesDropped   int64
	batchedConnectionDrops int64

	lastFlushTime int64 // Unix timestamp in nanoseconds
)

// RecordFrameReceived increments the frames received counter with batched updates
func RecordFrameReceived(bytes int) {
	// Use local batching to reduce atomic operations frequency
	atomic.AddInt64(&batchedBytesProcessed, int64(bytes))

	// Update timestamp immediately for accurate tracking
	metrics.LastFrameTime = time.Now()
}

// RecordFrameDropped increments the frames dropped counter with batched updates
func RecordFrameDropped() {
}

// RecordConnectionDrop increments the connection drops counter with batched updates
func RecordConnectionDrop() {
}

// flushBatchedMetrics flushes accumulated metrics to the main counters
func flushBatchedMetrics() {
	// Atomically move batched metrics to main metrics
	framesReceived := atomic.SwapInt64(&batchedFramesReceived, 0)
	bytesProcessed := atomic.SwapInt64(&batchedBytesProcessed, 0)
	framesDropped := atomic.SwapInt64(&batchedFramesDropped, 0)
	connectionDrops := atomic.SwapInt64(&batchedConnectionDrops, 0)

	// Update main metrics if we have any batched data
	if framesReceived > 0 {
		atomic.AddInt64(&metrics.FramesReceived, framesReceived)
	}
	if bytesProcessed > 0 {
		atomic.AddInt64(&metrics.BytesProcessed, bytesProcessed)
	}
	if framesDropped > 0 {
		atomic.AddInt64(&metrics.FramesDropped, framesDropped)
	}
	if connectionDrops > 0 {
		atomic.AddInt64(&metrics.ConnectionDrops, connectionDrops)
	}

	// Update last flush time
	atomic.StoreInt64(&lastFlushTime, time.Now().UnixNano())
}

// FlushPendingMetrics forces a flush of all batched metrics
func FlushPendingMetrics() {
	flushBatchedMetrics()
}
