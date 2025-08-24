package audio

import (
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
)

// UpdateAudioMetrics updates Prometheus metrics with current audio data
func UpdateAudioMetrics(metrics AudioMetrics) {
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
func UpdateMicrophoneMetrics(metrics AudioInputMetrics) {
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
			UpdateAudioMetrics(audioMetrics)

			// Update microphone input metrics
			micMetrics := GetAudioInputMetrics()
			UpdateMicrophoneMetrics(micMetrics)

			// Update microphone subprocess process metrics
			if inputSupervisor := GetAudioInputIPCSupervisor(); inputSupervisor != nil {
				if processMetrics := inputSupervisor.GetProcessMetrics(); processMetrics != nil {
					UpdateMicrophoneProcessMetrics(*processMetrics, inputSupervisor.IsRunning())
				}
			}

			// Update audio configuration metrics
			audioConfig := GetAudioConfig()
			UpdateAudioConfigMetrics(audioConfig)
			micConfig := GetMicrophoneConfig()
			UpdateMicrophoneConfigMetrics(micConfig)
		}
	}()
}
