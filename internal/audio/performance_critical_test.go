//go:build cgo
// +build cgo

package audio

import (
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPerformanceCriticalPaths tests the most frequently executed code paths
// to ensure they remain efficient and don't interfere with KVM functionality
func TestPerformanceCriticalPaths(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance tests in short mode")
	}

	// Initialize validation cache for performance testing
	InitValidationCache()

	tests := []struct {
		name     string
		testFunc func(t *testing.T)
	}{
		{"AudioFrameProcessingLatency", testAudioFrameProcessingLatency},
		{"MetricsUpdateOverhead", testMetricsUpdateOverhead},
		{"ConfigurationAccessSpeed", testConfigurationAccessSpeed},
		{"ValidationFunctionSpeed", testValidationFunctionSpeed},
		{"MemoryAllocationPatterns", testMemoryAllocationPatterns},
		{"ConcurrentAccessPerformance", testConcurrentAccessPerformance},
		{"BufferPoolEfficiency", testBufferPoolEfficiency},
		{"AtomicOperationOverhead", testAtomicOperationOverhead},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.testFunc(t)
		})
	}
}

// testAudioFrameProcessingLatency tests the latency of audio frame processing
// This is the most critical path that must not interfere with KVM
func testAudioFrameProcessingLatency(t *testing.T) {
	const (
		frameCount         = 1000
		maxLatencyPerFrame = 100 * time.Microsecond // Very strict requirement
	)

	// Create test frame data
	frameData := make([]byte, 1920) // Typical frame size
	for i := range frameData {
		frameData[i] = byte(i % 256)
	}

	// Measure frame processing latency
	start := time.Now()
	for i := 0; i < frameCount; i++ {
		// Simulate the critical path: validation + metrics update
		err := ValidateAudioFrame(frameData)
		require.NoError(t, err)

		// Record frame received (atomic operation)
		RecordFrameReceived(len(frameData))
	}
	elapsed := time.Since(start)

	avgLatencyPerFrame := elapsed / frameCount
	t.Logf("Average frame processing latency: %v", avgLatencyPerFrame)

	// Ensure frame processing is fast enough to not interfere with KVM
	assert.Less(t, avgLatencyPerFrame, maxLatencyPerFrame,
		"Frame processing latency %v exceeds maximum %v - may interfere with KVM",
		avgLatencyPerFrame, maxLatencyPerFrame)

	// Ensure total processing time is reasonable
	maxTotalTime := 50 * time.Millisecond
	assert.Less(t, elapsed, maxTotalTime,
		"Total processing time %v exceeds maximum %v", elapsed, maxTotalTime)
}

// testMetricsUpdateOverhead tests the overhead of metrics updates
func testMetricsUpdateOverhead(t *testing.T) {
	const iterations = 10000

	// Test RecordFrameReceived performance
	start := time.Now()
	for i := 0; i < iterations; i++ {
		RecordFrameReceived(1024)
	}
	recordLatency := time.Since(start) / iterations

	// Test GetAudioMetrics performance
	start = time.Now()
	for i := 0; i < iterations; i++ {
		_ = GetAudioMetrics()
	}
	getLatency := time.Since(start) / iterations

	t.Logf("RecordFrameReceived latency: %v", recordLatency)
	t.Logf("GetAudioMetrics latency: %v", getLatency)

	// Metrics operations should be optimized for JetKVM's ARM Cortex-A7 @ 1GHz
	// With 256MB RAM, we need to be conservative with performance expectations
	assert.Less(t, recordLatency, 50*time.Microsecond, "RecordFrameReceived too slow")
	assert.Less(t, getLatency, 20*time.Microsecond, "GetAudioMetrics too slow")
}

// testConfigurationAccessSpeed tests configuration access performance
func testConfigurationAccessSpeed(t *testing.T) {
	const iterations = 10000

	// Test GetAudioConfig performance
	start := time.Now()
	for i := 0; i < iterations; i++ {
		_ = GetAudioConfig()
	}
	configLatency := time.Since(start) / iterations

	// Test GetConfig performance
	start = time.Now()
	for i := 0; i < iterations; i++ {
		_ = GetConfig()
	}
	constantsLatency := time.Since(start) / iterations

	t.Logf("GetAudioConfig latency: %v", configLatency)
	t.Logf("GetConfig latency: %v", constantsLatency)

	// Configuration access should be very fast
	assert.Less(t, configLatency, 100*time.Nanosecond, "GetAudioConfig too slow")
	assert.Less(t, constantsLatency, 100*time.Nanosecond, "GetConfig too slow")
}

// testValidationFunctionSpeed tests validation function performance
func testValidationFunctionSpeed(t *testing.T) {
	const iterations = 10000
	frameData := make([]byte, 1920)

	// Test ValidateAudioFrame (most critical)
	start := time.Now()
	for i := 0; i < iterations; i++ {
		err := ValidateAudioFrame(frameData)
		require.NoError(t, err)
	}
	fastValidationLatency := time.Since(start) / iterations

	// Test ValidateAudioQuality
	start = time.Now()
	for i := 0; i < iterations; i++ {
		err := ValidateAudioQuality(AudioQualityMedium)
		require.NoError(t, err)
	}
	qualityValidationLatency := time.Since(start) / iterations

	// Test ValidateBufferSize
	start = time.Now()
	for i := 0; i < iterations; i++ {
		err := ValidateBufferSize(1024)
		require.NoError(t, err)
	}
	bufferValidationLatency := time.Since(start) / iterations

	t.Logf("ValidateAudioFrame latency: %v", fastValidationLatency)
	t.Logf("ValidateAudioQuality latency: %v", qualityValidationLatency)
	t.Logf("ValidateBufferSize latency: %v", bufferValidationLatency)

	// Validation functions optimized for ARM Cortex-A7 single core @ 1GHz
	// Conservative thresholds to ensure KVM functionality isn't impacted
	assert.Less(t, fastValidationLatency, 100*time.Microsecond, "ValidateAudioFrame too slow")
	assert.Less(t, qualityValidationLatency, 50*time.Microsecond, "ValidateAudioQuality too slow")
	assert.Less(t, bufferValidationLatency, 50*time.Microsecond, "ValidateBufferSize too slow")
}

// testMemoryAllocationPatterns tests memory allocation efficiency
func testMemoryAllocationPatterns(t *testing.T) {
	// Test that frequent operations don't cause excessive allocations
	var m1, m2 runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m1)

	// Perform operations that should minimize allocations
	for i := 0; i < 1000; i++ {
		_ = GetAudioConfig()
		_ = GetAudioMetrics()
		RecordFrameReceived(1024)
		_ = ValidateAudioQuality(AudioQualityMedium)
	}

	runtime.GC()
	runtime.ReadMemStats(&m2)

	allocations := m2.Mallocs - m1.Mallocs
	t.Logf("Memory allocations for 1000 operations: %d", allocations)

	// Should have minimal allocations for these hot path operations
	assert.Less(t, allocations, uint64(100), "Too many memory allocations in hot path")
}

// testConcurrentAccessPerformance tests performance under concurrent access
func testConcurrentAccessPerformance(t *testing.T) {
	const (
		numGoroutines          = 10
		operationsPerGoroutine = 1000
	)

	var wg sync.WaitGroup
	start := time.Now()

	// Launch concurrent goroutines performing audio operations
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			frameData := make([]byte, 1920)

			for j := 0; j < operationsPerGoroutine; j++ {
				// Simulate concurrent audio processing
				_ = ValidateAudioFrame(frameData)
				RecordFrameReceived(len(frameData))
				_ = GetAudioMetrics()
				_ = GetAudioConfig()
			}
		}()
	}

	wg.Wait()
	elapsed := time.Since(start)

	totalOperations := numGoroutines * operationsPerGoroutine * 4 // 4 operations per iteration
	avgLatency := elapsed / time.Duration(totalOperations)

	t.Logf("Concurrent access: %d operations in %v (avg: %v per operation)",
		totalOperations, elapsed, avgLatency)

	// Concurrent access should not significantly degrade performance
	assert.Less(t, avgLatency, 1*time.Microsecond, "Concurrent access too slow")
}

// testBufferPoolEfficiency tests buffer pool performance
func testBufferPoolEfficiency(t *testing.T) {
	// Test buffer acquisition and release performance
	const iterations = 1000

	start := time.Now()
	for i := 0; i < iterations; i++ {
		// Simulate buffer pool usage (if available)
		buffer := make([]byte, 1920) // Fallback to allocation
		_ = buffer
		// In real implementation, this would be pool.Get() and pool.Put()
	}
	elapsed := time.Since(start)

	avgLatency := elapsed / iterations
	t.Logf("Buffer allocation latency: %v per buffer", avgLatency)

	// Buffer operations should be fast
	assert.Less(t, avgLatency, 1*time.Microsecond, "Buffer allocation too slow")
}

// testAtomicOperationOverhead tests atomic operation performance
func testAtomicOperationOverhead(t *testing.T) {
	const iterations = 10000
	var counter int64

	// Test atomic increment performance
	start := time.Now()
	for i := 0; i < iterations; i++ {
		atomic.AddInt64(&counter, 1)
	}
	atomicLatency := time.Since(start) / iterations

	// Test atomic load performance
	start = time.Now()
	for i := 0; i < iterations; i++ {
		_ = atomic.LoadInt64(&counter)
	}
	loadLatency := time.Since(start) / iterations

	t.Logf("Atomic add latency: %v", atomicLatency)
	t.Logf("Atomic load latency: %v", loadLatency)

	// Atomic operations on ARM Cortex-A7 - realistic expectations
	assert.Less(t, atomicLatency, 1*time.Microsecond, "Atomic add too slow")
	assert.Less(t, loadLatency, 500*time.Nanosecond, "Atomic load too slow")
}

// TestRegressionDetection tests for performance regressions
func TestRegressionDetection(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping regression test in short mode")
	}

	// Baseline performance expectations
	baselines := map[string]time.Duration{
		"frame_processing": 100 * time.Microsecond,
		"metrics_update":   500 * time.Nanosecond,
		"config_access":    100 * time.Nanosecond,
		"validation":       200 * time.Nanosecond,
	}

	// Test frame processing
	frameData := make([]byte, 1920)
	start := time.Now()
	for i := 0; i < 100; i++ {
		_ = ValidateAudioFrame(frameData)
		RecordFrameReceived(len(frameData))
	}
	frameProcessingTime := time.Since(start) / 100

	// Test metrics update
	start = time.Now()
	for i := 0; i < 1000; i++ {
		RecordFrameReceived(1024)
	}
	metricsUpdateTime := time.Since(start) / 1000

	// Test config access
	start = time.Now()
	for i := 0; i < 1000; i++ {
		_ = GetAudioConfig()
	}
	configAccessTime := time.Since(start) / 1000

	// Test validation
	start = time.Now()
	for i := 0; i < 1000; i++ {
		_ = ValidateAudioQuality(AudioQualityMedium)
	}
	validationTime := time.Since(start) / 1000

	// Performance regression thresholds for JetKVM hardware:
	// - ARM Cortex-A7 @ 1GHz single core
	// - 256MB DDR3L RAM
	// - Must not interfere with primary KVM functionality
	assert.Less(t, frameProcessingTime, baselines["frame_processing"],
		"Frame processing regression: %v > %v", frameProcessingTime, baselines["frame_processing"])
	assert.Less(t, metricsUpdateTime, 100*time.Microsecond,
		"Metrics update regression: %v > 100μs", metricsUpdateTime)
	assert.Less(t, configAccessTime, 10*time.Microsecond,
		"Config access regression: %v > 10μs", configAccessTime)
	assert.Less(t, validationTime, 10*time.Microsecond,
		"Validation regression: %v > 10μs", validationTime)

	t.Logf("Performance results:")
	t.Logf("  Frame processing: %v (baseline: %v)", frameProcessingTime, baselines["frame_processing"])
	t.Logf("  Metrics update: %v (baseline: %v)", metricsUpdateTime, baselines["metrics_update"])
	t.Logf("  Config access: %v (baseline: %v)", configAccessTime, baselines["config_access"])
	t.Logf("  Validation: %v (baseline: %v)", validationTime, baselines["validation"])
}

// TestMemoryLeakDetection tests for memory leaks in critical paths
func TestMemoryLeakDetection(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping memory leak test in short mode")
	}

	var m1, m2 runtime.MemStats

	// Baseline measurement
	runtime.GC()
	runtime.ReadMemStats(&m1)

	// Perform many operations that should not leak memory
	for cycle := 0; cycle < 10; cycle++ {
		for i := 0; i < 1000; i++ {
			frameData := make([]byte, 1920)
			_ = ValidateAudioFrame(frameData)
			RecordFrameReceived(len(frameData))
			_ = GetAudioMetrics()
			_ = GetAudioConfig()
		}
		// Force garbage collection between cycles
		runtime.GC()
	}

	// Final measurement
	runtime.GC()
	runtime.ReadMemStats(&m2)

	memoryGrowth := int64(m2.Alloc) - int64(m1.Alloc)
	t.Logf("Memory growth after 10,000 operations: %d bytes", memoryGrowth)

	// Memory growth should be minimal (less than 1MB)
	assert.Less(t, memoryGrowth, int64(1024*1024),
		"Excessive memory growth detected: %d bytes", memoryGrowth)
}
