package audio

import (
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jetkvm/kvm/internal/logging"
	"github.com/rs/zerolog"
)

// LatencyHistogram tracks latency distribution with percentile calculations
type LatencyHistogram struct {
	// Atomic fields MUST be first for ARM32 alignment
	sampleCount  int64 // Total number of samples (atomic)
	totalLatency int64 // Sum of all latencies in nanoseconds (atomic)

	// Latency buckets for histogram (in nanoseconds)
	buckets []int64 // Bucket boundaries
	counts  []int64 // Count for each bucket (atomic)

	// Recent samples for percentile calculation
	recentSamples []time.Duration
	samplesMutex  sync.RWMutex
	maxSamples    int

	logger zerolog.Logger
}

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
	// Latency histograms by source
	inputLatencyHist      *LatencyHistogram
	outputLatencyHist     *LatencyHistogram
	processingLatencyHist *LatencyHistogram

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

// NewLatencyHistogram creates a new latency histogram with predefined buckets
func NewLatencyHistogram(maxSamples int, logger zerolog.Logger) *LatencyHistogram {
	// Define latency buckets using configuration constants
	buckets := []int64{
		int64(1 * time.Millisecond),
		int64(5 * time.Millisecond),
		int64(GetConfig().LatencyBucket10ms),
		int64(GetConfig().LatencyBucket25ms),
		int64(GetConfig().LatencyBucket50ms),
		int64(GetConfig().LatencyBucket100ms),
		int64(GetConfig().LatencyBucket250ms),
		int64(GetConfig().LatencyBucket500ms),
		int64(GetConfig().LatencyBucket1s),
		int64(GetConfig().LatencyBucket2s),
	}

	return &LatencyHistogram{
		buckets:       buckets,
		counts:        make([]int64, len(buckets)+1), // +1 for overflow bucket
		recentSamples: make([]time.Duration, 0, maxSamples),
		maxSamples:    maxSamples,
		logger:        logger,
	}
}

// RecordLatency adds a latency measurement to the histogram
func (lh *LatencyHistogram) RecordLatency(latency time.Duration) {
	latencyNs := latency.Nanoseconds()
	atomic.AddInt64(&lh.sampleCount, 1)
	atomic.AddInt64(&lh.totalLatency, latencyNs)

	// Find appropriate bucket
	bucketIndex := len(lh.buckets) // Default to overflow bucket
	for i, boundary := range lh.buckets {
		if latencyNs <= boundary {
			bucketIndex = i
			break
		}
	}
	atomic.AddInt64(&lh.counts[bucketIndex], 1)

	// Store recent sample for percentile calculation
	lh.samplesMutex.Lock()
	if len(lh.recentSamples) >= lh.maxSamples {
		// Remove oldest sample
		lh.recentSamples = lh.recentSamples[1:]
	}
	lh.recentSamples = append(lh.recentSamples, latency)
	lh.samplesMutex.Unlock()
}

// LatencyHistogramData represents histogram data for WebSocket transmission
type LatencyHistogramData struct {
	Buckets []float64 `json:"buckets"` // Bucket boundaries in milliseconds
	Counts  []int64   `json:"counts"`  // Count for each bucket
}

// GetHistogramData returns histogram buckets and counts for WebSocket transmission
func (lh *LatencyHistogram) GetHistogramData() LatencyHistogramData {
	// Convert bucket boundaries from nanoseconds to milliseconds
	buckets := make([]float64, len(lh.buckets))
	for i, bucket := range lh.buckets {
		buckets[i] = float64(bucket) / 1e6 // Convert ns to ms
	}

	// Get current counts atomically
	counts := make([]int64, len(lh.counts))
	for i := range lh.counts {
		counts[i] = atomic.LoadInt64(&lh.counts[i])
	}

	return LatencyHistogramData{
		Buckets: buckets,
		Counts:  counts,
	}
}

// GetPercentiles calculates latency percentiles from recent samples
func (lh *LatencyHistogram) GetPercentiles() LatencyPercentiles {
	lh.samplesMutex.RLock()
	samples := make([]time.Duration, len(lh.recentSamples))
	copy(samples, lh.recentSamples)
	lh.samplesMutex.RUnlock()

	if len(samples) == 0 {
		return LatencyPercentiles{}
	}

	// Sort samples for percentile calculation
	sort.Slice(samples, func(i, j int) bool {
		return samples[i] < samples[j]
	})

	n := len(samples)
	totalLatency := atomic.LoadInt64(&lh.totalLatency)
	sampleCount := atomic.LoadInt64(&lh.sampleCount)

	var avg time.Duration
	if sampleCount > 0 {
		avg = time.Duration(totalLatency / sampleCount)
	}

	return LatencyPercentiles{
		P50: samples[n*50/100],
		P95: samples[n*95/100],
		P99: samples[n*99/100],
		Min: samples[0],
		Max: samples[n-1],
		Avg: avg,
	}
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
	maxSamples := GetConfig().LatencyHistorySize

	return &GranularMetricsCollector{
		inputLatencyHist:      NewLatencyHistogram(maxSamples, logger.With().Str("histogram", "input").Logger()),
		outputLatencyHist:     NewLatencyHistogram(maxSamples, logger.With().Str("histogram", "output").Logger()),
		processingLatencyHist: NewLatencyHistogram(maxSamples, logger.With().Str("histogram", "processing").Logger()),
		framePoolMetrics:      NewBufferPoolEfficiencyTracker("frame_pool", logger.With().Str("pool", "frame").Logger()),
		controlPoolMetrics:    NewBufferPoolEfficiencyTracker("control_pool", logger.With().Str("pool", "control").Logger()),
		zeroCopyMetrics:       NewBufferPoolEfficiencyTracker("zero_copy_pool", logger.With().Str("pool", "zero_copy").Logger()),
		logger:                logger,
	}
}

// RecordInputLatency records latency for input operations
func (gmc *GranularMetricsCollector) RecordInputLatency(latency time.Duration) {
	gmc.inputLatencyHist.RecordLatency(latency)
}

// RecordOutputLatency records latency for output operations
func (gmc *GranularMetricsCollector) RecordOutputLatency(latency time.Duration) {
	gmc.outputLatencyHist.RecordLatency(latency)
}

// RecordProcessingLatency records latency for processing operations
func (gmc *GranularMetricsCollector) RecordProcessingLatency(latency time.Duration) {
	gmc.processingLatencyHist.RecordLatency(latency)
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

// GetLatencyPercentiles returns percentiles for all latency types
func (gmc *GranularMetricsCollector) GetLatencyPercentiles() map[string]LatencyPercentiles {
	gmc.mutex.RLock()
	defer gmc.mutex.RUnlock()

	return map[string]LatencyPercentiles{
		"input":      gmc.inputLatencyHist.GetPercentiles(),
		"output":     gmc.outputLatencyHist.GetPercentiles(),
		"processing": gmc.processingLatencyHist.GetPercentiles(),
	}
}

// GetInputLatencyHistogram returns histogram data for input latency
func (gmc *GranularMetricsCollector) GetInputLatencyHistogram() *LatencyHistogramData {
	gmc.mutex.RLock()
	defer gmc.mutex.RUnlock()

	if gmc.inputLatencyHist == nil {
		return nil
	}

	data := gmc.inputLatencyHist.GetHistogramData()
	return &data
}

// GetOutputLatencyHistogram returns histogram data for output latency
func (gmc *GranularMetricsCollector) GetOutputLatencyHistogram() *LatencyHistogramData {
	gmc.mutex.RLock()
	defer gmc.mutex.RUnlock()

	if gmc.outputLatencyHist == nil {
		return nil
	}

	data := gmc.outputLatencyHist.GetHistogramData()
	return &data
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
	latencyPercentiles := gmc.GetLatencyPercentiles()
	bufferEfficiency := gmc.GetBufferPoolEfficiency()

	// Log latency percentiles
	for source, percentiles := range latencyPercentiles {
		gmc.logger.Info().
			Str("source", source).
			Dur("p50", percentiles.P50).
			Dur("p95", percentiles.P95).
			Dur("p99", percentiles.P99).
			Dur("min", percentiles.Min).
			Dur("max", percentiles.Max).
			Dur("avg", percentiles.Avg).
			Msg("Latency percentiles")
	}

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
