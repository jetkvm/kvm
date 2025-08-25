package audio

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"
)

// LatencyMonitor tracks and optimizes audio latency in real-time
type LatencyMonitor struct {
	// Atomic fields MUST be first for ARM32 alignment (int64 fields need 8-byte alignment)
	currentLatency    int64 // Current latency in nanoseconds (atomic)
	averageLatency    int64 // Rolling average latency in nanoseconds (atomic)
	minLatency        int64 // Minimum observed latency in nanoseconds (atomic)
	maxLatency        int64 // Maximum observed latency in nanoseconds (atomic)
	latencySamples    int64 // Number of latency samples collected (atomic)
	jitterAccumulator int64 // Accumulated jitter for variance calculation (atomic)
	lastOptimization  int64 // Timestamp of last optimization in nanoseconds (atomic)

	config LatencyConfig
	logger zerolog.Logger

	// Control channels
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Optimization callbacks
	optimizationCallbacks []OptimizationCallback
	mutex                 sync.RWMutex

	// Performance tracking
	latencyHistory []LatencyMeasurement
	historyMutex   sync.RWMutex
}

// LatencyConfig holds configuration for latency monitoring
type LatencyConfig struct {
	TargetLatency        time.Duration // Target latency to maintain
	MaxLatency           time.Duration // Maximum acceptable latency
	OptimizationInterval time.Duration // How often to run optimization
	HistorySize          int           // Number of latency measurements to keep
	JitterThreshold      time.Duration // Jitter threshold for optimization
	AdaptiveThreshold    float64       // Threshold for adaptive adjustments (0.0-1.0)
}

// LatencyMeasurement represents a single latency measurement
type LatencyMeasurement struct {
	Timestamp time.Time
	Latency   time.Duration
	Jitter    time.Duration
	Source    string // Source of the measurement (e.g., "input", "output", "processing")
}

// OptimizationCallback is called when latency optimization is triggered
type OptimizationCallback func(metrics LatencyMetrics) error

// LatencyMetrics provides comprehensive latency statistics
type LatencyMetrics struct {
	Current     time.Duration
	Average     time.Duration
	Min         time.Duration
	Max         time.Duration
	Jitter      time.Duration
	SampleCount int64
	Trend       LatencyTrend
}

// LatencyTrend indicates the direction of latency changes
type LatencyTrend int

const (
	LatencyTrendStable LatencyTrend = iota
	LatencyTrendIncreasing
	LatencyTrendDecreasing
	LatencyTrendVolatile
)

// DefaultLatencyConfig returns a sensible default configuration
func DefaultLatencyConfig() LatencyConfig {
	return LatencyConfig{
		TargetLatency:        50 * time.Millisecond,
		MaxLatency:           GetConfig().MaxLatencyThreshold,
		OptimizationInterval: 5 * time.Second,
		HistorySize:          GetConfig().LatencyHistorySize,
		JitterThreshold:      GetConfig().JitterThreshold,
		AdaptiveThreshold:    0.8, // Trigger optimization when 80% above target
	}
}

// NewLatencyMonitor creates a new latency monitoring system
func NewLatencyMonitor(config LatencyConfig, logger zerolog.Logger) *LatencyMonitor {
	ctx, cancel := context.WithCancel(context.Background())

	return &LatencyMonitor{
		config:         config,
		logger:         logger.With().Str("component", "latency-monitor").Logger(),
		ctx:            ctx,
		cancel:         cancel,
		latencyHistory: make([]LatencyMeasurement, 0, config.HistorySize),
		minLatency:     int64(time.Hour), // Initialize to high value
	}
}

// Start begins latency monitoring and optimization
func (lm *LatencyMonitor) Start() {
	lm.wg.Add(1)
	go lm.monitoringLoop()
	lm.logger.Info().Msg("Latency monitor started")
}

// Stop stops the latency monitor
func (lm *LatencyMonitor) Stop() {
	lm.cancel()
	lm.wg.Wait()
	lm.logger.Info().Msg("Latency monitor stopped")
}

// RecordLatency records a new latency measurement
func (lm *LatencyMonitor) RecordLatency(latency time.Duration, source string) {
	now := time.Now()
	latencyNanos := latency.Nanoseconds()

	// Update atomic counters
	atomic.StoreInt64(&lm.currentLatency, latencyNanos)
	atomic.AddInt64(&lm.latencySamples, 1)

	// Update min/max
	for {
		oldMin := atomic.LoadInt64(&lm.minLatency)
		if latencyNanos >= oldMin || atomic.CompareAndSwapInt64(&lm.minLatency, oldMin, latencyNanos) {
			break
		}
	}

	for {
		oldMax := atomic.LoadInt64(&lm.maxLatency)
		if latencyNanos <= oldMax || atomic.CompareAndSwapInt64(&lm.maxLatency, oldMax, latencyNanos) {
			break
		}
	}

	// Update rolling average using exponential moving average
	oldAvg := atomic.LoadInt64(&lm.averageLatency)
	newAvg := oldAvg + (latencyNanos-oldAvg)/10 // Alpha = 0.1
	atomic.StoreInt64(&lm.averageLatency, newAvg)

	// Calculate jitter (difference from average)
	jitter := latencyNanos - newAvg
	if jitter < 0 {
		jitter = -jitter
	}
	atomic.AddInt64(&lm.jitterAccumulator, jitter)

	// Store in history
	lm.historyMutex.Lock()
	measurement := LatencyMeasurement{
		Timestamp: now,
		Latency:   latency,
		Jitter:    time.Duration(jitter),
		Source:    source,
	}

	if len(lm.latencyHistory) >= lm.config.HistorySize {
		// Remove oldest measurement
		copy(lm.latencyHistory, lm.latencyHistory[1:])
		lm.latencyHistory[len(lm.latencyHistory)-1] = measurement
	} else {
		lm.latencyHistory = append(lm.latencyHistory, measurement)
	}
	lm.historyMutex.Unlock()
}

// GetMetrics returns current latency metrics
func (lm *LatencyMonitor) GetMetrics() LatencyMetrics {
	current := atomic.LoadInt64(&lm.currentLatency)
	average := atomic.LoadInt64(&lm.averageLatency)
	min := atomic.LoadInt64(&lm.minLatency)
	max := atomic.LoadInt64(&lm.maxLatency)
	samples := atomic.LoadInt64(&lm.latencySamples)
	jitterSum := atomic.LoadInt64(&lm.jitterAccumulator)

	var jitter time.Duration
	if samples > 0 {
		jitter = time.Duration(jitterSum / samples)
	}

	return LatencyMetrics{
		Current:     time.Duration(current),
		Average:     time.Duration(average),
		Min:         time.Duration(min),
		Max:         time.Duration(max),
		Jitter:      jitter,
		SampleCount: samples,
		Trend:       lm.calculateTrend(),
	}
}

// AddOptimizationCallback adds a callback for latency optimization
func (lm *LatencyMonitor) AddOptimizationCallback(callback OptimizationCallback) {
	lm.mutex.Lock()
	lm.optimizationCallbacks = append(lm.optimizationCallbacks, callback)
	lm.mutex.Unlock()
}

// monitoringLoop runs the main monitoring and optimization loop
func (lm *LatencyMonitor) monitoringLoop() {
	defer lm.wg.Done()

	ticker := time.NewTicker(lm.config.OptimizationInterval)
	defer ticker.Stop()

	for {
		select {
		case <-lm.ctx.Done():
			return
		case <-ticker.C:
			lm.runOptimization()
		}
	}
}

// runOptimization checks if optimization is needed and triggers callbacks
func (lm *LatencyMonitor) runOptimization() {
	metrics := lm.GetMetrics()

	// Check if optimization is needed
	needsOptimization := false

	// Check if current latency exceeds threshold
	if metrics.Current > lm.config.MaxLatency {
		needsOptimization = true
		lm.logger.Warn().Dur("current_latency", metrics.Current).Dur("max_latency", lm.config.MaxLatency).Msg("Latency exceeds maximum threshold")
	}

	// Check if average latency is above adaptive threshold
	adaptiveThreshold := time.Duration(float64(lm.config.TargetLatency.Nanoseconds()) * (1.0 + lm.config.AdaptiveThreshold))
	if metrics.Average > adaptiveThreshold {
		needsOptimization = true
		lm.logger.Info().Dur("average_latency", metrics.Average).Dur("threshold", adaptiveThreshold).Msg("Average latency above adaptive threshold")
	}

	// Check if jitter is too high
	if metrics.Jitter > lm.config.JitterThreshold {
		needsOptimization = true
		lm.logger.Info().Dur("jitter", metrics.Jitter).Dur("threshold", lm.config.JitterThreshold).Msg("Jitter above threshold")
	}

	if needsOptimization {
		atomic.StoreInt64(&lm.lastOptimization, time.Now().UnixNano())

		// Run optimization callbacks
		lm.mutex.RLock()
		callbacks := make([]OptimizationCallback, len(lm.optimizationCallbacks))
		copy(callbacks, lm.optimizationCallbacks)
		lm.mutex.RUnlock()

		for _, callback := range callbacks {
			if err := callback(metrics); err != nil {
				lm.logger.Error().Err(err).Msg("Optimization callback failed")
			}
		}

		lm.logger.Info().Interface("metrics", metrics).Msg("Latency optimization triggered")
	}
}

// calculateTrend analyzes recent latency measurements to determine trend
func (lm *LatencyMonitor) calculateTrend() LatencyTrend {
	lm.historyMutex.RLock()
	defer lm.historyMutex.RUnlock()

	if len(lm.latencyHistory) < 10 {
		return LatencyTrendStable
	}

	// Analyze last 10 measurements
	recentMeasurements := lm.latencyHistory[len(lm.latencyHistory)-10:]

	var increasing, decreasing int
	for i := 1; i < len(recentMeasurements); i++ {
		if recentMeasurements[i].Latency > recentMeasurements[i-1].Latency {
			increasing++
		} else if recentMeasurements[i].Latency < recentMeasurements[i-1].Latency {
			decreasing++
		}
	}

	// Determine trend based on direction changes
	if increasing > 6 {
		return LatencyTrendIncreasing
	} else if decreasing > 6 {
		return LatencyTrendDecreasing
	} else if increasing+decreasing > 7 {
		return LatencyTrendVolatile
	}

	return LatencyTrendStable
}

// GetLatencyHistory returns a copy of recent latency measurements
func (lm *LatencyMonitor) GetLatencyHistory() []LatencyMeasurement {
	lm.historyMutex.RLock()
	defer lm.historyMutex.RUnlock()

	history := make([]LatencyMeasurement, len(lm.latencyHistory))
	copy(history, lm.latencyHistory)
	return history
}
