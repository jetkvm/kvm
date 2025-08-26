package audio

import (
	"sync/atomic"
	"time"

	"github.com/jetkvm/kvm/internal/logging"
	"github.com/rs/zerolog"
)

// AudioInputIPCManager manages microphone input using IPC when enabled
type AudioInputIPCManager struct {
	metrics AudioInputMetrics

	supervisor *AudioInputSupervisor
	logger     zerolog.Logger
	running    int32
}

// NewAudioInputIPCManager creates a new IPC-based audio input manager
func NewAudioInputIPCManager() *AudioInputIPCManager {
	return &AudioInputIPCManager{
		supervisor: NewAudioInputSupervisor(),
		logger:     logging.GetDefaultLogger().With().Str("component", "audio-input-ipc").Logger(),
	}
}

// Start starts the IPC-based audio input system
func (aim *AudioInputIPCManager) Start() error {
	if !atomic.CompareAndSwapInt32(&aim.running, 0, 1) {
		return nil
	}

	aim.logger.Info().Msg("Starting IPC-based audio input system")

	err := aim.supervisor.Start()
	if err != nil {
		// Ensure proper cleanup on supervisor start failure
		atomic.StoreInt32(&aim.running, 0)
		// Reset metrics on failed start
		atomic.StoreInt64(&aim.metrics.FramesSent, 0)
		atomic.StoreInt64(&aim.metrics.FramesDropped, 0)
		atomic.StoreInt64(&aim.metrics.BytesProcessed, 0)
		atomic.StoreInt64(&aim.metrics.ConnectionDrops, 0)
		aim.logger.Error().Err(err).Msg("Failed to start audio input supervisor")
		return err
	}

	config := InputIPCConfig{
		SampleRate: GetConfig().InputIPCSampleRate,
		Channels:   GetConfig().InputIPCChannels,
		FrameSize:  GetConfig().InputIPCFrameSize,
	}

	// Wait for subprocess readiness
	time.Sleep(GetConfig().LongSleepDuration)

	err = aim.supervisor.SendConfig(config)
	if err != nil {
		// Config send failure is not critical, log warning and continue
		aim.logger.Warn().Err(err).Msg("Failed to send initial config, will retry later")
	}

	aim.logger.Info().Msg("IPC-based audio input system started")
	return nil
}

// Stop stops the IPC-based audio input system
func (aim *AudioInputIPCManager) Stop() {
	if !atomic.CompareAndSwapInt32(&aim.running, 1, 0) {
		return
	}

	aim.logger.Info().Msg("Stopping IPC-based audio input system")
	aim.supervisor.Stop()
	aim.logger.Info().Msg("IPC-based audio input system stopped")
}

// WriteOpusFrame sends an Opus frame to the audio input server via IPC
func (aim *AudioInputIPCManager) WriteOpusFrame(frame []byte) error {
	if atomic.LoadInt32(&aim.running) == 0 {
		return nil // Not running, silently ignore
	}

	if len(frame) == 0 {
		return nil // Empty frame, ignore
	}

	// Start latency measurement
	startTime := time.Now()

	// Update metrics
	atomic.AddInt64(&aim.metrics.FramesSent, 1)
	atomic.AddInt64(&aim.metrics.BytesProcessed, int64(len(frame)))
	aim.metrics.LastFrameTime = startTime

	// Send frame via IPC
	err := aim.supervisor.SendFrame(frame)
	if err != nil {
		// Count as dropped frame
		atomic.AddInt64(&aim.metrics.FramesDropped, 1)
		aim.logger.Debug().Err(err).Msg("Failed to send frame via IPC")
		return err
	}

	// Calculate and update latency (end-to-end IPC transmission time)
	latency := time.Since(startTime)
	aim.updateLatencyMetrics(latency)

	return nil
}

// WriteOpusFrameZeroCopy sends an Opus frame via IPC using zero-copy optimization
func (aim *AudioInputIPCManager) WriteOpusFrameZeroCopy(frame *ZeroCopyAudioFrame) error {
	if atomic.LoadInt32(&aim.running) == 0 {
		return nil // Not running, silently ignore
	}

	if frame == nil || frame.Length() == 0 {
		return nil // Empty frame, ignore
	}

	// Start latency measurement
	startTime := time.Now()

	// Update metrics
	atomic.AddInt64(&aim.metrics.FramesSent, 1)
	atomic.AddInt64(&aim.metrics.BytesProcessed, int64(frame.Length()))
	aim.metrics.LastFrameTime = startTime

	// Send frame via IPC using zero-copy data
	err := aim.supervisor.SendFrameZeroCopy(frame)
	if err != nil {
		// Count as dropped frame
		atomic.AddInt64(&aim.metrics.FramesDropped, 1)
		aim.logger.Debug().Err(err).Msg("Failed to send zero-copy frame via IPC")
		return err
	}

	// Calculate and update latency (end-to-end IPC transmission time)
	latency := time.Since(startTime)
	aim.updateLatencyMetrics(latency)

	return nil
}

// IsRunning returns whether the IPC manager is running
func (aim *AudioInputIPCManager) IsRunning() bool {
	return atomic.LoadInt32(&aim.running) == 1
}

// IsReady returns whether the IPC manager is ready to receive frames
// This checks that the supervisor is connected to the audio input server
func (aim *AudioInputIPCManager) IsReady() bool {
	if !aim.IsRunning() {
		return false
	}
	return aim.supervisor.IsConnected()
}

// GetMetrics returns current metrics
func (aim *AudioInputIPCManager) GetMetrics() AudioInputMetrics {
	return AudioInputMetrics{
		FramesSent:      atomic.LoadInt64(&aim.metrics.FramesSent),
		FramesDropped:   atomic.LoadInt64(&aim.metrics.FramesDropped),
		BytesProcessed:  atomic.LoadInt64(&aim.metrics.BytesProcessed),
		ConnectionDrops: atomic.LoadInt64(&aim.metrics.ConnectionDrops),
		AverageLatency:  aim.metrics.AverageLatency,
		LastFrameTime:   aim.metrics.LastFrameTime,
	}
}

// updateLatencyMetrics updates the latency metrics with exponential moving average
func (aim *AudioInputIPCManager) updateLatencyMetrics(latency time.Duration) {
	// Use exponential moving average for smooth latency calculation
	currentAvg := aim.metrics.AverageLatency
	if currentAvg == 0 {
		aim.metrics.AverageLatency = latency
	} else {
		// EMA with alpha = 0.1 for smooth averaging
		aim.metrics.AverageLatency = time.Duration(float64(currentAvg)*0.9 + float64(latency)*0.1)
	}
}

// GetDetailedMetrics returns comprehensive performance metrics
func (aim *AudioInputIPCManager) GetDetailedMetrics() (AudioInputMetrics, map[string]interface{}) {
	metrics := aim.GetMetrics()

	// Get client frame statistics
	client := aim.supervisor.GetClient()
	totalFrames, droppedFrames := int64(0), int64(0)
	dropRate := 0.0
	if client != nil {
		totalFrames, droppedFrames = client.GetFrameStats()
		dropRate = client.GetDropRate()
	}

	// Get server statistics if available
	serverStats := make(map[string]interface{})
	if aim.supervisor.IsRunning() {
		serverStats["status"] = "running"
	} else {
		serverStats["status"] = "stopped"
	}

	detailedStats := map[string]interface{}{
		"client_total_frames":   totalFrames,
		"client_dropped_frames": droppedFrames,
		"client_drop_rate":      dropRate,
		"server_stats":          serverStats,
		"ipc_latency_ms":        float64(metrics.AverageLatency.Nanoseconds()) / 1e6,
		"frames_per_second":     aim.calculateFrameRate(),
	}

	return metrics, detailedStats
}

// calculateFrameRate calculates the current frame rate
func (aim *AudioInputIPCManager) calculateFrameRate() float64 {
	framesSent := atomic.LoadInt64(&aim.metrics.FramesSent)
	if framesSent == 0 {
		return 0.0
	}

	// Return typical Opus frame rate
	return 50.0
}

// GetSupervisor returns the supervisor for advanced operations
func (aim *AudioInputIPCManager) GetSupervisor() *AudioInputSupervisor {
	return aim.supervisor
}
