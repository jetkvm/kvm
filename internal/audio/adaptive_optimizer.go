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
	optimizationCount int64 // Number of optimizations performed (atomic)
	lastOptimization  int64 // Timestamp of last optimization (atomic)
	optimizationLevel int64 // Current optimization level (0-10) (atomic)

	latencyMonitor *LatencyMonitor
	bufferManager  *AdaptiveBufferManager
	logger         zerolog.Logger

	// Control channels
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Configuration
	config OptimizerConfig
}

// OptimizerConfig holds configuration for the adaptive optimizer
type OptimizerConfig struct {
	MaxOptimizationLevel int           // Maximum optimization level (0-10)
	CooldownPeriod       time.Duration // Minimum time between optimizations
	Aggressiveness       float64       // How aggressively to optimize (0.0-1.0)
	RollbackThreshold    time.Duration // Latency threshold to rollback optimizations
	StabilityPeriod      time.Duration // Time to wait for stability after optimization
}

// DefaultOptimizerConfig returns a sensible default configuration
func DefaultOptimizerConfig() OptimizerConfig {
	return OptimizerConfig{
		MaxOptimizationLevel: 8,
		CooldownPeriod:       GetConfig().CooldownPeriod,
		Aggressiveness:       GetConfig().OptimizerAggressiveness,
		RollbackThreshold:    GetConfig().RollbackThreshold,
		StabilityPeriod:      GetConfig().AdaptiveOptimizerStability,
	}
}

// NewAdaptiveOptimizer creates a new adaptive optimizer
func NewAdaptiveOptimizer(latencyMonitor *LatencyMonitor, bufferManager *AdaptiveBufferManager, config OptimizerConfig, logger zerolog.Logger) *AdaptiveOptimizer {
	ctx, cancel := context.WithCancel(context.Background())

	optimizer := &AdaptiveOptimizer{
		latencyMonitor: latencyMonitor,
		bufferManager:  bufferManager,
		config:         config,
		logger:         logger.With().Str("component", "adaptive-optimizer").Logger(),
		ctx:            ctx,
		cancel:         cancel,
	}

	// Register as latency monitor callback
	latencyMonitor.AddOptimizationCallback(optimizer.handleLatencyOptimization)

	return optimizer
}

// Start begins the adaptive optimization process
func (ao *AdaptiveOptimizer) Start() {
	ao.wg.Add(1)
	go ao.optimizationLoop()
	ao.logger.Info().Msg("Adaptive optimizer started")
}

// Stop stops the adaptive optimizer
func (ao *AdaptiveOptimizer) Stop() {
	ao.cancel()
	ao.wg.Wait()
	ao.logger.Info().Msg("Adaptive optimizer stopped")
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
	latencyRatio := float64(metrics.Current) / float64(GetConfig().LatencyTarget) // 50ms target

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

	ticker := time.NewTicker(ao.config.StabilityPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-ao.ctx.Done():
			return
		case <-ticker.C:
			ao.checkStability()
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
			ao.logger.Warn().Dur("current_latency", metrics.Current).Dur("threshold", ao.config.RollbackThreshold).Msg("Rolling back optimizations due to excessive latency")
			if err := ao.decreaseOptimization(currentLevel - 1); err != nil {
				ao.logger.Error().Err(err).Msg("Failed to decrease optimization level")
			}
		}
	}
}

// GetOptimizationStats returns current optimization statistics
func (ao *AdaptiveOptimizer) GetOptimizationStats() map[string]interface{} {
	return map[string]interface{}{
		"optimization_level": atomic.LoadInt64(&ao.optimizationLevel),
		"optimization_count": atomic.LoadInt64(&ao.optimizationCount),
		"last_optimization":  time.Unix(0, atomic.LoadInt64(&ao.lastOptimization)),
	}
}

// Strategy implementation methods (stubs for now)
