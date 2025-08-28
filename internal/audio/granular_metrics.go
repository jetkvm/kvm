package audio

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/jetkvm/kvm/internal/logging"
	"github.com/rs/zerolog"
)

// LatencyPercentiles holds calculated percentile values
type LatencyPercentiles struct {
	P50 time.Duration `json:"p50"`
	P95 time.Duration `json:"p95"`
	P99 time.Duration `json:"p99"`
	Min time.Duration `json:"min"`
	Max time.Duration `json:"max"`
	Avg time.Duration `json:"avg"`
}

// BufferPoolEfficiencyMetrics tracks detailed buffer pool performance
type BufferPoolEfficiencyMetrics struct {
	// Pool utilization metrics
	HitRate           float64 `json:"hit_rate"`
	MissRate          float64 `json:"miss_rate"`
	UtilizationRate   float64 `json:"utilization_rate"`
	FragmentationRate float64 `json:"fragmentation_rate"`

	// Memory efficiency metrics
	MemoryEfficiency   float64 `json:"memory_efficiency"`
	AllocationOverhead float64 `json:"allocation_overhead"`
	ReuseEffectiveness float64 `json:"reuse_effectiveness"`

	// Performance metrics
	AverageGetLatency time.Duration `json:"average_get_latency"`
	AveragePutLatency time.Duration `json:"average_put_latency"`
	Throughput        float64       `json:"throughput"` // Operations per second
}

// GranularMetricsCollector aggregates all granular metrics
type GranularMetricsCollector struct {
	// Buffer pool efficiency tracking
	framePoolMetrics   *BufferPoolEfficiencyTracker
	controlPoolMetrics *BufferPoolEfficiencyTracker
	zeroCopyMetrics    *BufferPoolEfficiencyTracker

	mutex  sync.RWMutex
	logger zerolog.Logger
}

// BufferPoolEfficiencyTracker tracks detailed efficiency metrics for a buffer pool
type BufferPoolEfficiencyTracker struct {
	// Atomic counters
	getOperations   int64 // Total get operations (atomic)
	putOperations   int64 // Total put operations (atomic)
	getLatencySum   int64 // Sum of get latencies in nanoseconds (atomic)
	putLatencySum   int64 // Sum of put latencies in nanoseconds (atomic)
	allocationBytes int64 // Total bytes allocated (atomic)
	reuseCount      int64 // Number of successful reuses (atomic)

	// Recent operation times for throughput calculation
	recentOps []time.Time
	opsMutex  sync.RWMutex

	poolName string
	logger   zerolog.Logger
}

// NewBufferPoolEfficiencyTracker creates a new efficiency tracker
func NewBufferPoolEfficiencyTracker(poolName string, logger zerolog.Logger) *BufferPoolEfficiencyTracker {
	return &BufferPoolEfficiencyTracker{
		recentOps: make([]time.Time, 0, 1000), // Track last 1000 operations
		poolName:  poolName,
		logger:    logger,
	}
}

// RecordGetOperation records a buffer get operation with its latency
func (bpet *BufferPoolEfficiencyTracker) RecordGetOperation(latency time.Duration, wasHit bool) {
	atomic.AddInt64(&bpet.getOperations, 1)
	atomic.AddInt64(&bpet.getLatencySum, latency.Nanoseconds())

	if wasHit {
		atomic.AddInt64(&bpet.reuseCount, 1)
	}

	// Record operation time for throughput calculation
	bpet.opsMutex.Lock()
	now := time.Now()
	if len(bpet.recentOps) >= 1000 {
		bpet.recentOps = bpet.recentOps[1:]
	}
	bpet.recentOps = append(bpet.recentOps, now)
	bpet.opsMutex.Unlock()
}

// RecordPutOperation records a buffer put operation with its latency
func (bpet *BufferPoolEfficiencyTracker) RecordPutOperation(latency time.Duration, bufferSize int) {
	atomic.AddInt64(&bpet.putOperations, 1)
	atomic.AddInt64(&bpet.putLatencySum, latency.Nanoseconds())
	atomic.AddInt64(&bpet.allocationBytes, int64(bufferSize))
}

// GetEfficiencyMetrics calculates current efficiency metrics
func (bpet *BufferPoolEfficiencyTracker) GetEfficiencyMetrics() BufferPoolEfficiencyMetrics {
	getOps := atomic.LoadInt64(&bpet.getOperations)
	putOps := atomic.LoadInt64(&bpet.putOperations)
	reuseCount := atomic.LoadInt64(&bpet.reuseCount)
	getLatencySum := atomic.LoadInt64(&bpet.getLatencySum)
	putLatencySum := atomic.LoadInt64(&bpet.putLatencySum)
	allocationBytes := atomic.LoadInt64(&bpet.allocationBytes)

	var hitRate, missRate, avgGetLatency, avgPutLatency float64
	var throughput float64

	if getOps > 0 {
		hitRate = float64(reuseCount) / float64(getOps) * 100
		missRate = 100 - hitRate
		avgGetLatency = float64(getLatencySum) / float64(getOps)
	}

	if putOps > 0 {
		avgPutLatency = float64(putLatencySum) / float64(putOps)
	}

	// Calculate throughput from recent operations
	bpet.opsMutex.RLock()
	if len(bpet.recentOps) > 1 {
		timeSpan := bpet.recentOps[len(bpet.recentOps)-1].Sub(bpet.recentOps[0])
		if timeSpan > 0 {
			throughput = float64(len(bpet.recentOps)) / timeSpan.Seconds()
		}
	}
	bpet.opsMutex.RUnlock()

	// Calculate efficiency metrics
	utilizationRate := hitRate  // Simplified: hit rate as utilization
	memoryEfficiency := hitRate // Simplified: reuse rate as memory efficiency
	reuseEffectiveness := hitRate

	// Calculate fragmentation (simplified as inverse of hit rate)
	fragmentationRate := missRate

	// Calculate allocation overhead (simplified)
	allocationOverhead := float64(0)
	if getOps > 0 && allocationBytes > 0 {
		allocationOverhead = float64(allocationBytes) / float64(getOps)
	}

	return BufferPoolEfficiencyMetrics{
		HitRate:            hitRate,
		MissRate:           missRate,
		UtilizationRate:    utilizationRate,
		FragmentationRate:  fragmentationRate,
		MemoryEfficiency:   memoryEfficiency,
		AllocationOverhead: allocationOverhead,
		ReuseEffectiveness: reuseEffectiveness,
		AverageGetLatency:  time.Duration(avgGetLatency),
		AveragePutLatency:  time.Duration(avgPutLatency),
		Throughput:         throughput,
	}
}

// NewGranularMetricsCollector creates a new granular metrics collector
func NewGranularMetricsCollector(logger zerolog.Logger) *GranularMetricsCollector {
	return &GranularMetricsCollector{
		framePoolMetrics:   NewBufferPoolEfficiencyTracker("frame_pool", logger.With().Str("pool", "frame").Logger()),
		controlPoolMetrics: NewBufferPoolEfficiencyTracker("control_pool", logger.With().Str("pool", "control").Logger()),
		zeroCopyMetrics:    NewBufferPoolEfficiencyTracker("zero_copy_pool", logger.With().Str("pool", "zero_copy").Logger()),
		logger:             logger,
	}
}

// RecordFramePoolOperation records frame pool operations
func (gmc *GranularMetricsCollector) RecordFramePoolGet(latency time.Duration, wasHit bool) {
	gmc.framePoolMetrics.RecordGetOperation(latency, wasHit)
}

func (gmc *GranularMetricsCollector) RecordFramePoolPut(latency time.Duration, bufferSize int) {
	gmc.framePoolMetrics.RecordPutOperation(latency, bufferSize)
}

// RecordControlPoolOperation records control pool operations
func (gmc *GranularMetricsCollector) RecordControlPoolGet(latency time.Duration, wasHit bool) {
	gmc.controlPoolMetrics.RecordGetOperation(latency, wasHit)
}

func (gmc *GranularMetricsCollector) RecordControlPoolPut(latency time.Duration, bufferSize int) {
	gmc.controlPoolMetrics.RecordPutOperation(latency, bufferSize)
}

// RecordZeroCopyOperation records zero-copy pool operations
func (gmc *GranularMetricsCollector) RecordZeroCopyGet(latency time.Duration, wasHit bool) {
	gmc.zeroCopyMetrics.RecordGetOperation(latency, wasHit)
}

func (gmc *GranularMetricsCollector) RecordZeroCopyPut(latency time.Duration, bufferSize int) {
	gmc.zeroCopyMetrics.RecordPutOperation(latency, bufferSize)
}

// GetBufferPoolEfficiency returns efficiency metrics for all buffer pools
func (gmc *GranularMetricsCollector) GetBufferPoolEfficiency() map[string]BufferPoolEfficiencyMetrics {
	gmc.mutex.RLock()
	defer gmc.mutex.RUnlock()

	return map[string]BufferPoolEfficiencyMetrics{
		"frame_pool":     gmc.framePoolMetrics.GetEfficiencyMetrics(),
		"control_pool":   gmc.controlPoolMetrics.GetEfficiencyMetrics(),
		"zero_copy_pool": gmc.zeroCopyMetrics.GetEfficiencyMetrics(),
	}
}

// LogGranularMetrics logs comprehensive granular metrics
func (gmc *GranularMetricsCollector) LogGranularMetrics() {
	bufferEfficiency := gmc.GetBufferPoolEfficiency()

	// Log buffer pool efficiency
	for poolName, efficiency := range bufferEfficiency {
		gmc.logger.Info().
			Str("pool", poolName).
			Float64("hit_rate", efficiency.HitRate).
			Float64("miss_rate", efficiency.MissRate).
			Float64("utilization_rate", efficiency.UtilizationRate).
			Float64("memory_efficiency", efficiency.MemoryEfficiency).
			Dur("avg_get_latency", efficiency.AverageGetLatency).
			Dur("avg_put_latency", efficiency.AveragePutLatency).
			Float64("throughput", efficiency.Throughput).
			Msg("Buffer pool efficiency metrics")
	}
}

// Global granular metrics collector instance
var (
	granularMetricsCollector *GranularMetricsCollector
	granularMetricsOnce      sync.Once
)

// GetGranularMetricsCollector returns the global granular metrics collector
func GetGranularMetricsCollector() *GranularMetricsCollector {
	granularMetricsOnce.Do(func() {
		logger := logging.GetDefaultLogger().With().Str("component", "granular-metrics").Logger()
		granularMetricsCollector = NewGranularMetricsCollector(logger)
	})
	return granularMetricsCollector
}

// StartGranularMetricsLogging starts periodic granular metrics logging
func StartGranularMetricsLogging(interval time.Duration) {
	collector := GetGranularMetricsCollector()
	logger := collector.logger

	logger.Info().Dur("interval", interval).Msg("Starting granular metrics logging")

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for range ticker.C {
			collector.LogGranularMetrics()
		}
	}()
}
