package audio

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAudioOutputStreamer tests the AudioOutputStreamer component
func TestAudioOutputStreamer(t *testing.T) {
	tests := []struct {
		name     string
		testFunc func(t *testing.T)
	}{
		{"NewAudioOutputStreamer", testNewAudioOutputStreamer},
		{"Start", testAudioOutputStreamerStart},
		{"Stop", testAudioOutputStreamerStop},
		{"StartStop", testAudioOutputStreamerStartStop},
		{"GetStats", testAudioOutputStreamerGetStats},
		{"GetDetailedStats", testAudioOutputStreamerGetDetailedStats},
		{"UpdateBatchSize", testAudioOutputStreamerUpdateBatchSize},
		{"ReportLatency", testAudioOutputStreamerReportLatency},
		{"ConcurrentOperations", testAudioOutputStreamerConcurrent},
		{"MultipleStarts", testAudioOutputStreamerMultipleStarts},
		{"MultipleStops", testAudioOutputStreamerMultipleStops},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.testFunc(t)
		})
	}
}

func testNewAudioOutputStreamer(t *testing.T) {
	streamer, err := NewAudioOutputStreamer()
	if err != nil {
		// If creation fails due to missing dependencies, skip the test
		t.Skipf("Skipping test due to missing dependencies: %v", err)
		return
	}
	require.NotNil(t, streamer)

	// Test initial state
	processed, dropped, avgTime := streamer.GetStats()
	assert.GreaterOrEqual(t, processed, int64(0))
	assert.GreaterOrEqual(t, dropped, int64(0))
	assert.GreaterOrEqual(t, avgTime, time.Duration(0))

	// Cleanup
	streamer.Stop()
}

func testAudioOutputStreamerStart(t *testing.T) {
	streamer, err := NewAudioOutputStreamer()
	if err != nil {
		t.Skipf("Skipping test due to missing dependencies: %v", err)
		return
	}
	require.NotNil(t, streamer)

	// Test start
	err = streamer.Start()
	assert.NoError(t, err)

	// Cleanup
	streamer.Stop()
}

func testAudioOutputStreamerStop(t *testing.T) {
	streamer, err := NewAudioOutputStreamer()
	if err != nil {
		t.Skipf("Skipping test due to missing dependencies: %v", err)
		return
	}
	require.NotNil(t, streamer)

	// Start first
	err = streamer.Start()
	require.NoError(t, err)

	// Test stop
	streamer.Stop()

	// Multiple stops should be safe
	streamer.Stop()
	streamer.Stop()
}

func testAudioOutputStreamerStartStop(t *testing.T) {
	streamer, err := NewAudioOutputStreamer()
	if err != nil {
		t.Skipf("Skipping test due to missing dependencies: %v", err)
		return
	}
	require.NotNil(t, streamer)

	// Test multiple start/stop cycles
	for i := 0; i < 3; i++ {
		// Start
		err = streamer.Start()
		assert.NoError(t, err)

		// Stop
		streamer.Stop()
	}
}

func testAudioOutputStreamerGetStats(t *testing.T) {
	streamer, err := NewAudioOutputStreamer()
	if err != nil {
		t.Skipf("Skipping test due to missing dependencies: %v", err)
		return
	}
	require.NotNil(t, streamer)

	// Test stats when not running
	processed, dropped, avgTime := streamer.GetStats()
	assert.Equal(t, int64(0), processed)
	assert.Equal(t, int64(0), dropped)
	assert.GreaterOrEqual(t, avgTime, time.Duration(0))

	// Start and test stats
	err = streamer.Start()
	require.NoError(t, err)

	processed, dropped, avgTime = streamer.GetStats()
	assert.GreaterOrEqual(t, processed, int64(0))
	assert.GreaterOrEqual(t, dropped, int64(0))
	assert.GreaterOrEqual(t, avgTime, time.Duration(0))

	// Cleanup
	streamer.Stop()
}

func testAudioOutputStreamerGetDetailedStats(t *testing.T) {
	streamer, err := NewAudioOutputStreamer()
	if err != nil {
		t.Skipf("Skipping test due to missing dependencies: %v", err)
		return
	}
	require.NotNil(t, streamer)

	// Test detailed stats
	stats := streamer.GetDetailedStats()
	assert.NotNil(t, stats)
	assert.Contains(t, stats, "processed_frames")
	assert.Contains(t, stats, "dropped_frames")
	assert.Contains(t, stats, "batch_size")
	assert.Contains(t, stats, "connected")
	assert.Equal(t, int64(0), stats["processed_frames"])
	assert.Equal(t, int64(0), stats["dropped_frames"])

	// Start and test detailed stats
	err = streamer.Start()
	require.NoError(t, err)

	stats = streamer.GetDetailedStats()
	assert.NotNil(t, stats)
	assert.Contains(t, stats, "processed_frames")
	assert.Contains(t, stats, "dropped_frames")

	// Cleanup
	streamer.Stop()
}

func testAudioOutputStreamerUpdateBatchSize(t *testing.T) {
	streamer, err := NewAudioOutputStreamer()
	if err != nil {
		t.Skipf("Skipping test due to missing dependencies: %v", err)
		return
	}
	require.NotNil(t, streamer)

	// Test updating batch size (no parameters, uses adaptive manager)
	streamer.UpdateBatchSize()
	streamer.UpdateBatchSize()
	streamer.UpdateBatchSize()

	// Cleanup
	streamer.Stop()
}

func testAudioOutputStreamerReportLatency(t *testing.T) {
	streamer, err := NewAudioOutputStreamer()
	if err != nil {
		t.Skipf("Skipping test due to missing dependencies: %v", err)
		return
	}
	require.NotNil(t, streamer)

	// Test reporting latency
	streamer.ReportLatency(10 * time.Millisecond)
	streamer.ReportLatency(5 * time.Millisecond)
	streamer.ReportLatency(15 * time.Millisecond)

	// Cleanup
	streamer.Stop()
}

func testAudioOutputStreamerConcurrent(t *testing.T) {
	streamer, err := NewAudioOutputStreamer()
	if err != nil {
		t.Skipf("Skipping test due to missing dependencies: %v", err)
		return
	}
	require.NotNil(t, streamer)

	var wg sync.WaitGroup
	const numGoroutines = 10

	// Test concurrent starts
	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			streamer.Start()
		}()
	}
	wg.Wait()

	// Test concurrent operations
	wg.Add(numGoroutines * 3)
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			streamer.GetStats()
		}()
		go func() {
			defer wg.Done()
			streamer.UpdateBatchSize()
		}()
		go func() {
			defer wg.Done()
			streamer.ReportLatency(10 * time.Millisecond)
		}()
	}
	wg.Wait()

	// Test concurrent stops
	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			streamer.Stop()
		}()
	}
	wg.Wait()
}

func testAudioOutputStreamerMultipleStarts(t *testing.T) {
	streamer, err := NewAudioOutputStreamer()
	if err != nil {
		t.Skipf("Skipping test due to missing dependencies: %v", err)
		return
	}
	require.NotNil(t, streamer)

	// First start should succeed
	err = streamer.Start()
	assert.NoError(t, err)

	// Subsequent starts should return error
	err = streamer.Start()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already running")

	err = streamer.Start()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already running")

	// Cleanup
	streamer.Stop()
}

func testAudioOutputStreamerMultipleStops(t *testing.T) {
	streamer, err := NewAudioOutputStreamer()
	if err != nil {
		t.Skipf("Skipping test due to missing dependencies: %v", err)
		return
	}
	require.NotNil(t, streamer)

	// Start first
	err = streamer.Start()
	require.NoError(t, err)

	// Multiple stops should be safe
	streamer.Stop()
	streamer.Stop()
	streamer.Stop()
}

// BenchmarkAudioOutputStreamer benchmarks the AudioOutputStreamer operations
func BenchmarkAudioOutputStreamer(b *testing.B) {
	b.Run("GetStats", func(b *testing.B) {
		streamer, err := NewAudioOutputStreamer()
		if err != nil {
			b.Skipf("Skipping benchmark due to missing dependencies: %v", err)
			return
		}
		defer streamer.Stop()

		streamer.Start()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			streamer.GetStats()
		}
	})

	b.Run("UpdateBatchSize", func(b *testing.B) {
		streamer, err := NewAudioOutputStreamer()
		if err != nil {
			b.Skipf("Skipping benchmark due to missing dependencies: %v", err)
			return
		}
		defer streamer.Stop()

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			streamer.UpdateBatchSize()
		}
	})

	b.Run("ReportLatency", func(b *testing.B) {
		streamer, err := NewAudioOutputStreamer()
		if err != nil {
			b.Skipf("Skipping benchmark due to missing dependencies: %v", err)
			return
		}
		defer streamer.Stop()

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			streamer.ReportLatency(10 * time.Millisecond)
		}
	})
}
