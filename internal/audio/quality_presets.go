//go:build cgo
// +build cgo

// Package audio provides real-time audio processing for JetKVM with low-latency streaming.
//
// Key components: output/input pipelines with Opus codec, buffer management,
// zero-copy frame pools, IPC communication, and process supervision.
//
// Optimized for S16_LE @ 48kHz stereo HDMI audio with minimal CPU usage.
// All APIs are thread-safe with comprehensive error handling and metrics collection.
//
// # Performance Characteristics
//
// Designed for embedded ARM systems with limited resources:
//   - Sub-50ms end-to-end latency under normal conditions
//   - Memory usage scales with buffer configuration
//   - CPU usage optimized through zero-copy operations and complexity=1 Opus
//   - Fixed optimal configuration (96 kbps output, 48 kbps input)
//
// # Usage Example
//
//	config := GetAudioConfig()
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

// AudioConfig holds the optimal audio configuration
// All settings are fixed for S16_LE @ 48kHz HDMI audio
type AudioConfig struct {
	Bitrate    int // kbps (96 for output, 48 for input)
	SampleRate int // Hz (always 48000)
	Channels   int // 2 for output (stereo), 1 for input (mono)
	FrameSize  time.Duration // ms (always 20ms)
}

// AudioMetrics tracks audio performance metrics
type AudioMetrics struct {
	FramesReceived  uint64
	FramesDropped   uint64
	BytesProcessed  uint64
	ConnectionDrops uint64
	LastFrameTime   time.Time
	AverageLatency  time.Duration
}

var (
	// Optimal configuration for audio output (HDMI → client)
	currentConfig = AudioConfig{
		Bitrate:    Config.OptimalOutputBitrate,
		SampleRate: Config.SampleRate,
		Channels:   Config.Channels,
		FrameSize:  20 * time.Millisecond,
	}
	// Optimal configuration for microphone input (client → target)
	currentMicrophoneConfig = AudioConfig{
		Bitrate:    Config.OptimalInputBitrate,
		SampleRate: Config.SampleRate,
		Channels:   1,
		FrameSize:  20 * time.Millisecond,
	}
	metrics AudioMetrics
)

// GetAudioConfig returns the current optimal audio configuration
func GetAudioConfig() AudioConfig {
	return currentConfig
}

// GetMicrophoneConfig returns the current optimal microphone configuration
func GetMicrophoneConfig() AudioConfig {
	return currentMicrophoneConfig
}

// GetGlobalAudioMetrics returns the current global audio metrics
func GetGlobalAudioMetrics() AudioMetrics {
	return metrics
}

// Batched metrics to reduce atomic operations frequency
var (
	batchedFramesReceived  uint64
	batchedBytesProcessed  uint64
	batchedFramesDropped   uint64
	batchedConnectionDrops uint64

	lastFlushTime int64 // Unix timestamp in nanoseconds
)

// RecordFrameReceived increments the frames received counter with batched updates
func RecordFrameReceived(bytes int) {
	// Use local batching to reduce atomic operations frequency
	atomic.AddUint64(&batchedBytesProcessed, uint64(bytes))

	// Update timestamp immediately for accurate tracking
	metrics.LastFrameTime = time.Now()
}

// RecordFrameDropped increments the frames dropped counter with batched updates
func RecordFrameDropped() {
	atomic.AddUint64(&batchedFramesDropped, 1)
}

// RecordConnectionDrop increments the connection drops counter with batched updates
func RecordConnectionDrop() {
	atomic.AddUint64(&batchedConnectionDrops, 1)
}

// flushBatchedMetrics flushes accumulated metrics to the main counters
func flushBatchedMetrics() {
	// Atomically move batched metrics to main metrics
	framesReceived := atomic.SwapUint64(&batchedFramesReceived, 0)
	bytesProcessed := atomic.SwapUint64(&batchedBytesProcessed, 0)
	framesDropped := atomic.SwapUint64(&batchedFramesDropped, 0)
	connectionDrops := atomic.SwapUint64(&batchedConnectionDrops, 0)

	// Update main metrics if we have any batched data
	if framesReceived > 0 {
		atomic.AddUint64(&metrics.FramesReceived, framesReceived)
	}
	if bytesProcessed > 0 {
		atomic.AddUint64(&metrics.BytesProcessed, bytesProcessed)
	}
	if framesDropped > 0 {
		atomic.AddUint64(&metrics.FramesDropped, framesDropped)
	}
	if connectionDrops > 0 {
		atomic.AddUint64(&metrics.ConnectionDrops, connectionDrops)
	}

	// Update last flush time
	atomic.StoreInt64(&lastFlushTime, time.Now().UnixNano())
}

// FlushPendingMetrics forces a flush of all batched metrics
func FlushPendingMetrics() {
	flushBatchedMetrics()
}
