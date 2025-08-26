package audio

import (
	"errors"
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
)

// ValidateAudioQuality validates audio quality enum values
func ValidateAudioQuality(quality AudioQuality) error {
	switch quality {
	case AudioQualityLow, AudioQualityMedium, AudioQualityHigh, AudioQualityUltra:
		return nil
	default:
		return ErrInvalidAudioQuality
	}
}

// ValidateFrameData validates audio frame data
func ValidateFrameData(data []byte) error {
	if len(data) == 0 {
		return ErrInvalidFrameData
	}
	// Use a reasonable default if config is not available
	maxFrameSize := 4096
	if config := GetConfig(); config != nil {
		maxFrameSize = config.MaxAudioFrameSize
	}
	if len(data) > maxFrameSize {
		return ErrInvalidFrameSize
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
	// Use a reasonable default if config is not available
	maxFrameSize := 4096
	if config := GetConfig(); config != nil {
		maxFrameSize = config.MaxAudioFrameSize
	}
	if len(data) > maxFrameSize {
		return ErrInvalidFrameSize
	}
	return nil
}

// ValidateBufferSize validates buffer size parameters
func ValidateBufferSize(size int) error {
	if size <= 0 {
		return ErrInvalidBufferSize
	}
	// Use a reasonable default if config is not available
	maxBuffer := 262144 // 256KB default
	if config := GetConfig(); config != nil {
		maxBuffer = config.SocketMaxBuffer
	}
	if size > maxBuffer {
		return ErrInvalidBufferSize
	}
	return nil
}

// ValidateThreadPriority validates thread priority values
func ValidateThreadPriority(priority int) error {
	// Use reasonable defaults if config is not available
	minPriority := -20
	maxPriority := 99
	if config := GetConfig(); config != nil {
		minPriority = config.MinNiceValue
		maxPriority = config.RTAudioHighPriority
	}
	if priority < minPriority || priority > maxPriority {
		return ErrInvalidPriority
	}
	return nil
}

// ValidateLatency validates latency values
func ValidateLatency(latency time.Duration) error {
	if latency < 0 {
		return ErrInvalidLatency
	}
	// Use a reasonable default if config is not available
	maxLatency := 500 * time.Millisecond
	if config := GetConfig(); config != nil {
		maxLatency = config.MaxLatency
	}
	if latency > maxLatency {
		return ErrInvalidLatency
	}
	return nil
}

// ValidateMetricsInterval validates metrics update interval
func ValidateMetricsInterval(interval time.Duration) error {
	// Use reasonable defaults if config is not available
	minInterval := 100 * time.Millisecond
	maxInterval := 10 * time.Second
	if config := GetConfig(); config != nil {
		minInterval = config.MinMetricsUpdateInterval
		maxInterval = config.MaxMetricsUpdateInterval
	}
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
	maxBuffer := 262144 // 256KB default
	if config := GetConfig(); config != nil {
		maxBuffer = config.SocketMaxBuffer
	}
	if maxSize > maxBuffer {
		return ErrInvalidBufferSize
	}
	return nil
}

// ValidateInputIPCConfig validates input IPC configuration
func ValidateInputIPCConfig(sampleRate, channels, frameSize int) error {
	// Use reasonable defaults if config is not available
	minSampleRate := 8000
	maxSampleRate := 48000
	maxChannels := 8
	if config := GetConfig(); config != nil {
		minSampleRate = config.MinSampleRate
		maxSampleRate = config.MaxSampleRate
		maxChannels = config.MaxChannels
	}
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
