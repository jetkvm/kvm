package audio

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"
)

// AdaptiveOptimizer automatically adjusts audio parameters based on latency metrics
type AdaptiveOptimizer struct {
	// Atomic fields MUST be first for ARM32 alignment (int64 fields need 8-byte alignment)
	optimizationCount    int64 // Number of optimizations performed (atomic)
	lastOptimization     int64 // Timestamp of last optimization (atomic)
	optimizationLevel    int64 // Current optimization level (0-10) (atomic)
	stabilityScore       int64 // Current stability score (0-100) (atomic)
	optimizationInterval int64 // Current optimization interval in nanoseconds (atomic)

	latencyMonitor *LatencyMonitor
	bufferManager  *AdaptiveBufferManager
	logger         zerolog.Logger

	// Control channels
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Configuration
	config OptimizerConfig

	// Stability tracking
	stabilityHistory []StabilityMetric
	stabilityMutex   sync.RWMutex
}

// StabilityMetric tracks system stability over time
type StabilityMetric struct {
	Timestamp      time.Time
	LatencyStdev   float64
	CPUVariance    float64
	MemoryStable   bool
	ErrorRate      float64
	StabilityScore int
}

// OptimizerConfig holds configuration for the adaptive optimizer
type OptimizerConfig struct {
	MaxOptimizationLevel int           // Maximum optimization level (0-10)
	CooldownPeriod       time.Duration // Minimum time between optimizations
	Aggressiveness       float64       // How aggressively to optimize (0.0-1.0)
	RollbackThreshold    time.Duration // Latency threshold to rollback optimizations
	StabilityPeriod      time.Duration // Time to wait for stability after optimization

	// Adaptive interval configuration
	MinOptimizationInterval time.Duration // Minimum optimization interval (high stability)
	MaxOptimizationInterval time.Duration // Maximum optimization interval (low stability)
	StabilityThreshold      int           // Stability score threshold for interval adjustment
	StabilityHistorySize    int           // Number of stability metrics to track
}

// DefaultOptimizerConfig returns a sensible default configuration
func DefaultOptimizerConfig() OptimizerConfig {
	return OptimizerConfig{
		MaxOptimizationLevel: 8,
		CooldownPeriod:       GetConfig().CooldownPeriod,
		Aggressiveness:       GetConfig().OptimizerAggressiveness,
		RollbackThreshold:    GetConfig().RollbackThreshold,
		StabilityPeriod:      GetConfig().AdaptiveOptimizerStability,

		// Adaptive interval defaults
		MinOptimizationInterval: 100 * time.Millisecond, // High stability: check every 100ms
		MaxOptimizationInterval: 2 * time.Second,        // Low stability: check every 2s
		StabilityThreshold:      70,                     // Stability score threshold
		StabilityHistorySize:    20,                     // Track last 20 stability metrics
	}
}

// NewAdaptiveOptimizer creates a new adaptive optimizer
func NewAdaptiveOptimizer(latencyMonitor *LatencyMonitor, bufferManager *AdaptiveBufferManager, config OptimizerConfig, logger zerolog.Logger) *AdaptiveOptimizer {
	ctx, cancel := context.WithCancel(context.Background())

	optimizer := &AdaptiveOptimizer{
		latencyMonitor:   latencyMonitor,
		bufferManager:    bufferManager,
		config:           config,
		logger:           logger.With().Str("component", "adaptive-optimizer").Logger(),
		ctx:              ctx,
		cancel:           cancel,
		stabilityHistory: make([]StabilityMetric, 0, config.StabilityHistorySize),
	}

	// Initialize stability score and optimization interval
	atomic.StoreInt64(&optimizer.stabilityScore, 50) // Start with medium stability
	atomic.StoreInt64(&optimizer.optimizationInterval, int64(config.MaxOptimizationInterval))

	// Register as latency monitor callback
	latencyMonitor.AddOptimizationCallback(optimizer.handleLatencyOptimization)

	return optimizer
}

// Start begins the adaptive optimization process
func (ao *AdaptiveOptimizer) Start() {
	ao.wg.Add(1)
	go ao.optimizationLoop()
	ao.logger.Debug().Msg("adaptive optimizer started")
}

// Stop stops the adaptive optimizer
func (ao *AdaptiveOptimizer) Stop() {
	ao.cancel()
	ao.wg.Wait()
	ao.logger.Debug().Msg("adaptive optimizer stopped")
}

// initializeStrategies sets up the available optimization strategies

// handleLatencyOptimization is called when latency optimization is needed
func (ao *AdaptiveOptimizer) handleLatencyOptimization(metrics LatencyMetrics) error {
	currentLevel := atomic.LoadInt64(&ao.optimizationLevel)
	lastOpt := atomic.LoadInt64(&ao.lastOptimization)

	// Check cooldown period
	if time.Since(time.Unix(0, lastOpt)) < ao.config.CooldownPeriod {
		return nil
	}

	// Determine if we need to increase or decrease optimization level
	targetLevel := ao.calculateTargetOptimizationLevel(metrics)

	if targetLevel > currentLevel {
		return ao.increaseOptimization(int(targetLevel))
	} else if targetLevel < currentLevel {
		return ao.decreaseOptimization(int(targetLevel))
	}

	return nil
}

// calculateTargetOptimizationLevel determines the appropriate optimization level
func (ao *AdaptiveOptimizer) calculateTargetOptimizationLevel(metrics LatencyMetrics) int64 {
	// Base calculation on current latency vs target
	latencyRatio := float64(metrics.Current) / float64(GetConfig().AdaptiveOptimizerLatencyTarget) // 50ms target

	// Adjust based on trend
	switch metrics.Trend {
	case LatencyTrendIncreasing:
		latencyRatio *= 1.2 // Be more aggressive
	case LatencyTrendDecreasing:
		latencyRatio *= 0.8 // Be less aggressive
	case LatencyTrendVolatile:
		latencyRatio *= 1.1 // Slightly more aggressive
	}

	// Apply aggressiveness factor
	latencyRatio *= ao.config.Aggressiveness

	// Convert to optimization level
	targetLevel := int64(latencyRatio * GetConfig().LatencyScalingFactor) // Scale to 0-10 range
	if targetLevel > int64(ao.config.MaxOptimizationLevel) {
		targetLevel = int64(ao.config.MaxOptimizationLevel)
	}
	if targetLevel < 0 {
		targetLevel = 0
	}

	return targetLevel
}

// increaseOptimization applies optimization strategies up to the target level
func (ao *AdaptiveOptimizer) increaseOptimization(targetLevel int) error {
	atomic.StoreInt64(&ao.optimizationLevel, int64(targetLevel))
	atomic.StoreInt64(&ao.lastOptimization, time.Now().UnixNano())
	atomic.AddInt64(&ao.optimizationCount, 1)

	return nil
}

// decreaseOptimization rolls back optimization strategies to the target level
func (ao *AdaptiveOptimizer) decreaseOptimization(targetLevel int) error {
	atomic.StoreInt64(&ao.optimizationLevel, int64(targetLevel))
	atomic.StoreInt64(&ao.lastOptimization, time.Now().UnixNano())

	return nil
}

// optimizationLoop runs the main optimization monitoring loop
func (ao *AdaptiveOptimizer) optimizationLoop() {
	defer ao.wg.Done()

	// Start with initial interval
	currentInterval := time.Duration(atomic.LoadInt64(&ao.optimizationInterval))
	ticker := time.NewTicker(currentInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ao.ctx.Done():
			return
		case <-ticker.C:
			// Update stability metrics and check for optimization needs
			ao.updateStabilityMetrics()
			ao.checkStability()

			// Adjust optimization interval based on current stability
			newInterval := ao.calculateOptimizationInterval()
			if newInterval != currentInterval {
				currentInterval = newInterval
				ticker.Reset(currentInterval)
				ao.logger.Debug().Dur("new_interval", currentInterval).Int64("stability_score", atomic.LoadInt64(&ao.stabilityScore)).Msg("adjusted optimization interval")
			}
		}
	}
}

// checkStability monitors system stability and rolls back if needed
func (ao *AdaptiveOptimizer) checkStability() {
	metrics := ao.latencyMonitor.GetMetrics()

	// Check if we need to rollback due to excessive latency
	if metrics.Current > ao.config.RollbackThreshold {
		currentLevel := int(atomic.LoadInt64(&ao.optimizationLevel))
		if currentLevel > 0 {
			ao.logger.Warn().Dur("current_latency", metrics.Current).Dur("threshold", ao.config.RollbackThreshold).Msg("rolling back optimizations due to excessive latency")
			if err := ao.decreaseOptimization(currentLevel - 1); err != nil {
				ao.logger.Error().Err(err).Msg("failed to decrease optimization level")
			}
		}
	}
}

// updateStabilityMetrics calculates and stores current system stability metrics
func (ao *AdaptiveOptimizer) updateStabilityMetrics() {
	metrics := ao.latencyMonitor.GetMetrics()

	// Calculate stability score based on multiple factors
	stabilityScore := ao.calculateStabilityScore(metrics)
	atomic.StoreInt64(&ao.stabilityScore, int64(stabilityScore))

	// Store stability metric in history
	stabilityMetric := StabilityMetric{
		Timestamp:      time.Now(),
		LatencyStdev:   float64(metrics.Jitter), // Use Jitter as variance indicator
		CPUVariance:    0.0,                     // TODO: Get from system metrics
		MemoryStable:   true,                    // TODO: Get from system metrics
		ErrorRate:      0.0,                     // TODO: Get from error tracking
		StabilityScore: stabilityScore,
	}

	ao.stabilityMutex.Lock()
	ao.stabilityHistory = append(ao.stabilityHistory, stabilityMetric)
	if len(ao.stabilityHistory) > ao.config.StabilityHistorySize {
		ao.stabilityHistory = ao.stabilityHistory[1:]
	}
	ao.stabilityMutex.Unlock()
}

// calculateStabilityScore computes a stability score (0-100) based on system metrics
func (ao *AdaptiveOptimizer) calculateStabilityScore(metrics LatencyMetrics) int {
	// Base score starts at 100 (perfect stability)
	score := 100.0

	// Penalize high jitter (latency variance)
	if metrics.Jitter > 0 && metrics.Average > 0 {
		jitterRatio := float64(metrics.Jitter) / float64(metrics.Average)
		variancePenalty := jitterRatio * 50 // Scale jitter impact
		score -= variancePenalty
	}

	// Penalize latency trend volatility
	switch metrics.Trend {
	case LatencyTrendVolatile:
		score -= 20
	case LatencyTrendIncreasing:
		score -= 10
	case LatencyTrendDecreasing:
		score += 5 // Slight bonus for improving latency
	}

	// Ensure score is within bounds
	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}

	return int(score)
}

// calculateOptimizationInterval determines the optimization interval based on stability
func (ao *AdaptiveOptimizer) calculateOptimizationInterval() time.Duration {
	stabilityScore := atomic.LoadInt64(&ao.stabilityScore)

	// High stability = shorter intervals (more frequent optimization)
	// Low stability = longer intervals (less frequent optimization)
	if stabilityScore >= int64(ao.config.StabilityThreshold) {
		// High stability: use minimum interval
		interval := ao.config.MinOptimizationInterval
		atomic.StoreInt64(&ao.optimizationInterval, int64(interval))
		return interval
	} else {
		// Low stability: scale interval based on stability score
		// Lower stability = longer intervals
		stabilityRatio := float64(stabilityScore) / float64(ao.config.StabilityThreshold)
		minInterval := float64(ao.config.MinOptimizationInterval)
		maxInterval := float64(ao.config.MaxOptimizationInterval)

		// Linear interpolation between min and max intervals
		interval := time.Duration(minInterval + (maxInterval-minInterval)*(1.0-stabilityRatio))
		atomic.StoreInt64(&ao.optimizationInterval, int64(interval))
		return interval
	}
}

// GetOptimizationStats returns current optimization statistics
func (ao *AdaptiveOptimizer) GetOptimizationStats() map[string]interface{} {
	return map[string]interface{}{
		"optimization_level":    atomic.LoadInt64(&ao.optimizationLevel),
		"optimization_count":    atomic.LoadInt64(&ao.optimizationCount),
		"last_optimization":     time.Unix(0, atomic.LoadInt64(&ao.lastOptimization)),
		"stability_score":       atomic.LoadInt64(&ao.stabilityScore),
		"optimization_interval": time.Duration(atomic.LoadInt64(&ao.optimizationInterval)),
	}
}

// Strategy implementation methods (stubs for now)
