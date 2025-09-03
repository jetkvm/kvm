package audio

import (
	"fmt"
	"sync/atomic"
	"time"

	"github.com/jetkvm/kvm/internal/logging"
)

// AudioInputMetrics holds metrics for microphone input
// Atomic fields MUST be first for ARM32 alignment (int64 fields need 8-byte alignment)
type AudioInputMetrics struct {
	// Atomic int64 field first for proper ARM32 alignment
	FramesSent int64 `json:"frames_sent"` // Total frames sent (input-specific)

	// Embedded struct with atomic fields properly aligned
	BaseAudioMetrics
}

// AudioInputManager manages microphone input stream using IPC mode only
type AudioInputManager struct {
	*BaseAudioManager
	ipcManager *AudioInputIPCManager
	framesSent int64 // Input-specific metric
}

// NewAudioInputManager creates a new audio input manager
func NewAudioInputManager() *AudioInputManager {
	logger := logging.GetDefaultLogger().With().Str("component", AudioInputManagerComponent).Logger()
	return &AudioInputManager{
		BaseAudioManager: NewBaseAudioManager(logger),
		ipcManager:       NewAudioInputIPCManager(),
	}
}

// Start begins processing microphone input
func (aim *AudioInputManager) Start() error {
	if !aim.setRunning(true) {
		return fmt.Errorf("audio input manager is already running")
	}

	aim.logComponentStart(AudioInputManagerComponent)

	// Start the IPC-based audio input
	err := aim.ipcManager.Start()
	if err != nil {
		aim.logComponentError(AudioInputManagerComponent, err, "failed to start component")
		// Ensure proper cleanup on error
		aim.setRunning(false)
		// Reset metrics on failed start
		aim.resetMetrics()
		return err
	}

	aim.logComponentStarted(AudioInputManagerComponent)
	return nil
}

// Stop stops processing microphone input
func (aim *AudioInputManager) Stop() {
	if !aim.setRunning(false) {
		return // Already stopped
	}

	aim.logComponentStop(AudioInputManagerComponent)

	// Stop the IPC-based audio input
	aim.ipcManager.Stop()

	aim.logComponentStopped(AudioInputManagerComponent)
}

// resetMetrics resets all metrics to zero
func (aim *AudioInputManager) resetMetrics() {
	aim.BaseAudioManager.resetMetrics()
	atomic.StoreInt64(&aim.framesSent, 0)
}

// WriteOpusFrame writes an Opus frame to the audio input system with latency tracking
func (aim *AudioInputManager) WriteOpusFrame(frame []byte) error {
	if !aim.IsRunning() {
		return nil // Not running, silently drop
	}

	// Use ultra-fast validation for critical audio path
	if err := ValidateAudioFrame(frame); err != nil {
		aim.logComponentError(AudioInputManagerComponent, err, "Frame validation failed")
		return fmt.Errorf("input frame validation failed: %w", err)
	}

	// Track end-to-end latency from WebRTC to IPC
	startTime := time.Now()
	err := aim.ipcManager.WriteOpusFrame(frame)
	processingTime := time.Since(startTime)

	// Log high latency warnings
	if processingTime > time.Duration(GetConfig().InputProcessingTimeoutMS)*time.Millisecond {
		latencyMs := float64(processingTime.Milliseconds())
		aim.logger.Warn().
			Float64("latency_ms", latencyMs).
			Msg("High audio processing latency detected")

		// Record latency for goroutine cleanup optimization
		RecordAudioLatency(latencyMs)
	}

	if err != nil {
		atomic.AddInt64(&aim.metrics.FramesDropped, 1)
		return err
	}

	// Update metrics
	atomic.AddInt64(&aim.framesSent, 1)
	aim.recordFrameProcessed(len(frame))
	aim.updateLatency(processingTime)

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
		latencyMs := float64(processingTime.Milliseconds())
		aim.logger.Warn().
			Float64("latency_ms", latencyMs).
			Msg("High audio processing latency detected")

		// Record latency for goroutine cleanup optimization
		RecordAudioLatency(latencyMs)
	}

	if err != nil {
		atomic.AddInt64(&aim.metrics.FramesDropped, 1)
		return err
	}

	// Update metrics
	atomic.AddInt64(&aim.framesSent, 1)
	aim.recordFrameProcessed(frame.Length())
	aim.updateLatency(processingTime)

	return nil
}

// GetMetrics returns current metrics
func (aim *AudioInputManager) GetMetrics() AudioInputMetrics {
	return AudioInputMetrics{
		FramesSent:       atomic.LoadInt64(&aim.framesSent),
		BaseAudioMetrics: aim.getBaseMetrics(),
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
// This checks both the internal state and existing system processes
func (aim *AudioInputManager) IsRunning() bool {
	// First check internal state
	if aim.BaseAudioManager.IsRunning() {
		return true
	}

	// If internal state says not running, check for existing system processes
	// This prevents duplicate subprocess creation when a process already exists
	if aim.ipcManager != nil {
		supervisor := aim.ipcManager.GetSupervisor()
		if supervisor != nil {
			if existingPID, exists := supervisor.HasExistingProcess(); exists {
				aim.logger.Info().Int("existing_pid", existingPID).Msg("Found existing audio input server process")
				// Update internal state to reflect reality
				aim.setRunning(true)
				return true
			}
		}
	}

	return false
}

// IsReady returns whether the audio input manager is ready to receive frames
// This checks both that it's running and that the IPC connection is established
func (aim *AudioInputManager) IsReady() bool {
	if !aim.IsRunning() {
		return false
	}
	return aim.ipcManager.IsReady()
}
