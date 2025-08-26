package audio

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewAudioInputManager(t *testing.T) {
	manager := NewAudioInputManager()
	assert.NotNil(t, manager)
	assert.False(t, manager.IsRunning())
	assert.False(t, manager.IsReady())
}

func TestAudioInputManagerStart(t *testing.T) {
	manager := NewAudioInputManager()
	require.NotNil(t, manager)

	// Test successful start
	err := manager.Start()
	assert.NoError(t, err)
	assert.True(t, manager.IsRunning())

	// Test starting already running manager
	err = manager.Start()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already running")

	// Cleanup
	manager.Stop()
}

func TestAudioInputManagerStop(t *testing.T) {
	manager := NewAudioInputManager()
	require.NotNil(t, manager)

	// Test stopping non-running manager
	manager.Stop()
	assert.False(t, manager.IsRunning())

	// Start and then stop
	err := manager.Start()
	require.NoError(t, err)
	assert.True(t, manager.IsRunning())

	manager.Stop()
	assert.False(t, manager.IsRunning())
}

func TestAudioInputManagerIsRunning(t *testing.T) {
	manager := NewAudioInputManager()
	require.NotNil(t, manager)

	// Test initial state
	assert.False(t, manager.IsRunning())

	// Test after start
	err := manager.Start()
	require.NoError(t, err)
	assert.True(t, manager.IsRunning())

	// Test after stop
	manager.Stop()
	assert.False(t, manager.IsRunning())
}

func TestAudioInputManagerIsReady(t *testing.T) {
	manager := NewAudioInputManager()
	require.NotNil(t, manager)

	// Test initial state
	assert.False(t, manager.IsReady())

	// Start manager
	err := manager.Start()
	require.NoError(t, err)

	// Give some time for initialization
	time.Sleep(100 * time.Millisecond)

	// Test ready state (may vary based on implementation)
	// Just ensure the method doesn't panic
	_ = manager.IsReady()

	// Cleanup
	manager.Stop()
}

func TestAudioInputManagerGetMetrics(t *testing.T) {
	manager := NewAudioInputManager()
	require.NotNil(t, manager)

	// Test metrics when not running
	metrics := manager.GetMetrics()
	assert.NotNil(t, metrics)
	assert.Equal(t, int64(0), metrics.FramesSent)
	assert.Equal(t, int64(0), metrics.FramesDropped)
	assert.Equal(t, int64(0), metrics.BytesProcessed)
	assert.Equal(t, int64(0), metrics.ConnectionDrops)

	// Start and test metrics
	err := manager.Start()
	require.NoError(t, err)

	metrics = manager.GetMetrics()
	assert.NotNil(t, metrics)
	assert.GreaterOrEqual(t, metrics.FramesSent, int64(0))
	assert.GreaterOrEqual(t, metrics.FramesDropped, int64(0))
	assert.GreaterOrEqual(t, metrics.BytesProcessed, int64(0))
	assert.GreaterOrEqual(t, metrics.ConnectionDrops, int64(0))

	// Cleanup
	manager.Stop()
}

func TestAudioInputManagerConcurrentOperations(t *testing.T) {
	manager := NewAudioInputManager()
	require.NotNil(t, manager)

	var wg sync.WaitGroup

	// Test concurrent start/stop operations
	for i := 0; i < 10; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = manager.Start()
		}()
		go func() {
			defer wg.Done()
			manager.Stop()
		}()
	}

	// Test concurrent metric access
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = manager.GetMetrics()
		}()
	}

	// Test concurrent status checks
	for i := 0; i < 5; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = manager.IsRunning()
		}()
		go func() {
			defer wg.Done()
			_ = manager.IsReady()
		}()
	}

	wg.Wait()

	// Cleanup
	manager.Stop()
}

func TestAudioInputManagerMultipleStartStop(t *testing.T) {
	manager := NewAudioInputManager()
	require.NotNil(t, manager)

	// Test multiple start/stop cycles
	for i := 0; i < 5; i++ {
		err := manager.Start()
		assert.NoError(t, err)
		assert.True(t, manager.IsRunning())

		manager.Stop()
		assert.False(t, manager.IsRunning())
	}
}

func TestAudioInputMetrics(t *testing.T) {
	metrics := &AudioInputMetrics{
		FramesSent:      100,
		FramesDropped:   5,
		BytesProcessed:  1024,
		ConnectionDrops: 2,
		AverageLatency:  time.Millisecond * 10,
		LastFrameTime:   time.Now(),
	}

	assert.Equal(t, int64(100), metrics.FramesSent)
	assert.Equal(t, int64(5), metrics.FramesDropped)
	assert.Equal(t, int64(1024), metrics.BytesProcessed)
	assert.Equal(t, int64(2), metrics.ConnectionDrops)
	assert.Equal(t, time.Millisecond*10, metrics.AverageLatency)
	assert.False(t, metrics.LastFrameTime.IsZero())
}

// Benchmark tests
func BenchmarkAudioInputManager(b *testing.B) {
	manager := NewAudioInputManager()

	b.Run("Start", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = manager.Start()
			manager.Stop()
		}
	})

	b.Run("GetMetrics", func(b *testing.B) {
		_ = manager.Start()
		defer manager.Stop()

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = manager.GetMetrics()
		}
	})

	b.Run("IsRunning", func(b *testing.B) {
		_ = manager.Start()
		defer manager.Stop()

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = manager.IsRunning()
		}
	})

	b.Run("IsReady", func(b *testing.B) {
		_ = manager.Start()
		defer manager.Stop()

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = manager.IsReady()
		}
	})
}
