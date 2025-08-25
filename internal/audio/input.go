package audio

import (
	"sync/atomic"
	"time"

	"github.com/jetkvm/kvm/internal/logging"
	"github.com/rs/zerolog"
)

// AudioInputMetrics holds metrics for microphone input
type AudioInputMetrics struct {
	FramesSent      int64
	FramesDropped   int64
	BytesProcessed  int64
	ConnectionDrops int64
	AverageLatency  time.Duration // time.Duration is int64
	LastFrameTime   time.Time
}

// AudioInputManager manages microphone input stream using IPC mode only
type AudioInputManager struct {
	metrics AudioInputMetrics

	ipcManager *AudioInputIPCManager
	logger     zerolog.Logger
	running    int32
}

// NewAudioInputManager creates a new audio input manager (IPC mode only)
func NewAudioInputManager() *AudioInputManager {
	return &AudioInputManager{
		ipcManager: NewAudioInputIPCManager(),
		logger:     logging.GetDefaultLogger().With().Str("component", "audio-input").Logger(),
	}
}

// Start begins processing microphone input
func (aim *AudioInputManager) Start() error {
	if !atomic.CompareAndSwapInt32(&aim.running, 0, 1) {
		return nil // Already running
	}

	aim.logger.Info().Msg("Starting audio input manager")

	// Start the IPC-based audio input
	err := aim.ipcManager.Start()
	if err != nil {
		aim.logger.Error().Err(err).Msg("Failed to start IPC audio input")
		atomic.StoreInt32(&aim.running, 0)
		return err
	}

	return nil
}

// Stop stops processing microphone input
func (aim *AudioInputManager) Stop() {
	if !atomic.CompareAndSwapInt32(&aim.running, 1, 0) {
		return // Already stopped
	}

	aim.logger.Info().Msg("Stopping audio input manager")

	// Stop the IPC-based audio input
	aim.ipcManager.Stop()

	aim.logger.Info().Msg("Audio input manager stopped")
}

// WriteOpusFrame writes an Opus frame to the audio input system with latency tracking
func (aim *AudioInputManager) WriteOpusFrame(frame []byte) error {
	if !aim.IsRunning() {
		return nil // Not running, silently drop
	}

	// Track end-to-end latency from WebRTC to IPC
	startTime := time.Now()
	err := aim.ipcManager.WriteOpusFrame(frame)
	processingTime := time.Since(startTime)

	// Log high latency warnings
	if processingTime > time.Duration(GetConfig().InputProcessingTimeoutMS)*time.Millisecond {
		aim.logger.Warn().
			Dur("latency_ms", processingTime).
			Msg("High audio processing latency detected")
	}

	if err != nil {
		atomic.AddInt64(&aim.metrics.FramesDropped, 1)
		return err
	}

	// Update metrics
	atomic.AddInt64(&aim.metrics.FramesSent, 1)
	atomic.AddInt64(&aim.metrics.BytesProcessed, int64(len(frame)))
	aim.metrics.LastFrameTime = time.Now()
	aim.metrics.AverageLatency = processingTime
	return nil
}

// WriteOpusFrameZeroCopy writes an Opus frame using zero-copy optimization
func (aim *AudioInputManager) WriteOpusFrameZeroCopy(frame *ZeroCopyAudioFrame) error {
	if !aim.IsRunning() {
		return nil // Not running, silently drop
	}

	if frame == nil {
		atomic.AddInt64(&aim.metrics.FramesDropped, 1)
		return nil
	}

	// Track end-to-end latency from WebRTC to IPC
	startTime := time.Now()
	err := aim.ipcManager.WriteOpusFrameZeroCopy(frame)
	processingTime := time.Since(startTime)

	// Log high latency warnings
	if processingTime > time.Duration(GetConfig().InputProcessingTimeoutMS)*time.Millisecond {
		aim.logger.Warn().
			Dur("latency_ms", processingTime).
			Msg("High audio processing latency detected")
	}

	if err != nil {
		atomic.AddInt64(&aim.metrics.FramesDropped, 1)
		return err
	}

	// Update metrics
	atomic.AddInt64(&aim.metrics.FramesSent, 1)
	atomic.AddInt64(&aim.metrics.BytesProcessed, int64(frame.Length()))
	aim.metrics.LastFrameTime = time.Now()
	aim.metrics.AverageLatency = processingTime
	return nil
}

// GetMetrics returns current audio input metrics
func (aim *AudioInputManager) GetMetrics() AudioInputMetrics {
	return AudioInputMetrics{
		FramesSent:     atomic.LoadInt64(&aim.metrics.FramesSent),
		FramesDropped:  atomic.LoadInt64(&aim.metrics.FramesDropped),
		BytesProcessed: atomic.LoadInt64(&aim.metrics.BytesProcessed),
		AverageLatency: aim.metrics.AverageLatency,
		LastFrameTime:  aim.metrics.LastFrameTime,
	}
}

// GetComprehensiveMetrics returns detailed performance metrics across all components
func (aim *AudioInputManager) GetComprehensiveMetrics() map[string]interface{} {
	// Get base metrics
	baseMetrics := aim.GetMetrics()

	// Get detailed IPC metrics
	ipcMetrics, detailedStats := aim.ipcManager.GetDetailedMetrics()

	comprehensiveMetrics := map[string]interface{}{
		"manager": map[string]interface{}{
			"frames_sent":        baseMetrics.FramesSent,
			"frames_dropped":     baseMetrics.FramesDropped,
			"bytes_processed":    baseMetrics.BytesProcessed,
			"average_latency_ms": float64(baseMetrics.AverageLatency.Nanoseconds()) / 1e6,
			"last_frame_time":    baseMetrics.LastFrameTime,
			"running":            aim.IsRunning(),
		},
		"ipc": map[string]interface{}{
			"frames_sent":        ipcMetrics.FramesSent,
			"frames_dropped":     ipcMetrics.FramesDropped,
			"bytes_processed":    ipcMetrics.BytesProcessed,
			"average_latency_ms": float64(ipcMetrics.AverageLatency.Nanoseconds()) / 1e6,
			"last_frame_time":    ipcMetrics.LastFrameTime,
		},
		"detailed": detailedStats,
	}

	return comprehensiveMetrics
}

// LogPerformanceStats logs current performance statistics
func (aim *AudioInputManager) LogPerformanceStats() {
	metrics := aim.GetComprehensiveMetrics()

	managerStats := metrics["manager"].(map[string]interface{})
	ipcStats := metrics["ipc"].(map[string]interface{})
	detailedStats := metrics["detailed"].(map[string]interface{})

	aim.logger.Info().
		Int64("manager_frames_sent", managerStats["frames_sent"].(int64)).
		Int64("manager_frames_dropped", managerStats["frames_dropped"].(int64)).
		Float64("manager_latency_ms", managerStats["average_latency_ms"].(float64)).
		Int64("ipc_frames_sent", ipcStats["frames_sent"].(int64)).
		Int64("ipc_frames_dropped", ipcStats["frames_dropped"].(int64)).
		Float64("ipc_latency_ms", ipcStats["average_latency_ms"].(float64)).
		Float64("client_drop_rate", detailedStats["client_drop_rate"].(float64)).
		Float64("frames_per_second", detailedStats["frames_per_second"].(float64)).
		Msg("Audio input performance metrics")
}

// IsRunning returns whether the audio input manager is running
func (aim *AudioInputManager) IsRunning() bool {
	return atomic.LoadInt32(&aim.running) == 1
}

// IsReady returns whether the audio input manager is ready to receive frames
// This checks both that it's running and that the IPC connection is established
func (aim *AudioInputManager) IsReady() bool {
	if !aim.IsRunning() {
		return false
	}
	return aim.ipcManager.IsReady()
}
