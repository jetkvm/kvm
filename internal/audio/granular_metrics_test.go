package audio

import (
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLatencyHistogram tests the LatencyHistogram functionality
func TestLatencyHistogram(t *testing.T) {
	tests := []struct {
		name     string
		testFunc func(t *testing.T)
	}{
		{"NewLatencyHistogram", testNewLatencyHistogram},
		{"RecordLatency", testRecordLatency},
		{"GetHistogramData", testGetHistogramData},
		{"GetPercentiles", testGetPercentiles},
		{"ConcurrentAccess", testLatencyHistogramConcurrentAccess},
		{"BucketDistribution", testBucketDistribution},
		{"OverflowBucket", testOverflowBucket},
		{"RecentSamplesLimit", testRecentSamplesLimit},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.testFunc(t)
		})
	}
}

// testNewLatencyHistogram tests LatencyHistogram creation
func testNewLatencyHistogram(t *testing.T) {
	logger := zerolog.Nop()
	maxSamples := 100

	hist := NewLatencyHistogram(maxSamples, logger)

	require.NotNil(t, hist)
	assert.Equal(t, maxSamples, hist.maxSamples)
	assert.NotNil(t, hist.buckets)
	assert.NotNil(t, hist.counts)
	assert.Equal(t, len(hist.buckets)+1, len(hist.counts)) // +1 for overflow bucket
	assert.NotNil(t, hist.recentSamples)
	assert.Equal(t, 0, len(hist.recentSamples))
}

// testRecordLatency tests latency recording functionality
func testRecordLatency(t *testing.T) {
	logger := zerolog.Nop()
	hist := NewLatencyHistogram(100, logger)

	// Test recording various latencies
	latencies := []time.Duration{
		500 * time.Microsecond, // Should go in first bucket (1ms)
		3 * time.Millisecond,   // Should go in second bucket (5ms)
		15 * time.Millisecond,  // Should go in third bucket (10ms)
		100 * time.Millisecond, // Should go in appropriate bucket
		3 * time.Second,        // Should go in overflow bucket
	}

	for _, latency := range latencies {
		hist.RecordLatency(latency)
	}

	// Verify sample count
	assert.Equal(t, int64(len(latencies)), hist.sampleCount)

	// Verify total latency is accumulated
	expectedTotal := int64(0)
	for _, latency := range latencies {
		expectedTotal += latency.Nanoseconds()
	}
	assert.Equal(t, expectedTotal, hist.totalLatency)

	// Verify recent samples are stored
	assert.Equal(t, len(latencies), len(hist.recentSamples))
}

// testGetHistogramData tests histogram data retrieval
func testGetHistogramData(t *testing.T) {
	logger := zerolog.Nop()
	hist := NewLatencyHistogram(100, logger)

	// Record some test latencies
	hist.RecordLatency(500 * time.Microsecond)
	hist.RecordLatency(3 * time.Millisecond)
	hist.RecordLatency(15 * time.Millisecond)
	hist.RecordLatency(3 * time.Second) // overflow

	data := hist.GetHistogramData()

	// Verify buckets are converted to milliseconds
	require.NotNil(t, data.Buckets)
	require.NotNil(t, data.Counts)
	assert.Equal(t, len(hist.buckets), len(data.Buckets))
	assert.Equal(t, len(hist.counts), len(data.Counts))

	// Verify first bucket is 1ms
	assert.Equal(t, 1.0, data.Buckets[0])

	// Verify counts are non-negative
	for i, count := range data.Counts {
		assert.GreaterOrEqual(t, count, int64(0), "Count at index %d should be non-negative", i)
	}

	// Verify total counts match recorded samples
	totalCounts := int64(0)
	for _, count := range data.Counts {
		totalCounts += count
	}
	assert.Equal(t, int64(4), totalCounts)
}

// testGetPercentiles tests percentile calculation
func testGetPercentiles(t *testing.T) {
	logger := zerolog.Nop()
	hist := NewLatencyHistogram(100, logger)

	// Record a known set of latencies
	latencies := []time.Duration{
		1 * time.Millisecond,
		2 * time.Millisecond,
		3 * time.Millisecond,
		4 * time.Millisecond,
		5 * time.Millisecond,
		10 * time.Millisecond,
		20 * time.Millisecond,
		50 * time.Millisecond,
		100 * time.Millisecond,
		200 * time.Millisecond,
	}

	for _, latency := range latencies {
		hist.RecordLatency(latency)
	}

	percentiles := hist.GetPercentiles()

	// Verify percentiles are calculated
	assert.Greater(t, percentiles.P50, time.Duration(0))
	assert.Greater(t, percentiles.P95, time.Duration(0))
	assert.Greater(t, percentiles.P99, time.Duration(0))
	assert.Greater(t, percentiles.Min, time.Duration(0))
	assert.Greater(t, percentiles.Max, time.Duration(0))
	assert.Greater(t, percentiles.Avg, time.Duration(0))

	// Verify ordering: Min <= P50 <= P95 <= P99 <= Max
	assert.LessOrEqual(t, percentiles.Min, percentiles.P50)
	assert.LessOrEqual(t, percentiles.P50, percentiles.P95)
	assert.LessOrEqual(t, percentiles.P95, percentiles.P99)
	assert.LessOrEqual(t, percentiles.P99, percentiles.Max)

	// Verify min and max are correct
	assert.Equal(t, 1*time.Millisecond, percentiles.Min)
	assert.Equal(t, 200*time.Millisecond, percentiles.Max)
}

// testLatencyHistogramConcurrentAccess tests thread safety
func testLatencyHistogramConcurrentAccess(t *testing.T) {
	logger := zerolog.Nop()
	hist := NewLatencyHistogram(1000, logger)

	const numGoroutines = 10
	const samplesPerGoroutine = 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// Concurrent writers
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < samplesPerGoroutine; j++ {
				latency := time.Duration(id*j+1) * time.Microsecond
				hist.RecordLatency(latency)
			}
		}(i)
	}

	// Concurrent readers
	for i := 0; i < 5; i++ {
		go func() {
			for j := 0; j < 50; j++ {
				_ = hist.GetHistogramData()
				_ = hist.GetPercentiles()
				time.Sleep(time.Microsecond)
			}
		}()
	}

	wg.Wait()

	// Verify final state
	assert.Equal(t, int64(numGoroutines*samplesPerGoroutine), hist.sampleCount)
	data := hist.GetHistogramData()
	assert.NotNil(t, data)
}

// testBucketDistribution tests that latencies are distributed correctly across buckets
func testBucketDistribution(t *testing.T) {
	logger := zerolog.Nop()
	hist := NewLatencyHistogram(100, logger)

	// Record latencies that should go into specific buckets
	testCases := []struct {
		latency        time.Duration
		expectedBucket int
	}{
		{500 * time.Microsecond, 0}, // < 1ms
		{3 * time.Millisecond, 1},   // < 5ms
		{8 * time.Millisecond, 2},   // < 10ms (assuming 10ms is bucket 2)
	}

	for _, tc := range testCases {
		hist.RecordLatency(tc.latency)
	}

	data := hist.GetHistogramData()

	// Verify that counts are in expected buckets
	for i, tc := range testCases {
		if tc.expectedBucket < len(data.Counts) {
			assert.GreaterOrEqual(t, data.Counts[tc.expectedBucket], int64(1),
				"Test case %d: Expected bucket %d to have at least 1 count", i, tc.expectedBucket)
		}
	}
}

// testOverflowBucket tests the overflow bucket functionality
func testOverflowBucket(t *testing.T) {
	logger := zerolog.Nop()
	hist := NewLatencyHistogram(100, logger)

	// Record a latency that should go into overflow bucket
	veryHighLatency := 10 * time.Second
	hist.RecordLatency(veryHighLatency)

	data := hist.GetHistogramData()

	// Verify overflow bucket (last bucket) has the count
	overflowBucketIndex := len(data.Counts) - 1
	assert.Equal(t, int64(1), data.Counts[overflowBucketIndex])

	// Verify other buckets are empty
	for i := 0; i < overflowBucketIndex; i++ {
		assert.Equal(t, int64(0), data.Counts[i], "Bucket %d should be empty", i)
	}
}

// testRecentSamplesLimit tests that recent samples are limited correctly
func testRecentSamplesLimit(t *testing.T) {
	logger := zerolog.Nop()
	maxSamples := 5
	hist := NewLatencyHistogram(maxSamples, logger)

	// Record more samples than the limit
	for i := 0; i < maxSamples*2; i++ {
		hist.RecordLatency(time.Duration(i+1) * time.Millisecond)
	}

	// Verify recent samples are limited
	hist.samplesMutex.RLock()
	assert.Equal(t, maxSamples, len(hist.recentSamples))
	hist.samplesMutex.RUnlock()

	// Verify total sample count is still correct
	assert.Equal(t, int64(maxSamples*2), hist.sampleCount)
}

// TestGranularMetricsCollector tests the GranularMetricsCollector functionality
func TestGranularMetricsCollector(t *testing.T) {
	tests := []struct {
		name     string
		testFunc func(t *testing.T)
	}{
		{"GetGranularMetricsCollector", testGetGranularMetricsCollector},
		{"RecordInputLatency", testRecordInputLatency},
		{"RecordOutputLatency", testRecordOutputLatency},
		{"GetInputLatencyHistogram", testGetInputLatencyHistogram},
		{"GetOutputLatencyHistogram", testGetOutputLatencyHistogram},
		{"GetLatencyPercentiles", testGetLatencyPercentiles},
		{"ConcurrentCollectorAccess", testConcurrentCollectorAccess},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.testFunc(t)
		})
	}
}

// testGetGranularMetricsCollector tests singleton behavior
func testGetGranularMetricsCollector(t *testing.T) {
	collector1 := GetGranularMetricsCollector()
	collector2 := GetGranularMetricsCollector()

	require.NotNil(t, collector1)
	require.NotNil(t, collector2)
	assert.Same(t, collector1, collector2, "Should return the same singleton instance")
}

// testRecordInputLatency tests input latency recording
func testRecordInputLatency(t *testing.T) {
	collector := GetGranularMetricsCollector()
	require.NotNil(t, collector)

	testLatency := 5 * time.Millisecond
	collector.RecordInputLatency(testLatency)

	// Verify histogram data is available
	histData := collector.GetInputLatencyHistogram()
	require.NotNil(t, histData)
	assert.NotNil(t, histData.Buckets)
	assert.NotNil(t, histData.Counts)

	// Verify at least one count is recorded
	totalCounts := int64(0)
	for _, count := range histData.Counts {
		totalCounts += count
	}
	assert.Equal(t, int64(1), totalCounts)
}

// testRecordOutputLatency tests output latency recording
func testRecordOutputLatency(t *testing.T) {
	collector := GetGranularMetricsCollector()
	require.NotNil(t, collector)

	testLatency := 10 * time.Millisecond
	collector.RecordOutputLatency(testLatency)

	// Verify histogram data is available
	histData := collector.GetOutputLatencyHistogram()
	require.NotNil(t, histData)
	assert.NotNil(t, histData.Buckets)
	assert.NotNil(t, histData.Counts)

	// Verify at least one count is recorded
	totalCounts := int64(0)
	for _, count := range histData.Counts {
		totalCounts += count
	}
	assert.Equal(t, int64(1), totalCounts)
}

// testGetInputLatencyHistogram tests input histogram retrieval
func testGetInputLatencyHistogram(t *testing.T) {
	collector := GetGranularMetricsCollector()
	require.NotNil(t, collector)

	// Test when no data is recorded
	histData := collector.GetInputLatencyHistogram()
	if histData != nil {
		assert.NotNil(t, histData.Buckets)
		assert.NotNil(t, histData.Counts)
	}

	// Record some data and test again
	collector.RecordInputLatency(2 * time.Millisecond)
	histData = collector.GetInputLatencyHistogram()
	require.NotNil(t, histData)
	assert.NotNil(t, histData.Buckets)
	assert.NotNil(t, histData.Counts)
}

// testGetOutputLatencyHistogram tests output histogram retrieval
func testGetOutputLatencyHistogram(t *testing.T) {
	collector := GetGranularMetricsCollector()
	require.NotNil(t, collector)

	// Test when no data is recorded
	histData := collector.GetOutputLatencyHistogram()
	if histData != nil {
		assert.NotNil(t, histData.Buckets)
		assert.NotNil(t, histData.Counts)
	}

	// Record some data and test again
	collector.RecordOutputLatency(7 * time.Millisecond)
	histData = collector.GetOutputLatencyHistogram()
	require.NotNil(t, histData)
	assert.NotNil(t, histData.Buckets)
	assert.NotNil(t, histData.Counts)
}

// testGetLatencyPercentiles tests percentile retrieval from collector
func testGetLatencyPercentiles(t *testing.T) {
	collector := GetGranularMetricsCollector()
	require.NotNil(t, collector)

	// Record some test data
	latencies := []time.Duration{
		1 * time.Millisecond,
		5 * time.Millisecond,
		10 * time.Millisecond,
		20 * time.Millisecond,
		50 * time.Millisecond,
	}

	for _, latency := range latencies {
		collector.RecordInputLatency(latency)
		collector.RecordOutputLatency(latency)
	}

	// Test percentiles map
	percentilesMap := collector.GetLatencyPercentiles()
	require.NotNil(t, percentilesMap)

	// Test input percentiles if available
	if inputPercentiles, exists := percentilesMap["input"]; exists {
		assert.Greater(t, inputPercentiles.P50, time.Duration(0))
		assert.Greater(t, inputPercentiles.P95, time.Duration(0))
		assert.Greater(t, inputPercentiles.P99, time.Duration(0))
	}

	// Test output percentiles if available
	if outputPercentiles, exists := percentilesMap["output"]; exists {
		assert.Greater(t, outputPercentiles.P50, time.Duration(0))
		assert.Greater(t, outputPercentiles.P95, time.Duration(0))
		assert.Greater(t, outputPercentiles.P99, time.Duration(0))
	}
}

// testConcurrentCollectorAccess tests thread safety of the collector
func testConcurrentCollectorAccess(t *testing.T) {
	collector := GetGranularMetricsCollector()
	require.NotNil(t, collector)

	const numGoroutines = 10
	const operationsPerGoroutine = 50

	var wg sync.WaitGroup
	wg.Add(numGoroutines * 3) // 3 types of operations

	// Concurrent input latency recording
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < operationsPerGoroutine; j++ {
				latency := time.Duration(id*j+1) * time.Microsecond
				collector.RecordInputLatency(latency)
			}
		}(i)
	}

	// Concurrent output latency recording
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < operationsPerGoroutine; j++ {
				latency := time.Duration(id*j+1) * time.Microsecond
				collector.RecordOutputLatency(latency)
			}
		}(i)
	}

	// Concurrent data retrieval
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < operationsPerGoroutine; j++ {
				_ = collector.GetInputLatencyHistogram()
				_ = collector.GetOutputLatencyHistogram()
				_ = collector.GetLatencyPercentiles()
				time.Sleep(time.Microsecond)
			}
		}()
	}

	wg.Wait()

	// Verify final state is consistent
	inputData := collector.GetInputLatencyHistogram()
	outputData := collector.GetOutputLatencyHistogram()
	assert.NotNil(t, inputData)
	assert.NotNil(t, outputData)
}

// Benchmark tests for performance validation
func BenchmarkLatencyHistogram(b *testing.B) {
	logger := zerolog.Nop()
	hist := NewLatencyHistogram(1000, logger)

	b.Run("RecordLatency", func(b *testing.B) {
		latency := 5 * time.Millisecond
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			hist.RecordLatency(latency)
		}
	})

	b.Run("GetHistogramData", func(b *testing.B) {
		// Pre-populate with some data
		for i := 0; i < 100; i++ {
			hist.RecordLatency(time.Duration(i) * time.Microsecond)
		}
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = hist.GetHistogramData()
		}
	})

	b.Run("GetPercentiles", func(b *testing.B) {
		// Pre-populate with some data
		for i := 0; i < 100; i++ {
			hist.RecordLatency(time.Duration(i) * time.Microsecond)
		}
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = hist.GetPercentiles()
		}
	})
}

func BenchmarkGranularMetricsCollector(b *testing.B) {
	collector := GetGranularMetricsCollector()

	b.Run("RecordInputLatency", func(b *testing.B) {
		latency := 5 * time.Millisecond
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			collector.RecordInputLatency(latency)
		}
	})

	b.Run("RecordOutputLatency", func(b *testing.B) {
		latency := 5 * time.Millisecond
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			collector.RecordOutputLatency(latency)
		}
	})

	b.Run("GetInputLatencyHistogram", func(b *testing.B) {
		// Pre-populate with some data
		for i := 0; i < 100; i++ {
			collector.RecordInputLatency(time.Duration(i) * time.Microsecond)
		}
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = collector.GetInputLatencyHistogram()
		}
	})

	b.Run("GetOutputLatencyHistogram", func(b *testing.B) {
		// Pre-populate with some data
		for i := 0; i < 100; i++ {
			collector.RecordOutputLatency(time.Duration(i) * time.Microsecond)
		}
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = collector.GetOutputLatencyHistogram()
		}
	})
}
