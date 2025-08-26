package audio

import (
	"errors"
	"fmt"
	"time"
	"unsafe"

	"github.com/rs/zerolog"
)

// Enhanced validation errors with more specific context
var (
	ErrInvalidFrameLength    = errors.New("invalid frame length")
	ErrFrameDataCorrupted    = errors.New("frame data appears corrupted")
	ErrBufferAlignment       = errors.New("buffer alignment invalid")
	ErrInvalidSampleFormat   = errors.New("invalid sample format")
	ErrInvalidTimestamp      = errors.New("invalid timestamp")
	ErrConfigurationMismatch = errors.New("configuration mismatch")
	ErrResourceExhaustion    = errors.New("resource exhaustion detected")
	ErrInvalidPointer        = errors.New("invalid pointer")
	ErrBufferOverflow        = errors.New("buffer overflow detected")
	ErrInvalidState          = errors.New("invalid state")
)

// ValidationLevel defines the level of validation to perform
type ValidationLevel int

const (
	ValidationMinimal  ValidationLevel = iota // Only critical safety checks
	ValidationStandard                        // Standard validation for production
	ValidationStrict                          // Comprehensive validation for debugging
)

// ValidationConfig controls validation behavior
type ValidationConfig struct {
	Level                ValidationLevel
	EnableRangeChecks    bool
	EnableAlignmentCheck bool
	EnableDataIntegrity  bool
	MaxValidationTime    time.Duration
}

// GetValidationConfig returns the current validation configuration
func GetValidationConfig() ValidationConfig {
	return ValidationConfig{
		Level:                ValidationStandard,
		EnableRangeChecks:    true,
		EnableAlignmentCheck: true,
		EnableDataIntegrity:  false,           // Disabled by default for performance
		MaxValidationTime:    5 * time.Second, // Default validation timeout
	}
}

// ValidateAudioFrameFast performs minimal validation for performance-critical paths
func ValidateAudioFrameFast(data []byte) error {
	if len(data) == 0 {
		return ErrInvalidFrameData
	}

	// Quick bounds check using config constants
	maxSize := GetConfig().MaxAudioFrameSize
	if len(data) > maxSize {
		return fmt.Errorf("%w: frame size %d exceeds maximum %d", ErrInvalidFrameSize, len(data), maxSize)
	}

	return nil
}

// ValidateAudioFrameComprehensive performs thorough validation
func ValidateAudioFrameComprehensive(data []byte, expectedSampleRate int, expectedChannels int) error {
	validationConfig := GetValidationConfig()
	start := time.Now()

	// Timeout protection for validation
	defer func() {
		if time.Since(start) > validationConfig.MaxValidationTime {
			// Log validation timeout but don't fail
			getValidationLogger().Warn().Dur("duration", time.Since(start)).Msg("validation timeout exceeded")
		}
	}()

	// Basic validation first
	if err := ValidateAudioFrameFast(data); err != nil {
		return err
	}

	// Range validation
	if validationConfig.EnableRangeChecks {
		config := GetConfig()
		minFrameSize := 64 // Minimum reasonable frame size
		if len(data) < minFrameSize {
			return fmt.Errorf("%w: frame size %d below minimum %d", ErrInvalidFrameSize, len(data), minFrameSize)
		}

		// Validate frame length matches expected sample format
		expectedFrameSize := (expectedSampleRate * expectedChannels * 2) / 1000 * int(config.AudioQualityMediumFrameSize/time.Millisecond)
		tolerance := 512 // Frame size tolerance in bytes
		if abs(len(data)-expectedFrameSize) > tolerance {
			return fmt.Errorf("%w: frame size %d doesn't match expected %d (±%d)", ErrInvalidFrameLength, len(data), expectedFrameSize, tolerance)
		}
	}

	// Alignment validation for ARM32 compatibility
	if validationConfig.EnableAlignmentCheck {
		if uintptr(unsafe.Pointer(&data[0]))%4 != 0 {
			return fmt.Errorf("%w: buffer not 4-byte aligned for ARM32", ErrBufferAlignment)
		}
	}

	// Data integrity checks (expensive, only for debugging)
	if validationConfig.EnableDataIntegrity && validationConfig.Level == ValidationStrict {
		if err := validateAudioDataIntegrity(data, expectedChannels); err != nil {
			return err
		}
	}

	return nil
}

// ValidateZeroCopyFrameEnhanced performs enhanced zero-copy frame validation
func ValidateZeroCopyFrameEnhanced(frame *ZeroCopyAudioFrame) error {
	if frame == nil {
		return fmt.Errorf("%w: frame is nil", ErrInvalidPointer)
	}

	// Check reference count validity
	frame.mutex.RLock()
	refCount := frame.refCount
	length := frame.length
	capacity := frame.capacity
	frame.mutex.RUnlock()

	if refCount <= 0 {
		return fmt.Errorf("%w: invalid reference count %d", ErrInvalidState, refCount)
	}

	if length < 0 || capacity < 0 {
		return fmt.Errorf("%w: negative length (%d) or capacity (%d)", ErrInvalidState, length, capacity)
	}

	if length > capacity {
		return fmt.Errorf("%w: length %d exceeds capacity %d", ErrBufferOverflow, length, capacity)
	}

	// Validate the underlying data
	data := frame.Data()
	return ValidateAudioFrameFast(data)
}

// ValidateBufferBounds performs bounds checking with overflow protection
func ValidateBufferBounds(buffer []byte, offset, length int) error {
	if buffer == nil {
		return fmt.Errorf("%w: buffer is nil", ErrInvalidPointer)
	}

	if offset < 0 {
		return fmt.Errorf("%w: negative offset %d", ErrInvalidState, offset)
	}

	if length < 0 {
		return fmt.Errorf("%w: negative length %d", ErrInvalidState, length)
	}

	// Check for integer overflow
	if offset > len(buffer) {
		return fmt.Errorf("%w: offset %d exceeds buffer length %d", ErrBufferOverflow, offset, len(buffer))
	}

	// Safe addition check for overflow
	if offset+length < offset || offset+length > len(buffer) {
		return fmt.Errorf("%w: range [%d:%d] exceeds buffer length %d", ErrBufferOverflow, offset, offset+length, len(buffer))
	}

	return nil
}

// ValidateAudioConfiguration performs comprehensive configuration validation
func ValidateAudioConfiguration(config AudioConfig) error {
	if err := ValidateAudioQuality(config.Quality); err != nil {
		return fmt.Errorf("quality validation failed: %w", err)
	}

	configConstants := GetConfig()

	// Validate bitrate ranges
	minBitrate := 6000   // Minimum Opus bitrate
	maxBitrate := 510000 // Maximum Opus bitrate
	if config.Bitrate < minBitrate || config.Bitrate > maxBitrate {
		return fmt.Errorf("%w: bitrate %d outside valid range [%d, %d]", ErrInvalidConfiguration, config.Bitrate, minBitrate, maxBitrate)
	}

	// Validate sample rate
	validSampleRates := []int{8000, 12000, 16000, 24000, 48000}
	validSampleRate := false
	for _, rate := range validSampleRates {
		if config.SampleRate == rate {
			validSampleRate = true
			break
		}
	}
	if !validSampleRate {
		return fmt.Errorf("%w: sample rate %d not in supported rates %v", ErrInvalidSampleRate, config.SampleRate, validSampleRates)
	}

	// Validate channels
	if config.Channels < 1 || config.Channels > configConstants.MaxChannels {
		return fmt.Errorf("%w: channels %d outside valid range [1, %d]", ErrInvalidChannels, config.Channels, configConstants.MaxChannels)
	}

	// Validate frame size
	minFrameSize := 10 * time.Millisecond  // Minimum frame duration
	maxFrameSize := 100 * time.Millisecond // Maximum frame duration
	if config.FrameSize < minFrameSize || config.FrameSize > maxFrameSize {
		return fmt.Errorf("%w: frame size %v outside valid range [%v, %v]", ErrInvalidConfiguration, config.FrameSize, minFrameSize, maxFrameSize)
	}

	return nil
}

// ValidateResourceLimits checks if system resources are within acceptable limits
func ValidateResourceLimits() error {
	config := GetConfig()

	// Check buffer pool sizes
	framePoolStats := GetAudioBufferPoolStats()
	if framePoolStats.FramePoolSize > int64(config.MaxPoolSize*2) {
		return fmt.Errorf("%w: frame pool size %d exceeds safe limit %d", ErrResourceExhaustion, framePoolStats.FramePoolSize, config.MaxPoolSize*2)
	}

	// Check zero-copy pool allocation count
	zeroCopyStats := GetGlobalZeroCopyPoolStats()
	if zeroCopyStats.AllocationCount > int64(config.MaxPoolSize*3) {
		return fmt.Errorf("%w: zero-copy allocations %d exceed safe limit %d", ErrResourceExhaustion, zeroCopyStats.AllocationCount, config.MaxPoolSize*3)
	}

	return nil
}

// validateAudioDataIntegrity performs expensive data integrity checks
func validateAudioDataIntegrity(data []byte, channels int) error {
	if len(data)%2 != 0 {
		return fmt.Errorf("%w: odd number of bytes for 16-bit samples", ErrInvalidSampleFormat)
	}

	if len(data)%(channels*2) != 0 {
		return fmt.Errorf("%w: data length %d not aligned to channel count %d", ErrInvalidSampleFormat, len(data), channels)
	}

	// Check for obvious corruption patterns (all zeros, all max values)
	sampleCount := len(data) / 2
	zeroCount := 0
	maxCount := 0

	for i := 0; i < len(data); i += 2 {
		sample := int16(data[i]) | int16(data[i+1])<<8
		switch sample {
		case 0:
			zeroCount++
		case 32767, -32768:
			maxCount++
		}
	}

	// Flag suspicious patterns
	if zeroCount > sampleCount*9/10 {
		return fmt.Errorf("%w: %d%% zero samples suggests silence or corruption", ErrFrameDataCorrupted, (zeroCount*100)/sampleCount)
	}

	if maxCount > sampleCount/10 {
		return fmt.Errorf("%w: %d%% max-value samples suggests clipping or corruption", ErrFrameDataCorrupted, (maxCount*100)/sampleCount)
	}

	return nil
}

// Helper function for absolute value
func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// getValidationLogger returns a logger for validation operations
func getValidationLogger() *zerolog.Logger {
	// Return a basic logger for validation
	logger := zerolog.New(nil).With().Timestamp().Logger()
	return &logger
}
