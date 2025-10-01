package audio

import (
	"fmt"
	"sync/atomic"
	"time"

	"github.com/jetkvm/kvm/internal/logging"
)

// Component name constant for logging
const (
	AudioInputManagerComponent = "audio-input-manager"
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
	framesSent int64 // Input-specific metric
}

// NewAudioInputManager creates a new audio input manager
func NewAudioInputManager() *AudioInputManager {
	logger := logging.GetDefaultLogger().With().Str("component", AudioInputManagerComponent).Logger()
	return &AudioInputManager{
		BaseAudioManager: NewBaseAudioManager(logger),
	}
}

// getClient returns the audio input client from the global supervisor
func (aim *AudioInputManager) getClient() *AudioInputClient {
	supervisor := GetAudioInputSupervisor()
	if supervisor == nil {
		return nil
	}
	return supervisor.GetClient()
}

// Start begins processing microphone input
func (aim *AudioInputManager) Start() error {
	if !aim.setRunning(true) {
		return fmt.Errorf("audio input manager is already running")
	}

	aim.logComponentStart(AudioInputManagerComponent)

	// Ensure supervisor and client are available
	supervisor := GetAudioInputSupervisor()
	if supervisor == nil {
		aim.setRunning(false)
		return fmt.Errorf("audio input supervisor not available")
	}

	// Start the supervisor if not already running
	if !supervisor.IsRunning() {
		err := supervisor.Start()
		if err != nil {
			aim.logComponentError(AudioInputManagerComponent, err, "failed to start supervisor")
			aim.setRunning(false)
			aim.resetMetrics()
			return err
		}
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

	// Note: We don't stop the supervisor here as it may be shared
	// The supervisor lifecycle is managed by the main process

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

	// Check mute state - drop frames if microphone is muted (like audio output)
	if IsMicrophoneMuted() {
		return nil // Muted, silently drop
	}

	// Use ultra-fast validation for critical audio path
	if err := ValidateAudioFrame(frame); err != nil {
		aim.logComponentError(AudioInputManagerComponent, err, "Frame validation failed")
		return fmt.Errorf("input frame validation failed: %w", err)
	}

	// Get client from supervisor
	client := aim.getClient()
	if client == nil {
		return fmt.Errorf("audio input client not available")
	}

	// Track end-to-end latency from WebRTC to IPC
	startTime := time.Now()
	err := client.SendFrame(frame)
	processingTime := time.Since(startTime)

	// Log high latency warnings
	if processingTime > time.Duration(Config.InputProcessingTimeoutMS)*time.Millisecond {
		latencyMs := float64(processingTime.Milliseconds())
		aim.logger.Warn().
			Float64("latency_ms", latencyMs).
			Msg("High audio processing latency detected")
	}

	if err != nil {
		return err
	}

	return nil
}

// WriteOpusFrameZeroCopy writes an Opus frame using zero-copy optimization
func (aim *AudioInputManager) WriteOpusFrameZeroCopy(frame *ZeroCopyAudioFrame) error {
	if !aim.IsRunning() {
		return nil // Not running, silently drop
	}

	// Check mute state - drop frames if microphone is muted (like audio output)
	if IsMicrophoneMuted() {
		return nil // Muted, silently drop
	}

	if frame == nil {
		atomic.AddInt64(&aim.metrics.FramesDropped, 1)
		return nil
	}

	// Get client from supervisor
	client := aim.getClient()
	if client == nil {
		atomic.AddInt64(&aim.metrics.FramesDropped, 1)
		return fmt.Errorf("audio input client not available")
	}

	// Track end-to-end latency from WebRTC to IPC
	startTime := time.Now()
	err := client.SendFrameZeroCopy(frame)
	processingTime := time.Since(startTime)

	// Log high latency warnings
	if processingTime > time.Duration(Config.InputProcessingTimeoutMS)*time.Millisecond {
		latencyMs := float64(processingTime.Milliseconds())
		aim.logger.Warn().
			Float64("latency_ms", latencyMs).
			Msg("High audio processing latency detected")
	}

	if err != nil {
		atomic.AddInt64(&aim.metrics.FramesDropped, 1)
		return err
	}

	// Update metrics
	atomic.AddInt64(&aim.framesSent, 1)

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

	// Get client stats if available
	var clientStats map[string]interface{}
	client := aim.getClient()
	if client != nil {
		total, dropped := client.GetFrameStats()
		clientStats = map[string]interface{}{
			"frames_sent":    total,
			"frames_dropped": dropped,
		}
	} else {
		clientStats = map[string]interface{}{
			"frames_sent":    0,
			"frames_dropped": 0,
		}
	}

	comprehensiveMetrics := map[string]interface{}{
		"manager": map[string]interface{}{
			"frames_sent":        baseMetrics.FramesSent,
			"frames_dropped":     baseMetrics.FramesDropped,
			"bytes_processed":    baseMetrics.BytesProcessed,
			"average_latency_ms": float64(baseMetrics.AverageLatency.Nanoseconds()) / 1e6,
			"last_frame_time":    baseMetrics.LastFrameTime,
			"running":            aim.IsRunning(),
		},
		"client": clientStats,
	}

	return comprehensiveMetrics
}

// IsRunning returns whether the audio input manager is running
// This checks both the internal state and existing system processes
func (aim *AudioInputManager) IsRunning() bool {
	// First check internal state
	if aim.BaseAudioManager.IsRunning() {
		return true
	}

	// If internal state says not running, check supervisor
	supervisor := GetAudioInputSupervisor()
	if supervisor != nil {
		if existingPID, exists := supervisor.HasExistingProcess(); exists {
			aim.logger.Info().Int("existing_pid", existingPID).Msg("Found existing audio input server process")
			// Update internal state to reflect reality
			aim.setRunning(true)
			return true
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

	// Check if client is connected
	client := aim.getClient()
	if client == nil {
		return false
	}

	return client.IsConnected()
}
