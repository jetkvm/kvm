package audio

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGranularMetricsCollector tests the GranularMetricsCollector functionality
func TestGranularMetricsCollector(t *testing.T) {
	tests := []struct {
		name     string
		testFunc func(t *testing.T)
	}{
		{"GetGranularMetricsCollector", testGetGranularMetricsCollector},
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

// testConcurrentCollectorAccess tests thread safety of the collector
func testConcurrentCollectorAccess(t *testing.T) {
	collector := GetGranularMetricsCollector()
	require.NotNil(t, collector)

	const numGoroutines = 10
	const operationsPerGoroutine = 50

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// Concurrent buffer pool operations
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < operationsPerGoroutine; j++ {
				// Test buffer pool operations
				latency := time.Duration(id*operationsPerGoroutine+j) * time.Microsecond
				collector.RecordFramePoolGet(latency, true)
				collector.RecordFramePoolPut(latency, 1024)
			}
		}(i)
	}

	wg.Wait()

	// Verify collector is still functional
	efficiency := collector.GetBufferPoolEfficiency()
	assert.NotNil(t, efficiency)
}

func BenchmarkGranularMetricsCollector(b *testing.B) {
	collector := GetGranularMetricsCollector()

	b.Run("RecordFramePoolGet", func(b *testing.B) {
		latency := 5 * time.Millisecond
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			collector.RecordFramePoolGet(latency, true)
		}
	})

	b.Run("RecordFramePoolPut", func(b *testing.B) {
		latency := 5 * time.Millisecond
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			collector.RecordFramePoolPut(latency, 1024)
		}
	})

	b.Run("GetBufferPoolEfficiency", func(b *testing.B) {
		// Pre-populate with some data
		for i := 0; i < 100; i++ {
			collector.RecordFramePoolGet(time.Duration(i)*time.Microsecond, true)
			collector.RecordFramePoolPut(time.Duration(i)*time.Microsecond, 1024)
		}
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = collector.GetBufferPoolEfficiency()
		}
	})
}
