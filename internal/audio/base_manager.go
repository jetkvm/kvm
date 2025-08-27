package audio

import (
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"
)

// BaseAudioMetrics provides common metrics fields for both input and output
// Atomic fields MUST be first for ARM32 alignment (int64 fields need 8-byte alignment)
type BaseAudioMetrics struct {
	// Atomic int64 fields first for proper ARM32 alignment
	FramesProcessed int64 `json:"frames_processed"`
	FramesDropped   int64 `json:"frames_dropped"`
	BytesProcessed  int64 `json:"bytes_processed"`
	ConnectionDrops int64 `json:"connection_drops"`

	// Non-atomic fields after atomic fields
	LastFrameTime  time.Time     `json:"last_frame_time"`
	AverageLatency time.Duration `json:"average_latency"`
}

// BaseAudioManager provides common functionality for audio managers
type BaseAudioManager struct {
	metrics BaseAudioMetrics
	logger  zerolog.Logger
	running int32
}

// NewBaseAudioManager creates a new base audio manager
func NewBaseAudioManager(logger zerolog.Logger) *BaseAudioManager {
	return &BaseAudioManager{
		logger: logger,
	}
}

// IsRunning returns whether the manager is running
func (bam *BaseAudioManager) IsRunning() bool {
	return atomic.LoadInt32(&bam.running) == 1
}

// setRunning atomically sets the running state
func (bam *BaseAudioManager) setRunning(running bool) bool {
	if running {
		return atomic.CompareAndSwapInt32(&bam.running, 0, 1)
	}
	return atomic.CompareAndSwapInt32(&bam.running, 1, 0)
}

// resetMetrics resets all metrics to zero
func (bam *BaseAudioManager) resetMetrics() {
	atomic.StoreInt64(&bam.metrics.FramesProcessed, 0)
	atomic.StoreInt64(&bam.metrics.FramesDropped, 0)
	atomic.StoreInt64(&bam.metrics.BytesProcessed, 0)
	atomic.StoreInt64(&bam.metrics.ConnectionDrops, 0)
	bam.metrics.LastFrameTime = time.Time{}
	bam.metrics.AverageLatency = 0
}

// getBaseMetrics returns a copy of the base metrics
func (bam *BaseAudioManager) getBaseMetrics() BaseAudioMetrics {
	return BaseAudioMetrics{
		FramesProcessed: atomic.LoadInt64(&bam.metrics.FramesProcessed),
		FramesDropped:   atomic.LoadInt64(&bam.metrics.FramesDropped),
		BytesProcessed:  atomic.LoadInt64(&bam.metrics.BytesProcessed),
		ConnectionDrops: atomic.LoadInt64(&bam.metrics.ConnectionDrops),
		LastFrameTime:   bam.metrics.LastFrameTime,
		AverageLatency:  bam.metrics.AverageLatency,
	}
}

// recordFrameProcessed records a processed frame
func (bam *BaseAudioManager) recordFrameProcessed(bytes int) {
	atomic.AddInt64(&bam.metrics.FramesProcessed, 1)
	atomic.AddInt64(&bam.metrics.BytesProcessed, int64(bytes))
	bam.metrics.LastFrameTime = time.Now()
}

// recordFrameDropped records a dropped frame
func (bam *BaseAudioManager) recordFrameDropped() {
	atomic.AddInt64(&bam.metrics.FramesDropped, 1)
}

// updateLatency updates the average latency
func (bam *BaseAudioManager) updateLatency(latency time.Duration) {
	// Simple moving average - could be enhanced with more sophisticated algorithms
	currentAvg := bam.metrics.AverageLatency
	if currentAvg == 0 {
		bam.metrics.AverageLatency = latency
	} else {
		// Weighted average: 90% old + 10% new
		bam.metrics.AverageLatency = time.Duration(float64(currentAvg)*0.9 + float64(latency)*0.1)
	}
}

// logComponentStart logs component start with consistent format
func (bam *BaseAudioManager) logComponentStart(component string) {
	bam.logger.Info().Str("component", component).Msg("starting component")
}

// logComponentStarted logs component started with consistent format
func (bam *BaseAudioManager) logComponentStarted(component string) {
	bam.logger.Info().Str("component", component).Msg("component started successfully")
}

// logComponentStop logs component stop with consistent format
func (bam *BaseAudioManager) logComponentStop(component string) {
	bam.logger.Info().Str("component", component).Msg("stopping component")
}

// logComponentStopped logs component stopped with consistent format
func (bam *BaseAudioManager) logComponentStopped(component string) {
	bam.logger.Info().Str("component", component).Msg("component stopped")
}

// logComponentError logs component error with consistent format
func (bam *BaseAudioManager) logComponentError(component string, err error, msg string) {
	bam.logger.Error().Err(err).Str("component", component).Msg(msg)
}
