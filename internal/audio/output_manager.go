package audio

import (
	"sync/atomic"
	"time"

	"github.com/jetkvm/kvm/internal/logging"
	"github.com/rs/zerolog"
)

// AudioOutputManager manages audio output stream using IPC mode
type AudioOutputManager struct {
	metrics AudioOutputMetrics

	streamer *AudioOutputStreamer
	logger   zerolog.Logger
	running  int32
}

// AudioOutputMetrics tracks output-specific metrics
type AudioOutputMetrics struct {
	FramesReceived  int64
	FramesDropped   int64
	BytesProcessed  int64
	ConnectionDrops int64
	LastFrameTime   time.Time
	AverageLatency  time.Duration
}

// NewAudioOutputManager creates a new audio output manager
func NewAudioOutputManager() *AudioOutputManager {
	streamer, err := NewAudioOutputStreamer()
	if err != nil {
		// Log error but continue with nil streamer - will be handled gracefully
		logger := logging.GetDefaultLogger().With().Str("component", AudioOutputManagerComponent).Logger()
		logger.Error().Err(err).Msg("Failed to create audio output streamer")
	}

	return &AudioOutputManager{
		streamer: streamer,
		logger:   logging.GetDefaultLogger().With().Str("component", AudioOutputManagerComponent).Logger(),
	}
}

// Start starts the audio output manager
func (aom *AudioOutputManager) Start() error {
	if !atomic.CompareAndSwapInt32(&aom.running, 0, 1) {
		return nil // Already running
	}

	aom.logger.Info().Str("component", AudioOutputManagerComponent).Msg("starting component")

	if aom.streamer == nil {
		// Try to recreate streamer if it was nil
		streamer, err := NewAudioOutputStreamer()
		if err != nil {
			atomic.StoreInt32(&aom.running, 0)
			aom.logger.Error().Err(err).Str("component", AudioOutputManagerComponent).Msg("failed to create audio output streamer")
			return err
		}
		aom.streamer = streamer
	}

	err := aom.streamer.Start()
	if err != nil {
		atomic.StoreInt32(&aom.running, 0)
		// Reset metrics on failed start
		aom.resetMetrics()
		aom.logger.Error().Err(err).Str("component", AudioOutputManagerComponent).Msg("failed to start component")
		return err
	}

	aom.logger.Info().Str("component", AudioOutputManagerComponent).Msg("component started successfully")
	return nil
}

// Stop stops the audio output manager
func (aom *AudioOutputManager) Stop() {
	if !atomic.CompareAndSwapInt32(&aom.running, 1, 0) {
		return // Already stopped
	}

	aom.logger.Info().Str("component", AudioOutputManagerComponent).Msg("stopping component")

	if aom.streamer != nil {
		aom.streamer.Stop()
	}

	aom.logger.Info().Str("component", AudioOutputManagerComponent).Msg("component stopped")
}

// resetMetrics resets all metrics to zero
func (aom *AudioOutputManager) resetMetrics() {
	atomic.StoreInt64(&aom.metrics.FramesReceived, 0)
	atomic.StoreInt64(&aom.metrics.FramesDropped, 0)
	atomic.StoreInt64(&aom.metrics.BytesProcessed, 0)
	atomic.StoreInt64(&aom.metrics.ConnectionDrops, 0)
}

// IsRunning returns whether the audio output manager is running
func (aom *AudioOutputManager) IsRunning() bool {
	return atomic.LoadInt32(&aom.running) == 1
}

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
		FramesReceived:  atomic.LoadInt64(&aom.metrics.FramesReceived),
		FramesDropped:   atomic.LoadInt64(&aom.metrics.FramesDropped),
		BytesProcessed:  atomic.LoadInt64(&aom.metrics.BytesProcessed),
		ConnectionDrops: atomic.LoadInt64(&aom.metrics.ConnectionDrops),
		AverageLatency:  aom.metrics.AverageLatency,
		LastFrameTime:   aom.metrics.LastFrameTime,
	}
}

// GetComprehensiveMetrics returns detailed performance metrics
func (aom *AudioOutputManager) GetComprehensiveMetrics() map[string]interface{} {
	baseMetrics := aom.GetMetrics()

	comprehensiveMetrics := map[string]interface{}{
		"manager": map[string]interface{}{
			"frames_received":    baseMetrics.FramesReceived,
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