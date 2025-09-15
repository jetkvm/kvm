package audio

import (
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// Socket buffer metrics
	socketBufferSizeGauge = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "jetkvm_audio_socket_buffer_size_bytes",
			Help: "Current socket buffer size in bytes",
		},
		[]string{"component", "buffer_type"}, // buffer_type: send, receive
	)

	socketBufferUtilizationGauge = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "jetkvm_audio_socket_buffer_utilization_percent",
			Help: "Socket buffer utilization percentage",
		},
		[]string{"component", "buffer_type"}, // buffer_type: send, receive
	)

	socketBufferOverflowCounter = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "jetkvm_audio_socket_buffer_overflow_total",
			Help: "Total number of socket buffer overflows",
		},
		[]string{"component", "buffer_type"}, // buffer_type: send, receive
	)

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

	// Device health metrics
	// Removed device health metrics - functionality not used

	// Memory metrics
	memoryHeapAllocBytes = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "jetkvm_audio_memory_heap_alloc_bytes",
			Help: "Current heap allocation in bytes",
		},
	)

	memoryHeapSysBytes = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "jetkvm_audio_memory_heap_sys_bytes",
			Help: "Total heap system memory in bytes",
		},
	)

	memoryHeapObjects = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "jetkvm_audio_memory_heap_objects",
			Help: "Number of heap objects",
		},
	)

	memoryGCCount = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "jetkvm_audio_memory_gc_total",
			Help: "Total number of garbage collections",
		},
	)

	memoryGCCPUFraction = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "jetkvm_audio_memory_gc_cpu_fraction",
			Help: "Fraction of CPU time spent in garbage collection",
		},
	)

	// Buffer pool efficiency metrics
	bufferPoolHitRate = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "jetkvm_audio_buffer_pool_hit_rate_percent",
			Help: "Buffer pool hit rate percentage",
		},
		[]string{"pool_name"}, // pool_name: frame_pool, control_pool, zero_copy_pool
	)

	bufferPoolMissRate = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "jetkvm_audio_buffer_pool_miss_rate_percent",
			Help: "Buffer pool miss rate percentage",
		},
		[]string{"pool_name"}, // pool_name: frame_pool, control_pool, zero_copy_pool
	)

	bufferPoolUtilization = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "jetkvm_audio_buffer_pool_utilization_percent",
			Help: "Buffer pool utilization percentage",
		},
		[]string{"pool_name"}, // pool_name: frame_pool, control_pool, zero_copy_pool
	)

	bufferPoolThroughput = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "jetkvm_audio_buffer_pool_throughput_ops_per_sec",
			Help: "Buffer pool throughput in operations per second",
		},
		[]string{"pool_name"}, // pool_name: frame_pool, control_pool, zero_copy_pool
	)

	bufferPoolGetLatency = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "jetkvm_audio_buffer_pool_get_latency_seconds",
			Help: "Average buffer pool get operation latency in seconds",
		},
		[]string{"pool_name"}, // pool_name: frame_pool, control_pool, zero_copy_pool
	)

	bufferPoolPutLatency = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "jetkvm_audio_buffer_pool_put_latency_seconds",
			Help: "Average buffer pool put operation latency in seconds",
		},
		[]string{"pool_name"}, // pool_name: frame_pool, control_pool, zero_copy_pool
	)

	// Latency percentile metrics
	latencyPercentile = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "jetkvm_audio_latency_percentile_milliseconds",
			Help: "Audio latency percentiles in milliseconds",
		},
		[]string{"source", "percentile"}, // source: input, output, processing; percentile: p50, p95, p99, min, max, avg
	)

	// Metrics update tracking
	metricsUpdateMutex sync.RWMutex
	lastMetricsUpdate  int64

	// Counter value tracking (since prometheus counters don't have Get() method)
	audioFramesReceivedValue  uint64
	audioFramesDroppedValue   uint64
	audioBytesProcessedValue  uint64
	audioConnectionDropsValue uint64
	micFramesSentValue        uint64
	micFramesDroppedValue     uint64
	micBytesProcessedValue    uint64
	micConnectionDropsValue   uint64

	// Atomic counters for device health metrics - functionality removed, no longer used

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

// convertAudioMetricsToUnified converts AudioMetrics to UnifiedAudioMetrics
func convertAudioMetricsToUnified(metrics AudioMetrics) UnifiedAudioMetrics {
	return UnifiedAudioMetrics{
		FramesReceived:  metrics.FramesReceived,
		FramesDropped:   metrics.FramesDropped,
		FramesSent:      0, // AudioMetrics doesn't have FramesSent
		BytesProcessed:  metrics.BytesProcessed,
		ConnectionDrops: metrics.ConnectionDrops,
		LastFrameTime:   metrics.LastFrameTime,
		AverageLatency:  metrics.AverageLatency,
	}
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

// UpdateSocketBufferMetrics updates socket buffer metrics
func UpdateSocketBufferMetrics(component, bufferType string, size, utilization float64, overflowOccurred bool) {
	metricsUpdateMutex.Lock()
	defer metricsUpdateMutex.Unlock()

	socketBufferSizeGauge.WithLabelValues(component, bufferType).Set(size)
	socketBufferUtilizationGauge.WithLabelValues(component, bufferType).Set(utilization)

	if overflowOccurred {
		socketBufferOverflowCounter.WithLabelValues(component, bufferType).Inc()
	}

	atomic.StoreInt64(&lastMetricsUpdate, time.Now().Unix())
}

// UpdateDeviceHealthMetrics - Placeholder for future device health metrics

// UpdateMemoryMetrics updates memory metrics
func UpdateMemoryMetrics() {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	memoryHeapAllocBytes.Set(float64(m.HeapAlloc))
	memoryHeapSysBytes.Set(float64(m.HeapSys))
	memoryHeapObjects.Set(float64(m.HeapObjects))
	memoryGCCPUFraction.Set(m.GCCPUFraction)

	// Update GC count with delta calculation
	currentGCCount := uint32(m.NumGC)
	prevGCCount := atomic.SwapUint32(&memoryGCCountValue, currentGCCount)
	if prevGCCount > 0 && currentGCCount > prevGCCount {
		memoryGCCount.Add(float64(currentGCCount - prevGCCount))
	}

	atomic.StoreInt64(&lastMetricsUpdate, time.Now().Unix())
}

// UpdateBufferPoolMetrics updates buffer pool efficiency metrics
func UpdateBufferPoolMetrics(poolName string, hitRate, missRate, utilization, throughput, getLatency, putLatency float64) {
	metricsUpdateMutex.Lock()
	defer metricsUpdateMutex.Unlock()

	bufferPoolHitRate.WithLabelValues(poolName).Set(hitRate * 100)
	bufferPoolMissRate.WithLabelValues(poolName).Set(missRate * 100)
	bufferPoolUtilization.WithLabelValues(poolName).Set(utilization * 100)
	bufferPoolThroughput.WithLabelValues(poolName).Set(throughput)
	bufferPoolGetLatency.WithLabelValues(poolName).Set(getLatency)
	bufferPoolPutLatency.WithLabelValues(poolName).Set(putLatency)

	atomic.StoreInt64(&lastMetricsUpdate, time.Now().Unix())
}

// UpdateLatencyMetrics updates latency percentile metrics
func UpdateLatencyMetrics(source, percentile string, latencyMilliseconds float64) {
	metricsUpdateMutex.Lock()
	defer metricsUpdateMutex.Unlock()

	latencyPercentile.WithLabelValues(source, percentile).Set(latencyMilliseconds)

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
