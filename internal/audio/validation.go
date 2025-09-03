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
	ErrInvalidAudioQuality    = errors.New("invalid audio quality level")
	ErrInvalidFrameSize       = errors.New("invalid frame size")
	ErrInvalidFrameData       = errors.New("invalid frame data")
	ErrFrameDataEmpty         = errors.New("invalid frame data: frame data is empty")
	ErrFrameDataTooLarge      = errors.New("invalid frame data: exceeds maximum")
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
// Optimized to use AudioConfigCache for frequently accessed values
func ValidateBufferSize(size int) error {
	if size <= 0 {
		return fmt.Errorf("%w: buffer size %d must be positive", ErrInvalidBufferSize, size)
	}

	// Fast path: Check against cached max frame size
	cache := GetCachedConfig()
	maxFrameSize := int(cache.maxAudioFrameSize.Load())

	// Most common case: validating a buffer that's sized for audio frames
	if maxFrameSize > 0 && size <= maxFrameSize {
		return nil
	}

	// Slower path: full validation against SocketMaxBuffer
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
// Optimized to use AudioConfigCache for frequently accessed values
func ValidateSampleRate(sampleRate int) error {
	if sampleRate <= 0 {
		return fmt.Errorf("%w: sample rate %d must be positive", ErrInvalidSampleRate, sampleRate)
	}

	// Fast path: Check against cached sample rate first
	cache := GetCachedConfig()
	cachedRate := int(cache.sampleRate.Load())

	// Most common case: validating against the current sample rate
	if sampleRate == cachedRate {
		return nil
	}

	// Slower path: check against all valid rates
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
// Optimized to use AudioConfigCache for frequently accessed values
func ValidateChannelCount(channels int) error {
	if channels <= 0 {
		return fmt.Errorf("%w: channel count %d must be positive", ErrInvalidChannels, channels)
	}

	// Fast path: Check against cached channels first
	cache := GetCachedConfig()
	cachedChannels := int(cache.channels.Load())

	// Most common case: validating against the current channel count
	if channels == cachedChannels {
		return nil
	}

	// Check against max channels - still using cache to avoid GetConfig()
	// Note: We don't have maxChannels in the cache yet, so we'll use GetConfig() for now
	config := GetConfig()
	if channels > config.MaxChannels {
		return fmt.Errorf("%w: channel count %d exceeds maximum %d",
			ErrInvalidChannels, channels, config.MaxChannels)
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
	cache := GetCachedConfig()
	minBitrate := int(cache.minOpusBitrate.Load())
	maxBitrate := int(cache.maxOpusBitrate.Load())

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

	// Slower path: full validation with GetConfig()
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
// Optimized to use AudioConfigCache for frequently accessed values
func ValidateFrameDuration(duration time.Duration) error {
	if duration <= 0 {
		return fmt.Errorf("%w: frame duration %v must be positive", ErrInvalidFrameDuration, duration)
	}

	// Fast path: Check against cached frame size first
	cache := GetCachedConfig()

	// Convert frameSize (samples) to duration for comparison
	// Note: This calculation should match how frameSize is converted to duration elsewhere
	cachedFrameSize := int(cache.frameSize.Load())
	cachedSampleRate := int(cache.sampleRate.Load())

	// Only do this calculation if we have valid cached values
	if cachedFrameSize > 0 && cachedSampleRate > 0 {
		cachedDuration := time.Duration(cachedFrameSize) * time.Second / time.Duration(cachedSampleRate)

		// Most common case: validating against the current frame duration
		if duration == cachedDuration {
			return nil
		}
	}

	// Slower path: full validation against min/max
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
// Uses optimized validation functions that leverage AudioConfigCache
func ValidateAudioConfigComplete(config AudioConfig) error {
	// Fast path: Check if all values match the current cached configuration
	cache := GetCachedConfig()
	cachedSampleRate := int(cache.sampleRate.Load())
	cachedChannels := int(cache.channels.Load())
	cachedBitrate := int(cache.opusBitrate.Load()) / 1000 // Convert from bps to kbps
	cachedFrameSize := int(cache.frameSize.Load())

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
		if config.MaxFrameSize <= 0 {
			return fmt.Errorf("invalid MaxFrameSize: %d", config.MaxFrameSize)
		}
		if config.SampleRate <= 0 {
			return fmt.Errorf("invalid SampleRate: %d", config.SampleRate)
		}
	}
	return nil
}

// Note: We're transitioning from individual cached values to using AudioConfigCache
// for better consistency and reduced maintenance overhead

// Global variable for backward compatibility
var cachedMaxFrameSize int

// InitValidationCache initializes cached validation values with actual config
func InitValidationCache() {
	// Initialize the global cache variable for backward compatibility
	config := GetConfig()
	cachedMaxFrameSize = config.MaxAudioFrameSize

	// Update the global audio config cache
	GetCachedConfig().Update()
}

// ValidateAudioFrame provides optimized validation for audio frame data
// This is the primary validation function used in all audio processing paths
//
// Performance optimizations:
// - Uses AudioConfigCache to eliminate GetConfig() call overhead
// - Single branch condition for optimal CPU pipeline efficiency
// - Inlined length checks for minimal overhead
// - Pre-allocated error messages for minimal allocations
//
//go:inline
func ValidateAudioFrame(data []byte) error {
	// Fast path: empty check first to avoid unnecessary cache access
	dataLen := len(data)
	if dataLen == 0 {
		return ErrFrameDataEmpty
	}

	// Get cached config - this is a pointer access, not a function call
	cache := GetCachedConfig()

	// Use atomic access to maxAudioFrameSize for lock-free validation
	maxSize := int(cache.maxAudioFrameSize.Load())

	// If cache not initialized or value is zero, use global cached value or update
	if maxSize == 0 {
		if cachedMaxFrameSize > 0 {
			maxSize = cachedMaxFrameSize
		} else {
			cache.Update()
			maxSize = int(cache.maxAudioFrameSize.Load())
			if maxSize == 0 {
				// Fallback to global config if cache still not initialized
				maxSize = GetConfig().MaxAudioFrameSize
			}
		}
	}

	// Optimized validation with error message
	if dataLen > maxSize {
		// Use formatted error since we can't guarantee pre-allocated error is available
		return fmt.Errorf("%w: frame size %d exceeds maximum %d bytes",
			ErrFrameDataTooLarge, dataLen, maxSize)
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
