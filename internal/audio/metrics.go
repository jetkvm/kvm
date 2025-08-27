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
	// Adaptive buffer metrics
	adaptiveInputBufferSize = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "jetkvm_adaptive_input_buffer_size_bytes",
			Help: "Current adaptive input buffer size in bytes",
		},
	)

	adaptiveOutputBufferSize = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "jetkvm_adaptive_output_buffer_size_bytes",
			Help: "Current adaptive output buffer size in bytes",
		},
	)

	adaptiveBufferAdjustmentsTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "jetkvm_adaptive_buffer_adjustments_total",
			Help: "Total number of adaptive buffer size adjustments",
		},
	)

	adaptiveSystemCpuPercent = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "jetkvm_adaptive_system_cpu_percent",
			Help: "System CPU usage percentage used by adaptive buffer manager",
		},
	)

	adaptiveSystemMemoryPercent = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "jetkvm_adaptive_system_memory_percent",
			Help: "System memory usage percentage used by adaptive buffer manager",
		},
	)

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

	audioAverageLatencySeconds = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "jetkvm_audio_average_latency_seconds",
			Help: "Average audio latency in seconds",
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

	microphoneAverageLatencySeconds = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "jetkvm_microphone_average_latency_seconds",
			Help: "Average microphone latency in seconds",
		},
	)

	microphoneLastFrameTimestamp = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "jetkvm_microphone_last_frame_timestamp_seconds",
			Help: "Timestamp of the last microphone frame sent",
		},
	)

	// Audio subprocess process metrics
	audioProcessCpuPercent = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "jetkvm_audio_process_cpu_percent",
			Help: "CPU usage percentage of audio output subprocess",
		},
	)

	audioProcessMemoryPercent = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "jetkvm_audio_process_memory_percent",
			Help: "Memory usage percentage of audio output subprocess",
		},
	)

	audioProcessMemoryRssBytes = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "jetkvm_audio_process_memory_rss_bytes",
			Help: "RSS memory usage in bytes of audio output subprocess",
		},
	)

	audioProcessMemoryVmsBytes = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "jetkvm_audio_process_memory_vms_bytes",
			Help: "VMS memory usage in bytes of audio output subprocess",
		},
	)

	audioProcessRunning = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "jetkvm_audio_process_running",
			Help: "Whether audio output subprocess is running (1=running, 0=stopped)",
		},
	)

	// Microphone subprocess process metrics
	microphoneProcessCpuPercent = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "jetkvm_microphone_process_cpu_percent",
			Help: "CPU usage percentage of microphone input subprocess",
		},
	)

	microphoneProcessMemoryPercent = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "jetkvm_microphone_process_memory_percent",
			Help: "Memory usage percentage of microphone input subprocess",
		},
	)

	microphoneProcessMemoryRssBytes = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "jetkvm_microphone_process_memory_rss_bytes",
			Help: "RSS memory usage in bytes of microphone input subprocess",
		},
	)

	microphoneProcessMemoryVmsBytes = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "jetkvm_microphone_process_memory_vms_bytes",
			Help: "VMS memory usage in bytes of microphone input subprocess",
		},
	)

	microphoneProcessRunning = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "jetkvm_microphone_process_running",
			Help: "Whether microphone input subprocess is running (1=running, 0=stopped)",
		},
	)

	// Audio configuration metrics
	audioConfigQuality = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "jetkvm_audio_config_quality",
			Help: "Current audio quality setting (0=Low, 1=Medium, 2=High, 3=Ultra)",
		},
	)

	audioConfigBitrate = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "jetkvm_audio_config_bitrate_kbps",
			Help: "Current audio bitrate in kbps",
		},
	)

	audioConfigSampleRate = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "jetkvm_audio_config_sample_rate_hz",
			Help: "Current audio sample rate in Hz",
		},
	)

	audioConfigChannels = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "jetkvm_audio_config_channels",
			Help: "Current audio channel count",
		},
	)

	microphoneConfigQuality = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "jetkvm_microphone_config_quality",
			Help: "Current microphone quality setting (0=Low, 1=Medium, 2=High, 3=Ultra)",
		},
	)

	microphoneConfigBitrate = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "jetkvm_microphone_config_bitrate_kbps",
			Help: "Current microphone bitrate in kbps",
		},
	)

	microphoneConfigSampleRate = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "jetkvm_microphone_config_sample_rate_hz",
			Help: "Current microphone sample rate in Hz",
		},
	)

	microphoneConfigChannels = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "jetkvm_microphone_config_channels",
			Help: "Current microphone channel count",
		},
	)

	// Device health metrics
	deviceHealthStatus = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "jetkvm_audio_device_health_status",
			Help: "Current device health status (0=Healthy, 1=Degraded, 2=Failing, 3=Critical)",
		},
		[]string{"device_type"}, // device_type: capture, playback
	)

	deviceHealthScore = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "jetkvm_audio_device_health_score",
			Help: "Device health score (0.0-1.0, higher is better)",
		},
		[]string{"device_type"}, // device_type: capture, playback
	)

	deviceConsecutiveErrors = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "jetkvm_audio_device_consecutive_errors",
			Help: "Number of consecutive errors for device",
		},
		[]string{"device_type"}, // device_type: capture, playback
	)

	deviceTotalErrors = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "jetkvm_audio_device_total_errors",
			Help: "Total number of errors for device",
		},
		[]string{"device_type"}, // device_type: capture, playback
	)

	deviceLatencySpikes = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "jetkvm_audio_device_latency_spikes_total",
			Help: "Total number of latency spikes for device",
		},
		[]string{"device_type"}, // device_type: capture, playback
	)

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
			Name: "jetkvm_audio_latency_percentile_seconds",
			Help: "Audio latency percentiles in seconds",
		},
		[]string{"source", "percentile"}, // source: input, output, processing; percentile: p50, p95, p99, min, max, avg
	)

	// Metrics update tracking
	metricsUpdateMutex sync.RWMutex
	lastMetricsUpdate  int64

	// Counter value tracking (since prometheus counters don't have Get() method)
	audioFramesReceivedValue  int64
	audioFramesDroppedValue   int64
	audioBytesProcessedValue  int64
	audioConnectionDropsValue int64
	micFramesSentValue        int64
	micFramesDroppedValue     int64
	micBytesProcessedValue    int64
	micConnectionDropsValue   int64

	// Atomic counters for device health metrics
	deviceCaptureErrorsValue  int64
	devicePlaybackErrorsValue int64
	deviceCaptureSpikesValue  int64
	devicePlaybackSpikesValue int64

	// Atomic counter for memory GC
	memoryGCCountValue uint32
)

// UnifiedAudioMetrics provides a common structure for both input and output audio streams
type UnifiedAudioMetrics struct {
	FramesReceived  int64         `json:"frames_received"`
	FramesDropped   int64         `json:"frames_dropped"`
	FramesSent      int64         `json:"frames_sent,omitempty"`
	BytesProcessed  int64         `json:"bytes_processed"`
	ConnectionDrops int64         `json:"connection_drops"`
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
		FramesDropped:   metrics.FramesDropped,
		FramesSent:      metrics.FramesSent,
		BytesProcessed:  metrics.BytesProcessed,
		ConnectionDrops: metrics.ConnectionDrops,
		LastFrameTime:   metrics.LastFrameTime,
		AverageLatency:  metrics.AverageLatency,
	}
}

// UpdateAudioMetrics updates Prometheus metrics with current audio data
func UpdateAudioMetrics(metrics UnifiedAudioMetrics) {
	oldReceived := atomic.SwapInt64(&audioFramesReceivedValue, metrics.FramesReceived)
	if metrics.FramesReceived > oldReceived {
		audioFramesReceivedTotal.Add(float64(metrics.FramesReceived - oldReceived))
	}

	oldDropped := atomic.SwapInt64(&audioFramesDroppedValue, metrics.FramesDropped)
	if metrics.FramesDropped > oldDropped {
		audioFramesDroppedTotal.Add(float64(metrics.FramesDropped - oldDropped))
	}

	oldBytes := atomic.SwapInt64(&audioBytesProcessedValue, metrics.BytesProcessed)
	if metrics.BytesProcessed > oldBytes {
		audioBytesProcessedTotal.Add(float64(metrics.BytesProcessed - oldBytes))
	}

	oldDrops := atomic.SwapInt64(&audioConnectionDropsValue, metrics.ConnectionDrops)
	if metrics.ConnectionDrops > oldDrops {
		audioConnectionDropsTotal.Add(float64(metrics.ConnectionDrops - oldDrops))
	}

	// Update gauges
	audioAverageLatencySeconds.Set(float64(metrics.AverageLatency.Nanoseconds()) / 1e9)
	if !metrics.LastFrameTime.IsZero() {
		audioLastFrameTimestamp.Set(float64(metrics.LastFrameTime.Unix()))
	}

	atomic.StoreInt64(&lastMetricsUpdate, time.Now().Unix())
}

// UpdateMicrophoneMetrics updates Prometheus metrics with current microphone data
func UpdateMicrophoneMetrics(metrics UnifiedAudioMetrics) {
	oldSent := atomic.SwapInt64(&micFramesSentValue, metrics.FramesSent)
	if metrics.FramesSent > oldSent {
		microphoneFramesSentTotal.Add(float64(metrics.FramesSent - oldSent))
	}

	oldDropped := atomic.SwapInt64(&micFramesDroppedValue, metrics.FramesDropped)
	if metrics.FramesDropped > oldDropped {
		microphoneFramesDroppedTotal.Add(float64(metrics.FramesDropped - oldDropped))
	}

	oldBytes := atomic.SwapInt64(&micBytesProcessedValue, metrics.BytesProcessed)
	if metrics.BytesProcessed > oldBytes {
		microphoneBytesProcessedTotal.Add(float64(metrics.BytesProcessed - oldBytes))
	}

	oldDrops := atomic.SwapInt64(&micConnectionDropsValue, metrics.ConnectionDrops)
	if metrics.ConnectionDrops > oldDrops {
		microphoneConnectionDropsTotal.Add(float64(metrics.ConnectionDrops - oldDrops))
	}

	// Update gauges
	microphoneAverageLatencySeconds.Set(float64(metrics.AverageLatency.Nanoseconds()) / 1e9)
	if !metrics.LastFrameTime.IsZero() {
		microphoneLastFrameTimestamp.Set(float64(metrics.LastFrameTime.Unix()))
	}

	atomic.StoreInt64(&lastMetricsUpdate, time.Now().Unix())
}

// UpdateAudioProcessMetrics updates Prometheus metrics with audio subprocess data
func UpdateAudioProcessMetrics(metrics ProcessMetrics, isRunning bool) {
	metricsUpdateMutex.Lock()
	defer metricsUpdateMutex.Unlock()

	audioProcessCpuPercent.Set(metrics.CPUPercent)
	audioProcessMemoryPercent.Set(metrics.MemoryPercent)
	audioProcessMemoryRssBytes.Set(float64(metrics.MemoryRSS))
	audioProcessMemoryVmsBytes.Set(float64(metrics.MemoryVMS))
	if isRunning {
		audioProcessRunning.Set(1)
	} else {
		audioProcessRunning.Set(0)
	}

	atomic.StoreInt64(&lastMetricsUpdate, time.Now().Unix())
}

// UpdateMicrophoneProcessMetrics updates Prometheus metrics with microphone subprocess data
func UpdateMicrophoneProcessMetrics(metrics ProcessMetrics, isRunning bool) {
	metricsUpdateMutex.Lock()
	defer metricsUpdateMutex.Unlock()

	microphoneProcessCpuPercent.Set(metrics.CPUPercent)
	microphoneProcessMemoryPercent.Set(metrics.MemoryPercent)
	microphoneProcessMemoryRssBytes.Set(float64(metrics.MemoryRSS))
	microphoneProcessMemoryVmsBytes.Set(float64(metrics.MemoryVMS))
	if isRunning {
		microphoneProcessRunning.Set(1)
	} else {
		microphoneProcessRunning.Set(0)
	}

	atomic.StoreInt64(&lastMetricsUpdate, time.Now().Unix())
}

// UpdateAudioConfigMetrics updates Prometheus metrics with audio configuration
func UpdateAudioConfigMetrics(config AudioConfig) {
	metricsUpdateMutex.Lock()
	defer metricsUpdateMutex.Unlock()

	audioConfigQuality.Set(float64(config.Quality))
	audioConfigBitrate.Set(float64(config.Bitrate))
	audioConfigSampleRate.Set(float64(config.SampleRate))
	audioConfigChannels.Set(float64(config.Channels))

	atomic.StoreInt64(&lastMetricsUpdate, time.Now().Unix())
}

// UpdateMicrophoneConfigMetrics updates Prometheus metrics with microphone configuration
func UpdateMicrophoneConfigMetrics(config AudioConfig) {
	metricsUpdateMutex.Lock()
	defer metricsUpdateMutex.Unlock()

	microphoneConfigQuality.Set(float64(config.Quality))
	microphoneConfigBitrate.Set(float64(config.Bitrate))
	microphoneConfigSampleRate.Set(float64(config.SampleRate))
	microphoneConfigChannels.Set(float64(config.Channels))

	atomic.StoreInt64(&lastMetricsUpdate, time.Now().Unix())
}

// UpdateAdaptiveBufferMetrics updates Prometheus metrics with adaptive buffer information
func UpdateAdaptiveBufferMetrics(inputBufferSize, outputBufferSize int, cpuPercent, memoryPercent float64, adjustmentMade bool) {
	metricsUpdateMutex.Lock()
	defer metricsUpdateMutex.Unlock()

	adaptiveInputBufferSize.Set(float64(inputBufferSize))
	adaptiveOutputBufferSize.Set(float64(outputBufferSize))
	adaptiveSystemCpuPercent.Set(cpuPercent)
	adaptiveSystemMemoryPercent.Set(memoryPercent)

	if adjustmentMade {
		adaptiveBufferAdjustmentsTotal.Inc()
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

// UpdateDeviceHealthMetrics updates device health metrics
func UpdateDeviceHealthMetrics(deviceType string, status int, healthScore float64, consecutiveErrors, totalErrors, latencySpikes int64) {
	metricsUpdateMutex.Lock()
	defer metricsUpdateMutex.Unlock()

	deviceHealthStatus.WithLabelValues(deviceType).Set(float64(status))
	deviceHealthScore.WithLabelValues(deviceType).Set(healthScore)
	deviceConsecutiveErrors.WithLabelValues(deviceType).Set(float64(consecutiveErrors))

	// Update error counters with delta calculation
	var prevErrors, prevSpikes int64
	if deviceType == "capture" {
		prevErrors = atomic.SwapInt64(&deviceCaptureErrorsValue, totalErrors)
		prevSpikes = atomic.SwapInt64(&deviceCaptureSpikesValue, latencySpikes)
	} else {
		prevErrors = atomic.SwapInt64(&devicePlaybackErrorsValue, totalErrors)
		prevSpikes = atomic.SwapInt64(&devicePlaybackSpikesValue, latencySpikes)
	}

	if prevErrors > 0 && totalErrors > prevErrors {
		deviceTotalErrors.WithLabelValues(deviceType).Add(float64(totalErrors - prevErrors))
	}
	if prevSpikes > 0 && latencySpikes > prevSpikes {
		deviceLatencySpikes.WithLabelValues(deviceType).Add(float64(latencySpikes - prevSpikes))
	}

	atomic.StoreInt64(&lastMetricsUpdate, time.Now().Unix())
}

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
func UpdateLatencyMetrics(source, percentile string, latencySeconds float64) {
	metricsUpdateMutex.Lock()
	defer metricsUpdateMutex.Unlock()

	latencyPercentile.WithLabelValues(source, percentile).Set(latencySeconds)

	atomic.StoreInt64(&lastMetricsUpdate, time.Now().Unix())
}

// GetLastMetricsUpdate returns the timestamp of the last metrics update
func GetLastMetricsUpdate() time.Time {
	timestamp := atomic.LoadInt64(&lastMetricsUpdate)
	return time.Unix(timestamp, 0)
}

// StartMetricsUpdater starts a goroutine that periodically updates Prometheus metrics
func StartMetricsUpdater() {
	go func() {
		ticker := time.NewTicker(5 * time.Second) // Update every 5 seconds
		defer ticker.Stop()

		for range ticker.C {
			// Update audio output metrics
			audioMetrics := GetAudioMetrics()
			UpdateAudioMetrics(convertAudioMetricsToUnified(audioMetrics))

			// Update microphone input metrics
			micMetrics := GetAudioInputMetrics()
			UpdateMicrophoneMetrics(convertAudioInputMetricsToUnified(micMetrics))

			// Update audio configuration metrics
			audioConfig := GetAudioConfig()
			UpdateAudioConfigMetrics(audioConfig)
			micConfig := GetMicrophoneConfig()
			UpdateMicrophoneConfigMetrics(micConfig)

			// Update memory metrics
			UpdateMemoryMetrics()
		}
	}()
}
