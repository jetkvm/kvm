package audio

import (
	"runtime"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// Audio output metrics
	audioFramesReceivedTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "jetkvm_audio_frames_received_total",
			Help: "Total number of audio frames received",
		},
	)

	audioFramesDroppedTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "jetkvm_audio_frames_dropped_total",
			Help: "Total number of audio frames dropped",
		},
	)

	audioBytesProcessedTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "jetkvm_audio_bytes_processed_total",
			Help: "Total number of audio bytes processed",
		},
	)

	audioConnectionDropsTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "jetkvm_audio_connection_drops_total",
			Help: "Total number of audio connection drops",
		},
	)

	audioAverageLatencyMilliseconds = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "jetkvm_audio_average_latency_milliseconds",
			Help: "Average audio latency in milliseconds",
		},
	)

	audioLastFrameTimestamp = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "jetkvm_audio_last_frame_timestamp_seconds",
			Help: "Timestamp of the last audio frame received",
		},
	)

	// Microphone input metrics
	microphoneFramesSentTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "jetkvm_microphone_frames_sent_total",
			Help: "Total number of microphone frames sent",
		},
	)

	microphoneFramesDroppedTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "jetkvm_microphone_frames_dropped_total",
			Help: "Total number of microphone frames dropped",
		},
	)

	microphoneBytesProcessedTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "jetkvm_microphone_bytes_processed_total",
			Help: "Total number of microphone bytes processed",
		},
	)

	microphoneConnectionDropsTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "jetkvm_microphone_connection_drops_total",
			Help: "Total number of microphone connection drops",
		},
	)

	microphoneAverageLatencyMilliseconds = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "jetkvm_microphone_average_latency_milliseconds",
			Help: "Average microphone latency in milliseconds",
		},
	)

	microphoneLastFrameTimestamp = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "jetkvm_microphone_last_frame_timestamp_seconds",
			Help: "Timestamp of the last microphone frame sent",
		},
	)

	// Memory metrics (basic monitoring)
	memoryHeapAllocBytes = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "jetkvm_audio_memory_heap_alloc_bytes",
			Help: "Current heap allocation in bytes",
		},
	)

	memoryGCCount = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "jetkvm_audio_memory_gc_total",
			Help: "Total number of garbage collections",
		},
	)

	// Metrics update tracking
	lastMetricsUpdate int64

	// Counter value tracking (since prometheus counters don't have Get() method)
	audioFramesReceivedValue  uint64
	audioFramesDroppedValue   uint64
	audioBytesProcessedValue  uint64
	audioConnectionDropsValue uint64
	micFramesSentValue        uint64
	micFramesDroppedValue     uint64
	micBytesProcessedValue    uint64
	micConnectionDropsValue   uint64

	// Atomic counter for memory GC
	memoryGCCountValue uint32
)

// UnifiedAudioMetrics provides a common structure for both input and output audio streams
type UnifiedAudioMetrics struct {
	FramesReceived  uint64        `json:"frames_received"`
	FramesDropped   uint64        `json:"frames_dropped"`
	FramesSent      uint64        `json:"frames_sent,omitempty"`
	BytesProcessed  uint64        `json:"bytes_processed"`
	ConnectionDrops uint64        `json:"connection_drops"`
	LastFrameTime   time.Time     `json:"last_frame_time"`
	AverageLatency  time.Duration `json:"average_latency"`
}

// convertAudioInputMetricsToUnified converts AudioInputMetrics to UnifiedAudioMetrics
func convertAudioInputMetricsToUnified(metrics AudioInputMetrics) UnifiedAudioMetrics {
	return UnifiedAudioMetrics{
		FramesReceived:  0, // AudioInputMetrics doesn't have FramesReceived
		FramesDropped:   uint64(metrics.FramesDropped),
		FramesSent:      uint64(metrics.FramesSent),
		BytesProcessed:  uint64(metrics.BytesProcessed),
		ConnectionDrops: uint64(metrics.ConnectionDrops),
		LastFrameTime:   metrics.LastFrameTime,
		AverageLatency:  metrics.AverageLatency,
	}
}

// UpdateAudioMetrics updates Prometheus metrics with current audio data
func UpdateAudioMetrics(metrics UnifiedAudioMetrics) {
	oldReceived := atomic.SwapUint64(&audioFramesReceivedValue, metrics.FramesReceived)
	if metrics.FramesReceived > oldReceived {
		audioFramesReceivedTotal.Add(float64(metrics.FramesReceived - oldReceived))
	}

	oldDropped := atomic.SwapUint64(&audioFramesDroppedValue, metrics.FramesDropped)
	if metrics.FramesDropped > oldDropped {
		audioFramesDroppedTotal.Add(float64(metrics.FramesDropped - oldDropped))
	}

	oldBytes := atomic.SwapUint64(&audioBytesProcessedValue, metrics.BytesProcessed)
	if metrics.BytesProcessed > oldBytes {
		audioBytesProcessedTotal.Add(float64(metrics.BytesProcessed - oldBytes))
	}

	oldDrops := atomic.SwapUint64(&audioConnectionDropsValue, metrics.ConnectionDrops)
	if metrics.ConnectionDrops > oldDrops {
		audioConnectionDropsTotal.Add(float64(metrics.ConnectionDrops - oldDrops))
	}

	// Update gauges
	audioAverageLatencyMilliseconds.Set(float64(metrics.AverageLatency.Nanoseconds()) / 1e6)
	if !metrics.LastFrameTime.IsZero() {
		audioLastFrameTimestamp.Set(float64(metrics.LastFrameTime.Unix()))
	}

	atomic.StoreInt64(&lastMetricsUpdate, time.Now().Unix())
}

// UpdateMicrophoneMetrics updates Prometheus metrics with current microphone data
func UpdateMicrophoneMetrics(metrics UnifiedAudioMetrics) {
	oldSent := atomic.SwapUint64(&micFramesSentValue, metrics.FramesSent)
	if metrics.FramesSent > oldSent {
		microphoneFramesSentTotal.Add(float64(metrics.FramesSent - oldSent))
	}

	oldDropped := atomic.SwapUint64(&micFramesDroppedValue, metrics.FramesDropped)
	if metrics.FramesDropped > oldDropped {
		microphoneFramesDroppedTotal.Add(float64(metrics.FramesDropped - oldDropped))
	}

	oldBytes := atomic.SwapUint64(&micBytesProcessedValue, metrics.BytesProcessed)
	if metrics.BytesProcessed > oldBytes {
		microphoneBytesProcessedTotal.Add(float64(metrics.BytesProcessed - oldBytes))
	}

	oldDrops := atomic.SwapUint64(&micConnectionDropsValue, metrics.ConnectionDrops)
	if metrics.ConnectionDrops > oldDrops {
		microphoneConnectionDropsTotal.Add(float64(metrics.ConnectionDrops - oldDrops))
	}

	// Update gauges
	microphoneAverageLatencyMilliseconds.Set(float64(metrics.AverageLatency.Nanoseconds()) / 1e6)
	if !metrics.LastFrameTime.IsZero() {
		microphoneLastFrameTimestamp.Set(float64(metrics.LastFrameTime.Unix()))
	}

	atomic.StoreInt64(&lastMetricsUpdate, time.Now().Unix())
}

// UpdateMemoryMetrics updates basic memory metrics
func UpdateMemoryMetrics() {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	memoryHeapAllocBytes.Set(float64(m.HeapAlloc))

	// Update GC count with delta calculation
	currentGCCount := uint32(m.NumGC)
	prevGCCount := atomic.SwapUint32(&memoryGCCountValue, currentGCCount)
	if prevGCCount > 0 && currentGCCount > prevGCCount {
		memoryGCCount.Add(float64(currentGCCount - prevGCCount))
	}

	atomic.StoreInt64(&lastMetricsUpdate, time.Now().Unix())
}

// GetLastMetricsUpdate returns the timestamp of the last metrics update
func GetLastMetricsUpdate() time.Time {
	timestamp := atomic.LoadInt64(&lastMetricsUpdate)
	return time.Unix(timestamp, 0)
}

// StartMetricsUpdater starts a goroutine that periodically updates Prometheus metrics
func StartMetricsUpdater() {
	// Start the centralized metrics collector
	registry := GetMetricsRegistry()
	registry.StartMetricsCollector()

	// Start a separate goroutine for periodic updates
	go func() {
		ticker := time.NewTicker(5 * time.Second) // Update every 5 seconds
		defer ticker.Stop()

		for range ticker.C {
			// Update memory metrics (not part of centralized registry)
			UpdateMemoryMetrics()
		}
	}()
}
