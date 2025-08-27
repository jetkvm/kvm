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
	// Perform validation
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
	// Use config value
	maxFrameSize := GetConfig().MaxAudioFrameSize
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
	// Use config value
	maxFrameSize := GetConfig().MaxAudioFrameSize
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
	// Use config value
	maxBuffer := GetConfig().SocketMaxBuffer
	if size > maxBuffer {
		return ErrInvalidBufferSize
	}
	return nil
}

// ValidateThreadPriority validates thread priority values
func ValidateThreadPriority(priority int) error {
	// Use config values
	config := GetConfig()
	if priority < config.MinNiceValue || priority > config.RTAudioHighPriority {
		return ErrInvalidPriority
	}
	return nil
}

// ValidateLatency validates latency values
func ValidateLatency(latency time.Duration) error {
	if latency < 0 {
		return ErrInvalidLatency
	}
	// Use config value
	maxLatency := GetConfig().MaxLatency
	if latency > maxLatency {
		return ErrInvalidLatency
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
