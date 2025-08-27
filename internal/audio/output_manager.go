package audio

import (
	"sync/atomic"

	"github.com/jetkvm/kvm/internal/logging"
)

// AudioOutputManager manages audio output stream using IPC mode
type AudioOutputManager struct {
	*BaseAudioManager
	streamer       *AudioOutputStreamer
	framesReceived int64 // Output-specific metric
}

// AudioOutputMetrics tracks output-specific metrics
// Atomic fields MUST be first for ARM32 alignment (int64 fields need 8-byte alignment)
type AudioOutputMetrics struct {
	// Atomic int64 field first for proper ARM32 alignment
	FramesReceived int64 `json:"frames_received"` // Total frames received (output-specific)

	// Embedded struct with atomic fields properly aligned
	BaseAudioMetrics
}

// NewAudioOutputManager creates a new audio output manager
func NewAudioOutputManager() *AudioOutputManager {
	logger := logging.GetDefaultLogger().With().Str("component", AudioOutputManagerComponent).Logger()
	streamer, err := NewAudioOutputStreamer()
	if err != nil {
		// Log error but continue with nil streamer - will be handled gracefully
		logger.Error().Err(err).Msg("Failed to create audio output streamer")
	}

	return &AudioOutputManager{
		BaseAudioManager: NewBaseAudioManager(logger),
		streamer:         streamer,
	}
}

// Start starts the audio output manager
func (aom *AudioOutputManager) Start() error {
	if !aom.setRunning(true) {
		return nil // Already running
	}

	aom.logComponentStart(AudioOutputManagerComponent)

	if aom.streamer == nil {
		// Try to recreate streamer if it was nil
		streamer, err := NewAudioOutputStreamer()
		if err != nil {
			aom.setRunning(false)
			aom.logComponentError(AudioOutputManagerComponent, err, "failed to create audio output streamer")
			return err
		}
		aom.streamer = streamer
	}

	err := aom.streamer.Start()
	if err != nil {
		aom.setRunning(false)
		// Reset metrics on failed start
		aom.resetMetrics()
		aom.logComponentError(AudioOutputManagerComponent, err, "failed to start component")
		return err
	}

	aom.logComponentStarted(AudioOutputManagerComponent)
	return nil
}

// Stop stops the audio output manager
func (aom *AudioOutputManager) Stop() {
	if !aom.setRunning(false) {
		return // Already stopped
	}

	aom.logComponentStop(AudioOutputManagerComponent)

	if aom.streamer != nil {
		aom.streamer.Stop()
	}

	aom.logComponentStopped(AudioOutputManagerComponent)
}

// resetMetrics resets all metrics to zero
func (aom *AudioOutputManager) resetMetrics() {
	aom.BaseAudioManager.resetMetrics()
	atomic.StoreInt64(&aom.framesReceived, 0)
}

// Note: IsRunning() is inherited from BaseAudioManager

// IsReady returns whether the audio output manager is ready to receive frames
func (aom *AudioOutputManager) IsReady() bool {
	if !aom.IsRunning() || aom.streamer == nil {
		return false
	}
	// For output, we consider it ready if the streamer is running
	// This could be enhanced with connection status checks
	return true
}

// GetMetrics returns current metrics
func (aom *AudioOutputManager) GetMetrics() AudioOutputMetrics {
	return AudioOutputMetrics{
		FramesReceived:   atomic.LoadInt64(&aom.framesReceived),
		BaseAudioMetrics: aom.getBaseMetrics(),
	}
}

// GetComprehensiveMetrics returns detailed performance metrics
func (aom *AudioOutputManager) GetComprehensiveMetrics() map[string]interface{} {
	baseMetrics := aom.GetMetrics()

	comprehensiveMetrics := map[string]interface{}{
		"manager": map[string]interface{}{
			"frames_received":    baseMetrics.FramesReceived,
			"frames_processed":   baseMetrics.FramesProcessed,
			"frames_dropped":     baseMetrics.FramesDropped,
			"bytes_processed":    baseMetrics.BytesProcessed,
			"connection_drops":   baseMetrics.ConnectionDrops,
			"average_latency_ms": float64(baseMetrics.AverageLatency.Nanoseconds()) / 1e6,
			"last_frame_time":    baseMetrics.LastFrameTime,
			"running":            aom.IsRunning(),
			"ready":              aom.IsReady(),
		},
	}

	if aom.streamer != nil {
		processed, dropped, avgTime := aom.streamer.GetStats()
		comprehensiveMetrics["streamer"] = map[string]interface{}{
			"frames_processed":       processed,
			"frames_dropped":         dropped,
			"avg_processing_time_ms": float64(avgTime.Nanoseconds()) / 1e6,
		}

		if detailedStats := aom.streamer.GetDetailedStats(); detailedStats != nil {
			comprehensiveMetrics["detailed"] = detailedStats
		}
	}

	return comprehensiveMetrics
}

// LogPerformanceStats logs current performance statistics
func (aom *AudioOutputManager) LogPerformanceStats() {
	metrics := aom.GetMetrics()
	aom.logger.Info().
		Int64("frames_received", metrics.FramesReceived).
		Int64("frames_dropped", metrics.FramesDropped).
		Int64("bytes_processed", metrics.BytesProcessed).
		Int64("connection_drops", metrics.ConnectionDrops).
		Float64("average_latency_ms", float64(metrics.AverageLatency.Nanoseconds())/1e6).
		Bool("running", aom.IsRunning()).
		Bool("ready", aom.IsReady()).
		Msg("Audio output manager performance stats")
}

// GetStreamer returns the streamer for advanced operations
func (aom *AudioOutputManager) GetStreamer() *AudioOutputStreamer {
	return aom.streamer
}
