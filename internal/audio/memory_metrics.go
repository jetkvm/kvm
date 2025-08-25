package audio

import (
	"encoding/json"
	"net/http"
	"runtime"
	"time"

	"github.com/jetkvm/kvm/internal/logging"
	"github.com/rs/zerolog"
)

// MemoryMetrics provides comprehensive memory allocation statistics
type MemoryMetrics struct {
	// Runtime memory statistics
	RuntimeStats RuntimeMemoryStats `json:"runtime_stats"`
	// Audio buffer pool statistics
	BufferPools AudioBufferPoolStats `json:"buffer_pools"`
	// Zero-copy frame pool statistics
	ZeroCopyPool ZeroCopyFramePoolStats `json:"zero_copy_pool"`
	// Message pool statistics
	MessagePool MessagePoolStats `json:"message_pool"`
	// Batch processor statistics
	BatchProcessor BatchProcessorMemoryStats `json:"batch_processor,omitempty"`
	// Collection timestamp
	Timestamp time.Time `json:"timestamp"`
}

// RuntimeMemoryStats provides Go runtime memory statistics
type RuntimeMemoryStats struct {
	Alloc         uint64  `json:"alloc"`           // Bytes allocated and not yet freed
	TotalAlloc    uint64  `json:"total_alloc"`     // Total bytes allocated (cumulative)
	Sys           uint64  `json:"sys"`             // Total bytes obtained from OS
	Lookups       uint64  `json:"lookups"`         // Number of pointer lookups
	Mallocs       uint64  `json:"mallocs"`         // Number of mallocs
	Frees         uint64  `json:"frees"`           // Number of frees
	HeapAlloc     uint64  `json:"heap_alloc"`      // Bytes allocated and not yet freed (heap)
	HeapSys       uint64  `json:"heap_sys"`        // Bytes obtained from OS for heap
	HeapIdle      uint64  `json:"heap_idle"`       // Bytes in idle spans
	HeapInuse     uint64  `json:"heap_inuse"`      // Bytes in non-idle spans
	HeapReleased  uint64  `json:"heap_released"`   // Bytes released to OS
	HeapObjects   uint64  `json:"heap_objects"`    // Total number of allocated objects
	StackInuse    uint64  `json:"stack_inuse"`     // Bytes used by stack spans
	StackSys      uint64  `json:"stack_sys"`       // Bytes obtained from OS for stack
	MSpanInuse    uint64  `json:"mspan_inuse"`     // Bytes used by mspan structures
	MSpanSys      uint64  `json:"mspan_sys"`       // Bytes obtained from OS for mspan
	MCacheInuse   uint64  `json:"mcache_inuse"`    // Bytes used by mcache structures
	MCacheSys     uint64  `json:"mcache_sys"`      // Bytes obtained from OS for mcache
	BuckHashSys   uint64  `json:"buck_hash_sys"`   // Bytes used by profiling bucket hash table
	GCSys         uint64  `json:"gc_sys"`          // Bytes used for garbage collection metadata
	OtherSys      uint64  `json:"other_sys"`       // Bytes used for other system allocations
	NextGC        uint64  `json:"next_gc"`         // Target heap size for next GC
	LastGC        uint64  `json:"last_gc"`         // Time of last GC (nanoseconds since epoch)
	PauseTotalNs  uint64  `json:"pause_total_ns"`  // Total GC pause time
	NumGC         uint32  `json:"num_gc"`          // Number of completed GC cycles
	NumForcedGC   uint32  `json:"num_forced_gc"`   // Number of forced GC cycles
	GCCPUFraction float64 `json:"gc_cpu_fraction"` // Fraction of CPU time used by GC
}

// BatchProcessorMemoryStats provides batch processor memory statistics
type BatchProcessorMemoryStats struct {
	Initialized bool                         `json:"initialized"`
	Running     bool                         `json:"running"`
	Stats       BatchAudioStats              `json:"stats"`
	BufferPool  AudioBufferPoolDetailedStats `json:"buffer_pool,omitempty"`
}

// GetBatchAudioProcessor is defined in batch_audio.go
// BatchAudioStats is defined in batch_audio.go

var memoryMetricsLogger *zerolog.Logger

func getMemoryMetricsLogger() *zerolog.Logger {
	if memoryMetricsLogger == nil {
		logger := logging.GetDefaultLogger().With().Str("component", "memory-metrics").Logger()
		memoryMetricsLogger = &logger
	}
	return memoryMetricsLogger
}

// CollectMemoryMetrics gathers comprehensive memory allocation statistics
func CollectMemoryMetrics() MemoryMetrics {
	// Collect runtime memory statistics
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	runtimeStats := RuntimeMemoryStats{
		Alloc:         m.Alloc,
		TotalAlloc:    m.TotalAlloc,
		Sys:           m.Sys,
		Lookups:       m.Lookups,
		Mallocs:       m.Mallocs,
		Frees:         m.Frees,
		HeapAlloc:     m.HeapAlloc,
		HeapSys:       m.HeapSys,
		HeapIdle:      m.HeapIdle,
		HeapInuse:     m.HeapInuse,
		HeapReleased:  m.HeapReleased,
		HeapObjects:   m.HeapObjects,
		StackInuse:    m.StackInuse,
		StackSys:      m.StackSys,
		MSpanInuse:    m.MSpanInuse,
		MSpanSys:      m.MSpanSys,
		MCacheInuse:   m.MCacheInuse,
		MCacheSys:     m.MCacheSys,
		BuckHashSys:   m.BuckHashSys,
		GCSys:         m.GCSys,
		OtherSys:      m.OtherSys,
		NextGC:        m.NextGC,
		LastGC:        m.LastGC,
		PauseTotalNs:  m.PauseTotalNs,
		NumGC:         m.NumGC,
		NumForcedGC:   m.NumForcedGC,
		GCCPUFraction: m.GCCPUFraction,
	}

	// Collect audio buffer pool statistics
	bufferPoolStats := GetAudioBufferPoolStats()

	// Collect zero-copy frame pool statistics
	zeroCopyStats := GetGlobalZeroCopyPoolStats()

	// Collect message pool statistics
	messagePoolStats := GetGlobalMessagePoolStats()

	// Collect batch processor statistics if available
	var batchStats BatchProcessorMemoryStats
	if processor := GetBatchAudioProcessor(); processor != nil {
		batchStats.Initialized = true
		batchStats.Running = processor.IsRunning()
		batchStats.Stats = processor.GetStats()
		// Note: BatchAudioProcessor uses sync.Pool, detailed stats not available
	}

	return MemoryMetrics{
		RuntimeStats:   runtimeStats,
		BufferPools:    bufferPoolStats,
		ZeroCopyPool:   zeroCopyStats,
		MessagePool:    messagePoolStats,
		BatchProcessor: batchStats,
		Timestamp:      time.Now(),
	}
}

// HandleMemoryMetrics provides an HTTP handler for memory metrics
func HandleMemoryMetrics(w http.ResponseWriter, r *http.Request) {
	logger := getMemoryMetricsLogger()

	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	metrics := CollectMemoryMetrics()

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")

	if err := json.NewEncoder(w).Encode(metrics); err != nil {
		logger.Error().Err(err).Msg("failed to encode memory metrics")
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	logger.Debug().Msg("memory metrics served")
}

// LogMemoryMetrics logs current memory metrics for debugging
func LogMemoryMetrics() {
	logger := getMemoryMetricsLogger()
	metrics := CollectMemoryMetrics()

	logger.Info().
		Uint64("heap_alloc_mb", metrics.RuntimeStats.HeapAlloc/uint64(GetConfig().BytesToMBDivisor)).
		Uint64("heap_sys_mb", metrics.RuntimeStats.HeapSys/uint64(GetConfig().BytesToMBDivisor)).
		Uint64("heap_objects", metrics.RuntimeStats.HeapObjects).
		Uint32("num_gc", metrics.RuntimeStats.NumGC).
		Float64("gc_cpu_fraction", metrics.RuntimeStats.GCCPUFraction).
		Float64("buffer_pool_hit_rate", metrics.BufferPools.FramePoolHitRate).
		Float64("zero_copy_hit_rate", metrics.ZeroCopyPool.HitRate).
		Float64("message_pool_hit_rate", metrics.MessagePool.HitRate).
		Msg("memory metrics snapshot")
}

// StartMemoryMetricsLogging starts periodic memory metrics logging
func StartMemoryMetricsLogging(interval time.Duration) {
	logger := getMemoryMetricsLogger()
	logger.Info().Dur("interval", interval).Msg("starting memory metrics logging")

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for range ticker.C {
			LogMemoryMetrics()
		}
	}()
}
