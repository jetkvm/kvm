package audio

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAudioInputIPCManager tests the AudioInputIPCManager component
func TestAudioInputIPCManager(t *testing.T) {
	tests := []struct {
		name     string
		testFunc func(t *testing.T)
	}{
		{"Start", testAudioInputIPCManagerStart},
		{"Stop", testAudioInputIPCManagerStop},
		{"StartStop", testAudioInputIPCManagerStartStop},
		{"IsRunning", testAudioInputIPCManagerIsRunning},
		{"IsReady", testAudioInputIPCManagerIsReady},
		{"GetMetrics", testAudioInputIPCManagerGetMetrics},
		{"ConcurrentOperations", testAudioInputIPCManagerConcurrent},
		{"MultipleStarts", testAudioInputIPCManagerMultipleStarts},
		{"MultipleStops", testAudioInputIPCManagerMultipleStops},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.testFunc(t)
		})
	}
}

func testAudioInputIPCManagerStart(t *testing.T) {
	manager := NewAudioInputIPCManager()
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

func testAudioInputIPCManagerStop(t *testing.T) {
	manager := NewAudioInputIPCManager()
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

func testAudioInputIPCManagerStartStop(t *testing.T) {
	manager := NewAudioInputIPCManager()
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

func testAudioInputIPCManagerIsRunning(t *testing.T) {
	manager := NewAudioInputIPCManager()
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

func testAudioInputIPCManagerIsReady(t *testing.T) {
	manager := NewAudioInputIPCManager()
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

func testAudioInputIPCManagerGetMetrics(t *testing.T) {
	manager := NewAudioInputIPCManager()
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

func testAudioInputIPCManagerConcurrent(t *testing.T) {
	manager := NewAudioInputIPCManager()
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

func testAudioInputIPCManagerMultipleStarts(t *testing.T) {
	manager := NewAudioInputIPCManager()
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

func testAudioInputIPCManagerMultipleStops(t *testing.T) {
	manager := NewAudioInputIPCManager()
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

// TestAudioInputIPCMetrics tests the AudioInputMetrics functionality
func TestAudioInputIPCMetrics(t *testing.T) {
	metrics := &AudioInputMetrics{}

	// Test initial state
	assert.Equal(t, int64(0), metrics.FramesSent)
	assert.Equal(t, int64(0), metrics.FramesDropped)
	assert.Equal(t, int64(0), metrics.BytesProcessed)
	assert.Equal(t, int64(0), metrics.ConnectionDrops)
	assert.Equal(t, time.Duration(0), metrics.AverageLatency)
	assert.True(t, metrics.LastFrameTime.IsZero())

	// Test field assignment
	metrics.FramesSent = 50
	metrics.FramesDropped = 2
	metrics.BytesProcessed = 512
	metrics.ConnectionDrops = 1
	metrics.AverageLatency = 5 * time.Millisecond
	metrics.LastFrameTime = time.Now()

	// Verify assignments
	assert.Equal(t, int64(50), metrics.FramesSent)
	assert.Equal(t, int64(2), metrics.FramesDropped)
	assert.Equal(t, int64(512), metrics.BytesProcessed)
	assert.Equal(t, int64(1), metrics.ConnectionDrops)
	assert.Equal(t, 5*time.Millisecond, metrics.AverageLatency)
	assert.False(t, metrics.LastFrameTime.IsZero())
}

// BenchmarkAudioInputIPCManager benchmarks the AudioInputIPCManager operations
func BenchmarkAudioInputIPCManager(b *testing.B) {
	b.Run("Start", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			manager := NewAudioInputIPCManager()
			manager.Start()
			manager.Stop()
		}
	})

	b.Run("IsRunning", func(b *testing.B) {
		manager := NewAudioInputIPCManager()
		manager.Start()
		defer manager.Stop()

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			manager.IsRunning()
		}
	})

	b.Run("GetMetrics", func(b *testing.B) {
		manager := NewAudioInputIPCManager()
		manager.Start()
		defer manager.Stop()

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			manager.GetMetrics()
		}
	})
}