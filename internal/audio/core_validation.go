//go:build cgo || arm
// +build cgo arm

package audio

import (
	"errors"
	"fmt"
	"time"
)

// Validation errors
var (
	ErrInvalidAudioQuality = errors.New("invalid audio quality level")
	ErrInvalidFrameSize    = errors.New("invalid frame size")
	ErrInvalidFrameData    = errors.New("invalid frame data")
	ErrFrameDataEmpty      = errors.New("invalid frame data: frame data is empty")
	ErrFrameDataTooLarge   = errors.New("invalid frame data: exceeds maximum")
	ErrInvalidBufferSize   = errors.New("invalid buffer size")

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
// Optimized to use cached max frame size
func ValidateZeroCopyFrame(frame *ZeroCopyAudioFrame) error {
	if frame == nil {
		return ErrInvalidFrameData
	}
	data := frame.Data()
	if len(data) == 0 {
		return ErrInvalidFrameData
	}

	// Fast path: use cached max frame size
	maxFrameSize := cachedMaxFrameSize
	if maxFrameSize == 0 {
		// Fallback: get from cache
		cache := Config
		maxFrameSize = cache.MaxAudioFrameSize
		if maxFrameSize == 0 {
			// Last resort: use default
			maxFrameSize = cache.MaxAudioFrameSize
		}
		// Cache globally for next calls
		cachedMaxFrameSize = maxFrameSize
	}

	if len(data) > maxFrameSize {
		return ErrInvalidFrameSize
	}
	return nil
}

// ValidateBufferSize validates buffer size parameters with enhanced boundary checks
// Optimized to use AudioConfigCache for frequently accessed values
func ValidateBufferSize(size int) error {
	if size <= 0 {
		return fmt.Errorf("%w: buffer size %d must be positive", ErrInvalidBufferSize, size)
	}

	// Fast path: Check against cached max frame size
	cache := Config
	maxFrameSize := cache.MaxAudioFrameSize

	// Most common case: validating a buffer that's sized for audio frames
	if maxFrameSize > 0 && size <= maxFrameSize {
		return nil
	}

	// Use SocketMaxBuffer as the upper limit for general buffer validation
	// This allows for socket buffers while still preventing extremely large allocations
	if size > Config.SocketMaxBuffer {
		return fmt.Errorf("%w: buffer size %d exceeds maximum %d",
			ErrInvalidBufferSize, size, Config.SocketMaxBuffer)
	}
	return nil
}

// ValidateLatency validates latency duration values with reasonable bounds
// Optimized to use AudioConfigCache for frequently accessed values
func ValidateLatency(latency time.Duration) error {
	if latency < 0 {
		return fmt.Errorf("%w: latency %v cannot be negative", ErrInvalidLatency, latency)
	}

	// Fast path: check against cached max latency
	cache := Config
	maxLatency := time.Duration(cache.MaxLatency)

	// If we have a valid cached value, use it
	if maxLatency > 0 {
		minLatency := time.Millisecond // Minimum reasonable latency
		if latency > 0 && latency < minLatency {
			return fmt.Errorf("%w: latency %v below minimum %v",
				ErrInvalidLatency, latency, minLatency)
		}
		if latency > maxLatency {
			return fmt.Errorf("%w: latency %v exceeds maximum %v",
				ErrInvalidLatency, latency, maxLatency)
		}
		return nil
	}

	minLatency := time.Millisecond // Minimum reasonable latency
	if latency > 0 && latency < minLatency {
		return fmt.Errorf("%w: latency %v below minimum %v",
			ErrInvalidLatency, latency, minLatency)
	}
	if latency > Config.MaxLatency {
		return fmt.Errorf("%w: latency %v exceeds maximum %v",
			ErrInvalidLatency, latency, Config.MaxLatency)
	}
	return nil
}

// ValidateMetricsInterval validates metrics update interval
// Optimized to use AudioConfigCache for frequently accessed values
func ValidateMetricsInterval(interval time.Duration) error {
	// Fast path: check against cached values
	cache := Config
	minInterval := time.Duration(cache.MinMetricsUpdateInterval)
	maxInterval := time.Duration(cache.MaxMetricsUpdateInterval)

	// If we have valid cached values, use them
	if minInterval > 0 && maxInterval > 0 {
		if interval < minInterval {
			return fmt.Errorf("%w: interval %v below minimum %v",
				ErrInvalidMetricsInterval, interval, minInterval)
		}
		if interval > maxInterval {
			return fmt.Errorf("%w: interval %v exceeds maximum %v",
				ErrInvalidMetricsInterval, interval, maxInterval)
		}
		return nil
	}

	minInterval = Config.MinMetricsUpdateInterval
	maxInterval = Config.MaxMetricsUpdateInterval
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
	maxBuffer := Config.SocketMaxBuffer
	if maxSize > maxBuffer {
		return ErrInvalidBufferSize
	}
	return nil
}

// ValidateInputIPCConfig validates input IPC configuration
func ValidateInputIPCConfig(sampleRate, channels, frameSize int) error {
	minSampleRate := Config.MinSampleRate
	maxSampleRate := Config.MaxSampleRate
	maxChannels := Config.MaxChannels
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
	minSampleRate := Config.MinSampleRate
	maxSampleRate := Config.MaxSampleRate
	maxChannels := Config.MaxChannels
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

// ValidateSampleRate validates audio sample rate values
// Optimized to use AudioConfigCache for frequently accessed values
func ValidateSampleRate(sampleRate int) error {
	if sampleRate <= 0 {
		return fmt.Errorf("%w: sample rate %d must be positive", ErrInvalidSampleRate, sampleRate)
	}

	// Fast path: Check against cached sample rate first
	cache := Config
	cachedRate := cache.SampleRate

	// Most common case: validating against the current sample rate
	if sampleRate == cachedRate {
		return nil
	}

	// Slower path: check against all valid rates
	validRates := Config.ValidSampleRates
	for _, rate := range validRates {
		if sampleRate == rate {
			return nil
		}
	}
	return fmt.Errorf("%w: sample rate %d not in supported rates %v",
		ErrInvalidSampleRate, sampleRate, validRates)
}

// ValidateChannelCount validates audio channel count
// Optimized to use AudioConfigCache for frequently accessed values
func ValidateChannelCount(channels int) error {
	if channels <= 0 {
		return fmt.Errorf("%w: channel count %d must be positive", ErrInvalidChannels, channels)
	}

	// Fast path: Check against cached channels first
	cache := Config
	cachedChannels := cache.Channels

	// Most common case: validating against the current channel count
	if channels == cachedChannels {
		return nil
	}

	// Fast path: Check against cached max channels
	cachedMaxChannels := cache.MaxChannels
	if cachedMaxChannels > 0 && channels <= cachedMaxChannels {
		return nil
	}

	// Slow path: Use current config values
	updatedMaxChannels := cache.MaxChannels
	if channels > updatedMaxChannels {
		return fmt.Errorf("%w: channel count %d exceeds maximum %d",
			ErrInvalidChannels, channels, updatedMaxChannels)
	}
	return nil
}

// ValidateBitrate validates audio bitrate values (expects kbps)
// Optimized to use AudioConfigCache for frequently accessed values
func ValidateBitrate(bitrate int) error {
	if bitrate <= 0 {
		return fmt.Errorf("%w: bitrate %d must be positive", ErrInvalidBitrate, bitrate)
	}

	// Fast path: Check against cached bitrate values
	cache := Config
	minBitrate := cache.MinOpusBitrate
	maxBitrate := cache.MaxOpusBitrate

	// If we have valid cached values, use them
	if minBitrate > 0 && maxBitrate > 0 {
		// Convert kbps to bps for comparison with config limits
		bitrateInBps := bitrate * 1000
		if bitrateInBps < minBitrate {
			return fmt.Errorf("%w: bitrate %d kbps (%d bps) below minimum %d bps",
				ErrInvalidBitrate, bitrate, bitrateInBps, minBitrate)
		}
		if bitrateInBps > maxBitrate {
			return fmt.Errorf("%w: bitrate %d kbps (%d bps) exceeds maximum %d bps",
				ErrInvalidBitrate, bitrate, bitrateInBps, maxBitrate)
		}
		return nil
	}

	// Convert kbps to bps for comparison with config limits
	bitrateInBps := bitrate * 1000
	if bitrateInBps < Config.MinOpusBitrate {
		return fmt.Errorf("%w: bitrate %d kbps (%d bps) below minimum %d bps",
			ErrInvalidBitrate, bitrate, bitrateInBps, Config.MinOpusBitrate)
	}
	if bitrateInBps > Config.MaxOpusBitrate {
		return fmt.Errorf("%w: bitrate %d kbps (%d bps) exceeds maximum %d bps",
			ErrInvalidBitrate, bitrate, bitrateInBps, Config.MaxOpusBitrate)
	}
	return nil
}

// ValidateFrameDuration validates frame duration values
// Optimized to use AudioConfigCache for frequently accessed values
func ValidateFrameDuration(duration time.Duration) error {
	if duration <= 0 {
		return fmt.Errorf("%w: frame duration %v must be positive", ErrInvalidFrameDuration, duration)
	}

	// Fast path: Check against cached frame size first
	cache := Config

	// Convert frameSize (samples) to duration for comparison
	cachedFrameSize := cache.FrameSize
	cachedSampleRate := cache.SampleRate

	// Only do this calculation if we have valid cached values
	if cachedFrameSize > 0 && cachedSampleRate > 0 {
		cachedDuration := time.Duration(cachedFrameSize) * time.Second / time.Duration(cachedSampleRate)

		// Most common case: validating against the current frame duration
		if duration == cachedDuration {
			return nil
		}
	}

	// Fast path: Check against cached min/max frame duration
	cachedMinDuration := time.Duration(cache.MinFrameDuration)
	cachedMaxDuration := time.Duration(cache.MaxFrameDuration)

	if cachedMinDuration > 0 && cachedMaxDuration > 0 {
		if duration < cachedMinDuration {
			return fmt.Errorf("%w: frame duration %v below minimum %v",
				ErrInvalidFrameDuration, duration, cachedMinDuration)
		}
		if duration > cachedMaxDuration {
			return fmt.Errorf("%w: frame duration %v exceeds maximum %v",
				ErrInvalidFrameDuration, duration, cachedMaxDuration)
		}
		return nil
	}

	// Slow path: Use current config values
	updatedMinDuration := time.Duration(cache.MinFrameDuration)
	updatedMaxDuration := time.Duration(cache.MaxFrameDuration)

	if duration < updatedMinDuration {
		return fmt.Errorf("%w: frame duration %v below minimum %v",
			ErrInvalidFrameDuration, duration, updatedMinDuration)
	}
	if duration > updatedMaxDuration {
		return fmt.Errorf("%w: frame duration %v exceeds maximum %v",
			ErrInvalidFrameDuration, duration, updatedMaxDuration)
	}
	return nil
}

// ValidateAudioConfigComplete performs comprehensive audio configuration validation
// Uses optimized validation functions that leverage AudioConfigCache
func ValidateAudioConfigComplete(config AudioConfig) error {
	// Fast path: Check if all values match the current cached configuration
	cache := Config
	cachedSampleRate := cache.SampleRate
	cachedChannels := cache.Channels
	cachedBitrate := cache.OpusBitrate / 1000 // Convert from bps to kbps
	cachedFrameSize := cache.FrameSize

	// Only do this calculation if we have valid cached values
	if cachedSampleRate > 0 && cachedChannels > 0 && cachedBitrate > 0 && cachedFrameSize > 0 {
		cachedDuration := time.Duration(cachedFrameSize) * time.Second / time.Duration(cachedSampleRate)

		// Most common case: validating the current configuration
		if config.SampleRate == cachedSampleRate &&
			config.Channels == cachedChannels &&
			config.Bitrate == cachedBitrate &&
			config.FrameSize == cachedDuration {
			return nil
		}
	}

	// Slower path: validate each parameter individually
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
		if Config.MaxFrameSize <= 0 {
			return fmt.Errorf("invalid MaxFrameSize: %d", Config.MaxFrameSize)
		}
		if Config.SampleRate <= 0 {
			return fmt.Errorf("invalid SampleRate: %d", Config.SampleRate)
		}
	}
	return nil
}

// Global variable for backward compatibility
var cachedMaxFrameSize int

// InitValidationCache initializes cached validation values with actual config
func InitValidationCache() {
	// Initialize the global cache variable for backward compatibility
	cachedMaxFrameSize = Config.MaxAudioFrameSize

	// Initialize the global audio config cache
	cachedMaxFrameSize = Config.MaxAudioFrameSize
}

// ValidateAudioFrame validates audio frame data with cached max size for performance
//
//go:inline
func ValidateAudioFrame(data []byte) error {
	// Fast path: check length against cached max size in single operation
	dataLen := len(data)
	if dataLen == 0 {
		return ErrFrameDataEmpty
	}

	// Use global cached value for fastest access - updated during initialization
	maxSize := cachedMaxFrameSize
	if maxSize == 0 {
		// Fallback: get from cache only if global cache not initialized
		cache := Config
		maxSize = cache.MaxAudioFrameSize
		if maxSize == 0 {
			// Last resort: get fresh value
			maxSize = cache.MaxAudioFrameSize
		}
		// Cache the value globally for next calls
		cachedMaxFrameSize = maxSize
	}

	// Single comparison for validation
	if dataLen > maxSize {
		return ErrFrameDataTooLarge
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
