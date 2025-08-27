//go:build cgo
// +build cgo

package audio

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestValidationFunctions provides comprehensive testing of all validation functions
// to ensure they catch breaking changes and regressions effectively
func TestValidationFunctions(t *testing.T) {
	// Initialize validation cache for testing
	InitValidationCache()

	tests := []struct {
		name     string
		testFunc func(t *testing.T)
	}{
		{"AudioQualityValidation", testAudioQualityValidation},
		{"FrameDataValidation", testFrameDataValidation},
		{"BufferSizeValidation", testBufferSizeValidation},
		{"ThreadPriorityValidation", testThreadPriorityValidation},
		{"LatencyValidation", testLatencyValidation},
		{"MetricsIntervalValidation", testMetricsIntervalValidation},
		{"SampleRateValidation", testSampleRateValidation},
		{"ChannelCountValidation", testChannelCountValidation},
		{"BitrateValidation", testBitrateValidation},
		{"FrameDurationValidation", testFrameDurationValidation},
		{"IPCConfigValidation", testIPCConfigValidation},
		{"AdaptiveBufferConfigValidation", testAdaptiveBufferConfigValidation},
		{"AudioConfigCompleteValidation", testAudioConfigCompleteValidation},
		{"ZeroCopyFrameValidation", testZeroCopyFrameValidation},
		{"AudioFrameFastValidation", testAudioFrameFastValidation},
		{"ErrorWrappingValidation", testErrorWrappingValidation},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.testFunc(t)
		})
	}
}

// testAudioQualityValidation tests audio quality validation with boundary conditions
func testAudioQualityValidation(t *testing.T) {
	// Test valid quality levels
	validQualities := []AudioQuality{AudioQualityLow, AudioQualityMedium, AudioQualityHigh, AudioQualityUltra}
	for _, quality := range validQualities {
		err := ValidateAudioQuality(quality)
		assert.NoError(t, err, "Valid quality %d should pass validation", quality)
	}

	// Test invalid quality levels
	invalidQualities := []AudioQuality{-1, 4, 100, -100}
	for _, quality := range invalidQualities {
		err := ValidateAudioQuality(quality)
		assert.Error(t, err, "Invalid quality %d should fail validation", quality)
		assert.Contains(t, err.Error(), "invalid audio quality level", "Error should mention audio quality")
	}
}

// testFrameDataValidation tests frame data validation with various edge cases using modern validation
func testFrameDataValidation(t *testing.T) {
	config := GetConfig()

	// Test empty data
	err := ValidateAudioFrame([]byte{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "frame data is empty")

	// Test data above maximum size
	largeData := make([]byte, config.MaxAudioFrameSize+1)
	err = ValidateAudioFrame(largeData)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds maximum")

	// Test valid data
	validData := make([]byte, 1000) // Within bounds
	if len(validData) <= config.MaxAudioFrameSize {
		err = ValidateAudioFrame(validData)
		assert.NoError(t, err)
	}
}

// testBufferSizeValidation tests buffer size validation
func testBufferSizeValidation(t *testing.T) {
	config := GetConfig()

	// Test negative and zero sizes
	invalidSizes := []int{-1, -100, 0}
	for _, size := range invalidSizes {
		err := ValidateBufferSize(size)
		assert.Error(t, err, "Buffer size %d should be invalid", size)
		assert.Contains(t, err.Error(), "must be positive")
	}

	// Test size exceeding maximum
	err := ValidateBufferSize(config.SocketMaxBuffer + 1)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds maximum")

	// Test valid sizes
	validSizes := []int{1, 1024, 4096, config.SocketMaxBuffer}
	for _, size := range validSizes {
		err := ValidateBufferSize(size)
		assert.NoError(t, err, "Buffer size %d should be valid", size)
	}
}

// testThreadPriorityValidation tests thread priority validation
func testThreadPriorityValidation(t *testing.T) {
	// Test valid priorities
	validPriorities := []int{-20, -10, 0, 10, 19}
	for _, priority := range validPriorities {
		err := ValidateThreadPriority(priority)
		assert.NoError(t, err, "Priority %d should be valid", priority)
	}

	// Test invalid priorities
	invalidPriorities := []int{-21, -100, 20, 100}
	for _, priority := range invalidPriorities {
		err := ValidateThreadPriority(priority)
		assert.Error(t, err, "Priority %d should be invalid", priority)
		assert.Contains(t, err.Error(), "outside valid range")
	}
}

// testLatencyValidation tests latency validation
func testLatencyValidation(t *testing.T) {
	config := GetConfig()

	// Test negative latency
	err := ValidateLatency(-1 * time.Millisecond)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cannot be negative")

	// Test zero latency (should be valid)
	err = ValidateLatency(0)
	assert.NoError(t, err)

	// Test very small positive latency
	err = ValidateLatency(500 * time.Microsecond)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "below minimum")

	// Test latency exceeding maximum
	err = ValidateLatency(config.MaxLatency + time.Second)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds maximum")

	// Test valid latencies
	validLatencies := []time.Duration{
		1 * time.Millisecond,
		10 * time.Millisecond,
		100 * time.Millisecond,
		config.MaxLatency,
	}
	for _, latency := range validLatencies {
		err := ValidateLatency(latency)
		assert.NoError(t, err, "Latency %v should be valid", latency)
	}
}

// testMetricsIntervalValidation tests metrics interval validation
func testMetricsIntervalValidation(t *testing.T) {
	config := GetConfig()

	// Test interval below minimum
	err := ValidateMetricsInterval(config.MinMetricsUpdateInterval - time.Millisecond)
	assert.Error(t, err)

	// Test interval above maximum
	err = ValidateMetricsInterval(config.MaxMetricsUpdateInterval + time.Second)
	assert.Error(t, err)

	// Test valid intervals
	validIntervals := []time.Duration{
		config.MinMetricsUpdateInterval,
		config.MaxMetricsUpdateInterval,
		(config.MinMetricsUpdateInterval + config.MaxMetricsUpdateInterval) / 2,
	}
	for _, interval := range validIntervals {
		err := ValidateMetricsInterval(interval)
		assert.NoError(t, err, "Interval %v should be valid", interval)
	}
}

// testSampleRateValidation tests sample rate validation
func testSampleRateValidation(t *testing.T) {
	config := GetConfig()

	// Test negative and zero sample rates
	invalidRates := []int{-1, -48000, 0}
	for _, rate := range invalidRates {
		err := ValidateSampleRate(rate)
		assert.Error(t, err, "Sample rate %d should be invalid", rate)
		assert.Contains(t, err.Error(), "must be positive")
	}

	// Test unsupported sample rates
	unsupportedRates := []int{1000, 12345, 96001}
	for _, rate := range unsupportedRates {
		err := ValidateSampleRate(rate)
		assert.Error(t, err, "Sample rate %d should be unsupported", rate)
		assert.Contains(t, err.Error(), "not in supported rates")
	}

	// Test valid sample rates
	for _, rate := range config.ValidSampleRates {
		err := ValidateSampleRate(rate)
		assert.NoError(t, err, "Sample rate %d should be valid", rate)
	}
}

// testChannelCountValidation tests channel count validation
func testChannelCountValidation(t *testing.T) {
	config := GetConfig()

	// Test invalid channel counts
	invalidCounts := []int{-1, -10, 0}
	for _, count := range invalidCounts {
		err := ValidateChannelCount(count)
		assert.Error(t, err, "Channel count %d should be invalid", count)
		assert.Contains(t, err.Error(), "must be positive")
	}

	// Test channel count exceeding maximum
	err := ValidateChannelCount(config.MaxChannels + 1)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds maximum")

	// Test valid channel counts
	validCounts := []int{1, 2, config.MaxChannels}
	for _, count := range validCounts {
		err := ValidateChannelCount(count)
		assert.NoError(t, err, "Channel count %d should be valid", count)
	}
}

// testBitrateValidation tests bitrate validation
func testBitrateValidation(t *testing.T) {
	// Test invalid bitrates
	invalidBitrates := []int{-1, -1000, 0}
	for _, bitrate := range invalidBitrates {
		err := ValidateBitrate(bitrate)
		assert.Error(t, err, "Bitrate %d should be invalid", bitrate)
		assert.Contains(t, err.Error(), "must be positive")
	}

	// Test bitrate below minimum (in kbps)
	err := ValidateBitrate(5) // 5 kbps = 5000 bps < 6000 bps minimum
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "below minimum")

	// Test bitrate above maximum (in kbps)
	err = ValidateBitrate(511) // 511 kbps = 511000 bps > 510000 bps maximum
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds maximum")

	// Test valid bitrates (in kbps)
	validBitrates := []int{
		6,   // 6 kbps = 6000 bps (minimum)
		64,  // Medium quality preset
		128, // High quality preset
		192, // Ultra quality preset
		510, // 510 kbps = 510000 bps (maximum)
	}
	for _, bitrate := range validBitrates {
		err := ValidateBitrate(bitrate)
		assert.NoError(t, err, "Bitrate %d kbps should be valid", bitrate)
	}
}

// testFrameDurationValidation tests frame duration validation
func testFrameDurationValidation(t *testing.T) {
	config := GetConfig()

	// Test invalid durations
	invalidDurations := []time.Duration{-1 * time.Millisecond, -1 * time.Second, 0}
	for _, duration := range invalidDurations {
		err := ValidateFrameDuration(duration)
		assert.Error(t, err, "Duration %v should be invalid", duration)
		assert.Contains(t, err.Error(), "must be positive")
	}

	// Test duration below minimum
	err := ValidateFrameDuration(config.MinFrameDuration - time.Microsecond)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "below minimum")

	// Test duration above maximum
	err = ValidateFrameDuration(config.MaxFrameDuration + time.Second)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds maximum")

	// Test valid durations
	validDurations := []time.Duration{
		config.MinFrameDuration,
		config.MaxFrameDuration,
		20 * time.Millisecond, // Common frame duration
	}
	for _, duration := range validDurations {
		err := ValidateFrameDuration(duration)
		assert.NoError(t, err, "Duration %v should be valid", duration)
	}
}

// testIPCConfigValidation tests IPC configuration validation
func testIPCConfigValidation(t *testing.T) {
	config := GetConfig()

	// Test invalid configurations for input IPC
	invalidConfigs := []struct {
		sampleRate, channels, frameSize int
		description                     string
	}{
		{0, 2, 960, "zero sample rate"},
		{48000, 0, 960, "zero channels"},
		{48000, 2, 0, "zero frame size"},
		{config.MinSampleRate - 1, 2, 960, "sample rate below minimum"},
		{config.MaxSampleRate + 1, 2, 960, "sample rate above maximum"},
		{48000, config.MaxChannels + 1, 960, "too many channels"},
		{48000, -1, 960, "negative channels"},
		{48000, 2, -1, "negative frame size"},
	}

	for _, tc := range invalidConfigs {
		// Test input IPC validation
		err := ValidateInputIPCConfig(tc.sampleRate, tc.channels, tc.frameSize)
		assert.Error(t, err, "Input IPC config should be invalid: %s", tc.description)

		// Test output IPC validation
		err = ValidateOutputIPCConfig(tc.sampleRate, tc.channels, tc.frameSize)
		assert.Error(t, err, "Output IPC config should be invalid: %s", tc.description)
	}

	// Test valid configuration
	err := ValidateInputIPCConfig(48000, 2, 960)
	assert.NoError(t, err)
	err = ValidateOutputIPCConfig(48000, 2, 960)
	assert.NoError(t, err)
}

// testAdaptiveBufferConfigValidation tests adaptive buffer configuration validation
func testAdaptiveBufferConfigValidation(t *testing.T) {
	config := GetConfig()

	// Test invalid configurations
	invalidConfigs := []struct {
		minSize, maxSize, defaultSize int
		description                   string
	}{
		{0, 1024, 512, "zero min size"},
		{-1, 1024, 512, "negative min size"},
		{512, 0, 256, "zero max size"},
		{512, -1, 256, "negative max size"},
		{512, 1024, 0, "zero default size"},
		{512, 1024, -1, "negative default size"},
		{1024, 512, 768, "min >= max"},
		{512, 1024, 256, "default < min"},
		{512, 1024, 2048, "default > max"},
		{512, config.SocketMaxBuffer + 1, 1024, "max exceeds global limit"},
	}

	for _, tc := range invalidConfigs {
		err := ValidateAdaptiveBufferConfig(tc.minSize, tc.maxSize, tc.defaultSize)
		assert.Error(t, err, "Config should be invalid: %s", tc.description)
	}

	// Test valid configuration
	err := ValidateAdaptiveBufferConfig(512, 4096, 1024)
	assert.NoError(t, err)
}

// testAudioConfigCompleteValidation tests complete audio configuration validation
func testAudioConfigCompleteValidation(t *testing.T) {
	// Test valid configuration using actual preset values
	validConfig := AudioConfig{
		Quality:    AudioQualityMedium,
		Bitrate:    64, // kbps - matches medium quality preset
		SampleRate: 48000,
		Channels:   2,
		FrameSize:  20 * time.Millisecond,
	}
	err := ValidateAudioConfigComplete(validConfig)
	assert.NoError(t, err)

	// Test invalid quality
	invalidQualityConfig := validConfig
	invalidQualityConfig.Quality = AudioQuality(99)
	err = ValidateAudioConfigComplete(invalidQualityConfig)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "quality validation failed")

	// Test invalid bitrate
	invalidBitrateConfig := validConfig
	invalidBitrateConfig.Bitrate = -1
	err = ValidateAudioConfigComplete(invalidBitrateConfig)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "bitrate validation failed")

	// Test invalid sample rate
	invalidSampleRateConfig := validConfig
	invalidSampleRateConfig.SampleRate = 12345
	err = ValidateAudioConfigComplete(invalidSampleRateConfig)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "sample rate validation failed")

	// Test invalid channels
	invalidChannelsConfig := validConfig
	invalidChannelsConfig.Channels = 0
	err = ValidateAudioConfigComplete(invalidChannelsConfig)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "channel count validation failed")

	// Test invalid frame duration
	invalidFrameDurationConfig := validConfig
	invalidFrameDurationConfig.FrameSize = -1 * time.Millisecond
	err = ValidateAudioConfigComplete(invalidFrameDurationConfig)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "frame duration validation failed")
}

// testZeroCopyFrameValidation tests zero-copy frame validation
func testZeroCopyFrameValidation(t *testing.T) {
	// Test nil frame
	err := ValidateZeroCopyFrame(nil)
	assert.Error(t, err)

	// Note: We can't easily test ZeroCopyAudioFrame without creating actual instances
	// This would require more complex setup, but the validation logic is tested
}

// testAudioFrameFastValidation tests fast audio frame validation
func testAudioFrameFastValidation(t *testing.T) {
	config := GetConfig()

	// Test empty data
	err := ValidateAudioFrame([]byte{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "frame data is empty")

	// Test data exceeding maximum size
	largeData := make([]byte, config.MaxAudioFrameSize+1)
	err = ValidateAudioFrame(largeData)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds maximum")

	// Test valid data
	validData := make([]byte, 1000)
	err = ValidateAudioFrame(validData)
	assert.NoError(t, err)
}

// testErrorWrappingValidation tests error wrapping functionality
func testErrorWrappingValidation(t *testing.T) {
	// Test wrapping nil error
	wrapped := WrapWithMetadata(nil, "component", "operation", map[string]interface{}{"key": "value"})
	assert.Nil(t, wrapped)

	// Test wrapping actual error
	originalErr := assert.AnError
	metadata := map[string]interface{}{
		"frame_size": 1024,
		"quality":    "high",
	}
	wrapped = WrapWithMetadata(originalErr, "audio", "decode", metadata)
	require.NotNil(t, wrapped)
	assert.Contains(t, wrapped.Error(), "audio.decode")
	assert.Contains(t, wrapped.Error(), "assert.AnError")
	assert.Contains(t, wrapped.Error(), "metadata")
	assert.Contains(t, wrapped.Error(), "frame_size")
	assert.Contains(t, wrapped.Error(), "quality")
}

// TestValidationIntegration tests validation functions working together
func TestValidationIntegration(t *testing.T) {
	// Test that validation functions work correctly with actual audio configurations
	presets := GetAudioQualityPresets()
	require.NotEmpty(t, presets)

	for quality, config := range presets {
		t.Run(fmt.Sprintf("Quality_%d", quality), func(t *testing.T) {
			// Validate the preset configuration
			err := ValidateAudioConfigComplete(config)
			assert.NoError(t, err, "Preset configuration for quality %d should be valid", quality)

			// Validate individual components
			err = ValidateAudioQuality(config.Quality)
			assert.NoError(t, err, "Quality should be valid")

			err = ValidateBitrate(config.Bitrate)
			assert.NoError(t, err, "Bitrate should be valid")

			err = ValidateSampleRate(config.SampleRate)
			assert.NoError(t, err, "Sample rate should be valid")

			err = ValidateChannelCount(config.Channels)
			assert.NoError(t, err, "Channel count should be valid")

			err = ValidateFrameDuration(config.FrameSize)
			assert.NoError(t, err, "Frame duration should be valid")
		})
	}
}

// TestValidationPerformance ensures validation functions are efficient
func TestValidationPerformance(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance test in short mode")
	}

	// Initialize validation cache for performance testing
	InitValidationCache()

	// Test that validation functions complete quickly
	start := time.Now()
	iterations := 10000

	for i := 0; i < iterations; i++ {
		_ = ValidateAudioQuality(AudioQualityMedium)
		_ = ValidateBufferSize(1024)
		_ = ValidateChannelCount(2)
		_ = ValidateSampleRate(48000)
		_ = ValidateBitrate(96) // 96 kbps
	}

	elapsed := time.Since(start)
	perIteration := elapsed / time.Duration(iterations)

	// Performance expectations for JetKVM (ARM Cortex-A7 @ 1GHz, 256MB RAM)
	// Audio processing must not interfere with primary KVM functionality
	assert.Less(t, perIteration, 200*time.Microsecond, "Validation should not impact KVM performance")
	t.Logf("Validation performance: %v per iteration", perIteration)
}
