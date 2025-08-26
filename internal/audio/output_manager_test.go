package audio

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAudioOutputManager tests the AudioOutputManager component
func TestAudioOutputManager(t *testing.T) {
	tests := []struct {
		name     string
		testFunc func(t *testing.T)
	}{
		{"Start", testAudioOutputManagerStart},
		{"Stop", testAudioOutputManagerStop},
		{"StartStop", testAudioOutputManagerStartStop},
		{"IsRunning", testAudioOutputManagerIsRunning},
		{"IsReady", testAudioOutputManagerIsReady},
		{"GetMetrics", testAudioOutputManagerGetMetrics},
		{"ConcurrentOperations", testAudioOutputManagerConcurrent},
		{"MultipleStarts", testAudioOutputManagerMultipleStarts},
		{"MultipleStops", testAudioOutputManagerMultipleStops},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.testFunc(t)
		})
	}
}

func testAudioOutputManagerStart(t *testing.T) {
	manager := NewAudioOutputManager()
	require.NotNil(t, manager)

	// Test initial state
	assert.False(t, manager.IsRunning())
	assert.False(t, manager.IsReady())

	// Test start
	err := manager.Start()
	assert.NoError(t, err)
	assert.True(t, manager.IsRunning())

	// Cleanup
	manager.Stop()
}

func testAudioOutputManagerStop(t *testing.T) {
	manager := NewAudioOutputManager()
	require.NotNil(t, manager)

	// Start first
	err := manager.Start()
	require.NoError(t, err)
	assert.True(t, manager.IsRunning())

	// Test stop
	manager.Stop()
	assert.False(t, manager.IsRunning())
	assert.False(t, manager.IsReady())
}

func testAudioOutputManagerStartStop(t *testing.T) {
	manager := NewAudioOutputManager()
	require.NotNil(t, manager)

	// Test multiple start/stop cycles
	for i := 0; i < 3; i++ {
		// Start
		err := manager.Start()
		assert.NoError(t, err)
		assert.True(t, manager.IsRunning())

		// Stop
		manager.Stop()
		assert.False(t, manager.IsRunning())
	}
}

func testAudioOutputManagerIsRunning(t *testing.T) {
	manager := NewAudioOutputManager()
	require.NotNil(t, manager)

	// Initially not running
	assert.False(t, manager.IsRunning())

	// Start and check
	err := manager.Start()
	require.NoError(t, err)
	assert.True(t, manager.IsRunning())

	// Stop and check
	manager.Stop()
	assert.False(t, manager.IsRunning())
}

func testAudioOutputManagerIsReady(t *testing.T) {
	manager := NewAudioOutputManager()
	require.NotNil(t, manager)

	// Initially not ready
	assert.False(t, manager.IsReady())

	// Start and check ready state
	err := manager.Start()
	require.NoError(t, err)

	// Give some time for initialization
	time.Sleep(100 * time.Millisecond)

	// Stop
	manager.Stop()
	assert.False(t, manager.IsReady())
}

func testAudioOutputManagerGetMetrics(t *testing.T) {
	manager := NewAudioOutputManager()
	require.NotNil(t, manager)

	// Test metrics when not running
	metrics := manager.GetMetrics()
	assert.NotNil(t, metrics)

	// Start and test metrics
	err := manager.Start()
	require.NoError(t, err)

	metrics = manager.GetMetrics()
	assert.NotNil(t, metrics)

	// Cleanup
	manager.Stop()
}

func testAudioOutputManagerConcurrent(t *testing.T) {
	manager := NewAudioOutputManager()
	require.NotNil(t, manager)

	var wg sync.WaitGroup
	const numGoroutines = 10

	// Test concurrent starts
	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			manager.Start()
		}()
	}
	wg.Wait()

	// Should be running
	assert.True(t, manager.IsRunning())

	// Test concurrent stops
	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			manager.Stop()
		}()
	}
	wg.Wait()

	// Should be stopped
	assert.False(t, manager.IsRunning())
}

func testAudioOutputManagerMultipleStarts(t *testing.T) {
	manager := NewAudioOutputManager()
	require.NotNil(t, manager)

	// First start should succeed
	err := manager.Start()
	assert.NoError(t, err)
	assert.True(t, manager.IsRunning())

	// Subsequent starts should be no-op
	err = manager.Start()
	assert.NoError(t, err)
	assert.True(t, manager.IsRunning())

	err = manager.Start()
	assert.NoError(t, err)
	assert.True(t, manager.IsRunning())

	// Cleanup
	manager.Stop()
}

func testAudioOutputManagerMultipleStops(t *testing.T) {
	manager := NewAudioOutputManager()
	require.NotNil(t, manager)

	// Start first
	err := manager.Start()
	require.NoError(t, err)
	assert.True(t, manager.IsRunning())

	// First stop should work
	manager.Stop()
	assert.False(t, manager.IsRunning())

	// Subsequent stops should be no-op
	manager.Stop()
	assert.False(t, manager.IsRunning())

	manager.Stop()
	assert.False(t, manager.IsRunning())
}

// TestAudioOutputMetrics tests the AudioOutputMetrics functionality
func TestAudioOutputMetrics(t *testing.T) {
	metrics := &AudioOutputMetrics{}

	// Test initial state
	assert.Equal(t, int64(0), metrics.FramesReceived)
	assert.Equal(t, int64(0), metrics.FramesDropped)
	assert.Equal(t, int64(0), metrics.BytesProcessed)
	assert.Equal(t, int64(0), metrics.ConnectionDrops)
	assert.Equal(t, time.Duration(0), metrics.AverageLatency)
	assert.True(t, metrics.LastFrameTime.IsZero())

	// Test field assignment
	metrics.FramesReceived = 100
	metrics.FramesDropped = 5
	metrics.BytesProcessed = 1024
	metrics.ConnectionDrops = 2
	metrics.AverageLatency = 10 * time.Millisecond
	metrics.LastFrameTime = time.Now()

	// Verify assignments
	assert.Equal(t, int64(100), metrics.FramesReceived)
	assert.Equal(t, int64(5), metrics.FramesDropped)
	assert.Equal(t, int64(1024), metrics.BytesProcessed)
	assert.Equal(t, int64(2), metrics.ConnectionDrops)
	assert.Equal(t, 10*time.Millisecond, metrics.AverageLatency)
	assert.False(t, metrics.LastFrameTime.IsZero())
}

// BenchmarkAudioOutputManager benchmarks the AudioOutputManager operations
func BenchmarkAudioOutputManager(b *testing.B) {
	b.Run("Start", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			manager := NewAudioOutputManager()
			manager.Start()
			manager.Stop()
		}
	})

	b.Run("IsRunning", func(b *testing.B) {
		manager := NewAudioOutputManager()
		manager.Start()
		defer manager.Stop()

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			manager.IsRunning()
		}
	})

	b.Run("GetMetrics", func(b *testing.B) {
		manager := NewAudioOutputManager()
		manager.Start()
		defer manager.Stop()

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			manager.GetMetrics()
		}
	})
}
