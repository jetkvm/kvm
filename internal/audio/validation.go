package audio

import (
	"errors"
	"fmt"
	"time"
)

// Validation errors
var (
	ErrInvalidAudioQuality    = errors.New("invalid audio quality level")
	ErrInvalidFrameSize       = errors.New("invalid frame size")
	ErrInvalidFrameData       = errors.New("invalid frame data")
	ErrInvalidBufferSize      = errors.New("invalid buffer size")
	ErrInvalidPriority        = errors.New("invalid priority value")
	ErrInvalidLatency         = errors.New("invalid latency value")
	ErrInvalidConfiguration   = errors.New("invalid configuration")
	ErrInvalidSocketConfig    = errors.New("invalid socket configuration")
	ErrInvalidMetricsInterval = errors.New("invalid metrics interval")
	ErrInvalidSampleRate      = errors.New("invalid sample rate")
	ErrInvalidChannels        = errors.New("invalid channels")
	ErrInvalidBitrate         = errors.New("invalid bitrate")
	ErrInvalidFrameDuration   = errors.New("invalid frame duration")
	ErrInvalidOffset          = errors.New("invalid offset")
	ErrInvalidLength          = errors.New("invalid length")
)

// ValidateAudioQuality validates audio quality enum values with enhanced checks
func ValidateAudioQuality(quality AudioQuality) error {
	// Validate enum range
	if quality < AudioQualityLow || quality > AudioQualityUltra {
		return fmt.Errorf("%w: quality value %d outside valid range [%d, %d]",
			ErrInvalidAudioQuality, int(quality), int(AudioQualityLow), int(AudioQualityUltra))
	}
	return nil
}

// ValidateZeroCopyFrame validates zero-copy audio frame
func ValidateZeroCopyFrame(frame *ZeroCopyAudioFrame) error {
	if frame == nil {
		return ErrInvalidFrameData
	}
	data := frame.Data()
	if len(data) == 0 {
		return ErrInvalidFrameData
	}
	// Use config value
	maxFrameSize := GetConfig().MaxAudioFrameSize
	if len(data) > maxFrameSize {
		return ErrInvalidFrameSize
	}
	return nil
}

// ValidateBufferSize validates buffer size parameters with enhanced boundary checks
func ValidateBufferSize(size int) error {
	if size <= 0 {
		return fmt.Errorf("%w: buffer size %d must be positive", ErrInvalidBufferSize, size)
	}
	config := GetConfig()
	// Use SocketMaxBuffer as the upper limit for general buffer validation
	// This allows for socket buffers while still preventing extremely large allocations
	if size > config.SocketMaxBuffer {
		return fmt.Errorf("%w: buffer size %d exceeds maximum %d",
			ErrInvalidBufferSize, size, config.SocketMaxBuffer)
	}
	return nil
}

// ValidateThreadPriority validates thread priority values with system limits
func ValidateThreadPriority(priority int) error {
	const minPriority, maxPriority = -20, 19
	if priority < minPriority || priority > maxPriority {
		return fmt.Errorf("%w: priority %d outside valid range [%d, %d]",
			ErrInvalidPriority, priority, minPriority, maxPriority)
	}
	return nil
}

// ValidateLatency validates latency duration values with reasonable bounds
func ValidateLatency(latency time.Duration) error {
	if latency < 0 {
		return fmt.Errorf("%w: latency %v cannot be negative", ErrInvalidLatency, latency)
	}
	config := GetConfig()
	minLatency := time.Millisecond // Minimum reasonable latency
	if latency > 0 && latency < minLatency {
		return fmt.Errorf("%w: latency %v below minimum %v",
			ErrInvalidLatency, latency, minLatency)
	}
	if latency > config.MaxLatency {
		return fmt.Errorf("%w: latency %v exceeds maximum %v",
			ErrInvalidLatency, latency, config.MaxLatency)
	}
	return nil
}

// ValidateMetricsInterval validates metrics update interval
func ValidateMetricsInterval(interval time.Duration) error {
	// Use config values
	config := GetConfig()
	minInterval := config.MinMetricsUpdateInterval
	maxInterval := config.MaxMetricsUpdateInterval
	if interval < minInterval {
		return ErrInvalidMetricsInterval
	}
	if interval > maxInterval {
		return ErrInvalidMetricsInterval
	}
	return nil
}

// ValidateAdaptiveBufferConfig validates adaptive buffer configuration
func ValidateAdaptiveBufferConfig(minSize, maxSize, defaultSize int) error {
	if minSize <= 0 || maxSize <= 0 || defaultSize <= 0 {
		return ErrInvalidBufferSize
	}
	if minSize >= maxSize {
		return ErrInvalidBufferSize
	}
	if defaultSize < minSize || defaultSize > maxSize {
		return ErrInvalidBufferSize
	}
	// Validate against global limits
	maxBuffer := GetConfig().SocketMaxBuffer
	if maxSize > maxBuffer {
		return ErrInvalidBufferSize
	}
	return nil
}

// ValidateInputIPCConfig validates input IPC configuration
func ValidateInputIPCConfig(sampleRate, channels, frameSize int) error {
	// Use config values
	config := GetConfig()
	minSampleRate := config.MinSampleRate
	maxSampleRate := config.MaxSampleRate
	maxChannels := config.MaxChannels
	if sampleRate < minSampleRate || sampleRate > maxSampleRate {
		return ErrInvalidSampleRate
	}
	if channels < 1 || channels > maxChannels {
		return ErrInvalidChannels
	}
	if frameSize <= 0 {
		return ErrInvalidFrameSize
	}
	return nil
}

// ValidateOutputIPCConfig validates output IPC configuration
func ValidateOutputIPCConfig(sampleRate, channels, frameSize int) error {
	// Use config values
	config := GetConfig()
	minSampleRate := config.MinSampleRate
	maxSampleRate := config.MaxSampleRate
	maxChannels := config.MaxChannels
	if sampleRate < minSampleRate || sampleRate > maxSampleRate {
		return ErrInvalidSampleRate
	}
	if channels < 1 || channels > maxChannels {
		return ErrInvalidChannels
	}
	if frameSize <= 0 {
		return ErrInvalidFrameSize
	}
	return nil
}

// ValidateLatencyConfig validates latency monitor configuration
func ValidateLatencyConfig(config LatencyConfig) error {
	if err := ValidateLatency(config.TargetLatency); err != nil {
		return err
	}
	if err := ValidateLatency(config.MaxLatency); err != nil {
		return err
	}
	if config.TargetLatency >= config.MaxLatency {
		return ErrInvalidLatency
	}
	if err := ValidateMetricsInterval(config.OptimizationInterval); err != nil {
		return err
	}
	if config.HistorySize <= 0 {
		return ErrInvalidBufferSize
	}
	if config.JitterThreshold < 0 {
		return ErrInvalidLatency
	}
	if config.AdaptiveThreshold < 0 || config.AdaptiveThreshold > 1.0 {
		return ErrInvalidConfiguration
	}
	return nil
}

// ValidateSampleRate validates audio sample rate values
func ValidateSampleRate(sampleRate int) error {
	if sampleRate <= 0 {
		return fmt.Errorf("%w: sample rate %d must be positive", ErrInvalidSampleRate, sampleRate)
	}
	config := GetConfig()
	validRates := config.ValidSampleRates
	for _, rate := range validRates {
		if sampleRate == rate {
			return nil
		}
	}
	return fmt.Errorf("%w: sample rate %d not in supported rates %v",
		ErrInvalidSampleRate, sampleRate, validRates)
}

// ValidateChannelCount validates audio channel count
func ValidateChannelCount(channels int) error {
	if channels <= 0 {
		return fmt.Errorf("%w: channel count %d must be positive", ErrInvalidChannels, channels)
	}
	config := GetConfig()
	if channels > config.MaxChannels {
		return fmt.Errorf("%w: channel count %d exceeds maximum %d",
			ErrInvalidChannels, channels, config.MaxChannels)
	}
	return nil
}

// ValidateBitrate validates audio bitrate values (expects kbps)
func ValidateBitrate(bitrate int) error {
	if bitrate <= 0 {
		return fmt.Errorf("%w: bitrate %d must be positive", ErrInvalidBitrate, bitrate)
	}
	config := GetConfig()
	// Convert kbps to bps for comparison with config limits
	bitrateInBps := bitrate * 1000
	if bitrateInBps < config.MinOpusBitrate {
		return fmt.Errorf("%w: bitrate %d kbps (%d bps) below minimum %d bps",
			ErrInvalidBitrate, bitrate, bitrateInBps, config.MinOpusBitrate)
	}
	if bitrateInBps > config.MaxOpusBitrate {
		return fmt.Errorf("%w: bitrate %d kbps (%d bps) exceeds maximum %d bps",
			ErrInvalidBitrate, bitrate, bitrateInBps, config.MaxOpusBitrate)
	}
	return nil
}

// ValidateFrameDuration validates frame duration values
func ValidateFrameDuration(duration time.Duration) error {
	if duration <= 0 {
		return fmt.Errorf("%w: frame duration %v must be positive", ErrInvalidFrameDuration, duration)
	}
	config := GetConfig()
	if duration < config.MinFrameDuration {
		return fmt.Errorf("%w: frame duration %v below minimum %v",
			ErrInvalidFrameDuration, duration, config.MinFrameDuration)
	}
	if duration > config.MaxFrameDuration {
		return fmt.Errorf("%w: frame duration %v exceeds maximum %v",
			ErrInvalidFrameDuration, duration, config.MaxFrameDuration)
	}
	return nil
}

// ValidateAudioConfigComplete performs comprehensive audio configuration validation
func ValidateAudioConfigComplete(config AudioConfig) error {
	if err := ValidateAudioQuality(config.Quality); err != nil {
		return fmt.Errorf("quality validation failed: %w", err)
	}
	if err := ValidateBitrate(config.Bitrate); err != nil {
		return fmt.Errorf("bitrate validation failed: %w", err)
	}
	if err := ValidateSampleRate(config.SampleRate); err != nil {
		return fmt.Errorf("sample rate validation failed: %w", err)
	}
	if err := ValidateChannelCount(config.Channels); err != nil {
		return fmt.Errorf("channel count validation failed: %w", err)
	}
	if err := ValidateFrameDuration(config.FrameSize); err != nil {
		return fmt.Errorf("frame duration validation failed: %w", err)
	}
	return nil
}

// ValidateAudioConfigConstants validates audio configuration constants
func ValidateAudioConfigConstants(config *AudioConfigConstants) error {
	// Validate that audio quality constants are within valid ranges
	for _, quality := range []AudioQuality{AudioQualityLow, AudioQualityMedium, AudioQualityHigh, AudioQualityUltra} {
		if err := ValidateAudioQuality(quality); err != nil {
			return fmt.Errorf("invalid audio quality constant %v: %w", quality, err)
		}
	}
	// Validate configuration values if config is provided
	if config != nil {
		if config.MaxFrameSize <= 0 {
			return fmt.Errorf("invalid MaxFrameSize: %d", config.MaxFrameSize)
		}
		if config.SampleRate <= 0 {
			return fmt.Errorf("invalid SampleRate: %d", config.SampleRate)
		}
	}
	return nil
}

// ValidateAudioFrameFast performs fast validation of audio frame data
// ValidateAudioFrameFast provides minimal validation for critical audio processing paths
// This function is optimized for performance and only checks essential safety bounds
func ValidateAudioFrameFast(data []byte) error {
	if len(data) == 0 {
		return fmt.Errorf("%w: frame data is empty", ErrInvalidFrameData)
	}
	maxFrameSize := GetConfig().MaxAudioFrameSize
	if len(data) > maxFrameSize {
		return fmt.Errorf("%w: frame size %d exceeds maximum %d", ErrInvalidFrameSize, len(data), maxFrameSize)
	}
	return nil
}

// ValidateAudioFrameUltraFast provides zero-overhead validation for ultra-critical paths
// This function only checks for nil/empty data and maximum size to prevent buffer overruns
// Use this in hot audio processing loops where every microsecond matters
func ValidateAudioFrameUltraFast(data []byte) error {
	// Only check for catastrophic failures that could crash the system
	if len(data) == 0 || len(data) > 8192 { // Hard-coded 8KB safety limit
		return ErrInvalidFrameData
	}
	return nil
}

// WrapWithMetadata wraps error with metadata for enhanced validation context
func WrapWithMetadata(err error, component, operation string, metadata map[string]interface{}) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s.%s: %w (metadata: %+v)", component, operation, err, metadata)
}
