package audio

import (
	"errors"
	"fmt"
	"time"
	"unsafe"

	"github.com/rs/zerolog"
)

// Validation errors
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
	ErrOperationTimeout      = errors.New("operation timeout")
	ErrSystemOverload        = errors.New("system overload detected")
	ErrHardwareFailure       = errors.New("hardware failure")
	ErrNetworkError          = errors.New("network error")
	ErrMemoryExhaustion      = errors.New("memory exhaustion")
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
	// Use direct config access
	config := GetConfig()
	return ValidationConfig{
		Level:                ValidationStandard,
		EnableRangeChecks:    true,
		EnableAlignmentCheck: true,
		EnableDataIntegrity:  false,                    // Disabled by default for performance
		MaxValidationTime:    config.MaxValidationTime, // Configurable validation timeout
	}
}

// ValidateAudioFrameFast performs minimal validation for performance-critical paths
func ValidateAudioFrameFast(data []byte) error {
	if len(data) == 0 {
		return ErrInvalidFrameData
	}

	// Quick bounds check using direct config access
	config := GetConfig()
	if len(data) > config.MaxAudioFrameSize {
		return fmt.Errorf("%w: frame size %d exceeds maximum %d", ErrInvalidFrameSize, len(data), config.MaxAudioFrameSize)
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
		// Use direct config access
		config := GetConfig()
		if len(data) < config.MinFrameSize {
			return fmt.Errorf("%w: frame size %d below minimum %d", ErrInvalidFrameSize, len(data), config.MinFrameSize)
		}

		// Validate frame length matches expected sample format
		expectedFrameSize := (expectedSampleRate * expectedChannels * 2) / 1000 * int(config.AudioQualityMediumFrameSize/time.Millisecond)
		if abs(len(data)-expectedFrameSize) > config.FrameSizeTolerance {
			return fmt.Errorf("%w: frame size %d doesn't match expected %d (±%d)", ErrInvalidFrameLength, len(data), expectedFrameSize, config.FrameSizeTolerance)
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

	// Use direct config access
	configConstants := GetConfig()
	minOpusBitrate := configConstants.MinOpusBitrate
	maxOpusBitrate := configConstants.MaxOpusBitrate
	maxChannels := configConstants.MaxChannels
	validSampleRates := configConstants.ValidSampleRates
	minFrameDuration := configConstants.MinFrameDuration
	maxFrameDuration := configConstants.MaxFrameDuration

	// Validate bitrate ranges
	if config.Bitrate < minOpusBitrate || config.Bitrate > maxOpusBitrate {
		return fmt.Errorf("%w: bitrate %d outside valid range [%d, %d]", ErrInvalidConfiguration, config.Bitrate, minOpusBitrate, maxOpusBitrate)
	}

	// Validate sample rate
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
	if config.Channels < 1 || config.Channels > maxChannels {
		return fmt.Errorf("%w: channels %d outside valid range [1, %d]", ErrInvalidChannels, config.Channels, maxChannels)
	}

	// Validate frame size
	if config.FrameSize < minFrameDuration || config.FrameSize > maxFrameDuration {
		return fmt.Errorf("%w: frame size %v outside valid range [%v, %v]", ErrInvalidConfiguration, config.FrameSize, minFrameDuration, maxFrameDuration)
	}

	return nil
}

// ValidateAudioConfigConstants performs comprehensive validation of AudioConfigConstants
func ValidateAudioConfigConstants(config *AudioConfigConstants) error {
	if config == nil {
		return fmt.Errorf("%w: configuration is nil", ErrInvalidConfiguration)
	}

	// Validate basic audio parameters
	if config.MaxAudioFrameSize <= 0 {
		return fmt.Errorf("%w: MaxAudioFrameSize must be positive", ErrInvalidConfiguration)
	}
	if config.SampleRate <= 0 {
		return fmt.Errorf("%w: SampleRate must be positive", ErrInvalidSampleRate)
	}
	if config.Channels <= 0 || config.Channels > 8 {
		return fmt.Errorf("%w: Channels must be between 1 and 8", ErrInvalidChannels)
	}

	// Validate Opus parameters
	if config.OpusBitrate < 6000 || config.OpusBitrate > 510000 {
		return fmt.Errorf("%w: OpusBitrate must be between 6000 and 510000", ErrInvalidConfiguration)
	}
	if config.OpusComplexity < 0 || config.OpusComplexity > 10 {
		return fmt.Errorf("%w: OpusComplexity must be between 0 and 10", ErrInvalidConfiguration)
	}

	// Validate bitrate ranges
	if config.MinOpusBitrate <= 0 || config.MaxOpusBitrate <= 0 {
		return fmt.Errorf("%w: MinOpusBitrate and MaxOpusBitrate must be positive", ErrInvalidConfiguration)
	}
	if config.MinOpusBitrate >= config.MaxOpusBitrate {
		return fmt.Errorf("%w: MinOpusBitrate must be less than MaxOpusBitrate", ErrInvalidConfiguration)
	}

	// Validate sample rate ranges
	if config.MinSampleRate <= 0 || config.MaxSampleRate <= 0 {
		return fmt.Errorf("%w: MinSampleRate and MaxSampleRate must be positive", ErrInvalidSampleRate)
	}
	if config.MinSampleRate >= config.MaxSampleRate {
		return fmt.Errorf("%w: MinSampleRate must be less than MaxSampleRate", ErrInvalidSampleRate)
	}

	// Validate frame duration ranges
	if config.MinFrameDuration <= 0 || config.MaxFrameDuration <= 0 {
		return fmt.Errorf("%w: MinFrameDuration and MaxFrameDuration must be positive", ErrInvalidConfiguration)
	}
	if config.MinFrameDuration >= config.MaxFrameDuration {
		return fmt.Errorf("%w: MinFrameDuration must be less than MaxFrameDuration", ErrInvalidConfiguration)
	}

	// Validate buffer sizes
	if config.SocketMinBuffer <= 0 || config.SocketMaxBuffer <= 0 {
		return fmt.Errorf("%w: SocketMinBuffer and SocketMaxBuffer must be positive", ErrInvalidBufferSize)
	}
	if config.SocketMinBuffer >= config.SocketMaxBuffer {
		return fmt.Errorf("%w: SocketMinBuffer must be less than SocketMaxBuffer", ErrInvalidBufferSize)
	}

	// Validate priority ranges
	if config.MinNiceValue < -20 || config.MinNiceValue > 19 {
		return fmt.Errorf("%w: MinNiceValue must be between -20 and 19", ErrInvalidPriority)
	}
	if config.MaxNiceValue < -20 || config.MaxNiceValue > 19 {
		return fmt.Errorf("%w: MaxNiceValue must be between -20 and 19", ErrInvalidPriority)
	}
	if config.MinNiceValue >= config.MaxNiceValue {
		return fmt.Errorf("%w: MinNiceValue must be less than MaxNiceValue", ErrInvalidPriority)
	}

	// Validate timeout values
	if config.MaxValidationTime <= 0 {
		return fmt.Errorf("%w: MaxValidationTime must be positive", ErrInvalidConfiguration)
	}
	if config.RestartDelay <= 0 || config.MaxRestartDelay <= 0 {
		return fmt.Errorf("%w: RestartDelay and MaxRestartDelay must be positive", ErrInvalidConfiguration)
	}
	if config.RestartDelay >= config.MaxRestartDelay {
		return fmt.Errorf("%w: RestartDelay must be less than MaxRestartDelay", ErrInvalidConfiguration)
	}

	// Validate valid sample rates array
	if len(config.ValidSampleRates) == 0 {
		return fmt.Errorf("%w: ValidSampleRates cannot be empty", ErrInvalidSampleRate)
	}
	for _, rate := range config.ValidSampleRates {
		if rate <= 0 {
			return fmt.Errorf("%w: all ValidSampleRates must be positive", ErrInvalidSampleRate)
		}
		if rate < config.MinSampleRate || rate > config.MaxSampleRate {
			return fmt.Errorf("%w: ValidSampleRate %d outside range [%d, %d]", ErrInvalidSampleRate, rate, config.MinSampleRate, config.MaxSampleRate)
		}
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

// ErrorContext provides structured error context for better debugging
type ErrorContext struct {
	Component  string                 `json:"component"`
	Operation  string                 `json:"operation"`
	Timestamp  time.Time              `json:"timestamp"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
	StackTrace []string               `json:"stack_trace,omitempty"`
}

// ContextualError wraps an error with additional context
type ContextualError struct {
	Err     error        `json:"error"`
	Context ErrorContext `json:"context"`
}

func (ce *ContextualError) Error() string {
	return fmt.Sprintf("%s [%s:%s]: %v", ce.Context.Component, ce.Context.Operation, ce.Context.Timestamp.Format(time.RFC3339), ce.Err)
}

func (ce *ContextualError) Unwrap() error {
	return ce.Err
}

// NewContextualError creates a new contextual error with metadata
func NewContextualError(err error, component, operation string, metadata map[string]interface{}) *ContextualError {
	return &ContextualError{
		Err: err,
		Context: ErrorContext{
			Component: component,
			Operation: operation,
			Timestamp: time.Now(),
			Metadata:  metadata,
		},
	}
}

// WrapWithContext wraps an error with component and operation context
func WrapWithContext(err error, component, operation string) error {
	if err == nil {
		return nil
	}
	return NewContextualError(err, component, operation, nil)
}

// WrapWithMetadata wraps an error with additional metadata
func WrapWithMetadata(err error, component, operation string, metadata map[string]interface{}) error {
	if err == nil {
		return nil
	}
	return NewContextualError(err, component, operation, metadata)
}

// IsTimeoutError checks if an error is a timeout error
func IsTimeoutError(err error) bool {
	return errors.Is(err, ErrOperationTimeout)
}

// IsResourceError checks if an error is related to resource exhaustion
func IsResourceError(err error) bool {
	return errors.Is(err, ErrResourceExhaustion) || errors.Is(err, ErrMemoryExhaustion) || errors.Is(err, ErrSystemOverload)
}

// IsHardwareError checks if an error is hardware-related
func IsHardwareError(err error) bool {
	return errors.Is(err, ErrHardwareFailure)
}

// IsNetworkError checks if an error is network-related
func IsNetworkError(err error) bool {
	return errors.Is(err, ErrNetworkError)
}

// GetErrorSeverity returns the severity level of an error
func GetErrorSeverity(err error) string {
	if IsHardwareError(err) {
		return "critical"
	}
	if IsResourceError(err) {
		return "high"
	}
	if IsNetworkError(err) || IsTimeoutError(err) {
		return "medium"
	}
	return "low"
}
