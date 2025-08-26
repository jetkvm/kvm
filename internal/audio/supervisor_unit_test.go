package audio

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewAudioOutputSupervisor(t *testing.T) {
	supervisor := NewAudioOutputSupervisor()
	assert.NotNil(t, supervisor)
	assert.False(t, supervisor.IsRunning())
}

func TestAudioOutputSupervisorStart(t *testing.T) {
	supervisor := NewAudioOutputSupervisor()
	require.NotNil(t, supervisor)

	// Test successful start
	err := supervisor.Start()
	assert.NoError(t, err)
	assert.True(t, supervisor.IsRunning())

	// Test starting already running supervisor
	err = supervisor.Start()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already running")

	// Cleanup
	supervisor.Stop()
}

func TestAudioOutputSupervisorStop(t *testing.T) {
	supervisor := NewAudioOutputSupervisor()
	require.NotNil(t, supervisor)

	// Test stopping non-running supervisor
	supervisor.Stop()
	assert.False(t, supervisor.IsRunning())

	// Start and then stop
	err := supervisor.Start()
	require.NoError(t, err)
	assert.True(t, supervisor.IsRunning())

	supervisor.Stop()
	assert.False(t, supervisor.IsRunning())
}

func TestAudioOutputSupervisorIsRunning(t *testing.T) {
	supervisor := NewAudioOutputSupervisor()
	require.NotNil(t, supervisor)

	// Test initial state
	assert.False(t, supervisor.IsRunning())

	// Test after start
	err := supervisor.Start()
	require.NoError(t, err)
	assert.True(t, supervisor.IsRunning())

	// Test after stop
	supervisor.Stop()
	assert.False(t, supervisor.IsRunning())
}

func TestAudioOutputSupervisorGetProcessMetrics(t *testing.T) {
	supervisor := NewAudioOutputSupervisor()
	require.NotNil(t, supervisor)

	// Test metrics when not running
	metrics := supervisor.GetProcessMetrics()
	assert.NotNil(t, metrics)

	// Start and test metrics
	err := supervisor.Start()
	require.NoError(t, err)

	metrics = supervisor.GetProcessMetrics()
	assert.NotNil(t, metrics)

	// Cleanup
	supervisor.Stop()
}

func TestAudioOutputSupervisorConcurrentOperations(t *testing.T) {
	supervisor := NewAudioOutputSupervisor()
	require.NotNil(t, supervisor)

	var wg sync.WaitGroup

	// Test concurrent start/stop operations
	for i := 0; i < 10; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = supervisor.Start()
		}()
		go func() {
			defer wg.Done()
			supervisor.Stop()
		}()
	}

	// Test concurrent metric access
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = supervisor.GetProcessMetrics()
		}()
	}

	// Test concurrent status checks
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = supervisor.IsRunning()
		}()
	}

	wg.Wait()

	// Cleanup
	supervisor.Stop()
}

func TestAudioOutputSupervisorMultipleStartStop(t *testing.T) {
	supervisor := NewAudioOutputSupervisor()
	require.NotNil(t, supervisor)

	// Test multiple start/stop cycles
	for i := 0; i < 5; i++ {
		err := supervisor.Start()
		assert.NoError(t, err)
		assert.True(t, supervisor.IsRunning())

		supervisor.Stop()
		assert.False(t, supervisor.IsRunning())
	}
}

func TestAudioOutputSupervisorHealthCheck(t *testing.T) {
	supervisor := NewAudioOutputSupervisor()
	require.NotNil(t, supervisor)

	// Start supervisor
	err := supervisor.Start()
	require.NoError(t, err)

	// Give some time for health monitoring to initialize
	time.Sleep(100 * time.Millisecond)

	// Test that supervisor is still running
	assert.True(t, supervisor.IsRunning())

	// Cleanup
	supervisor.Stop()
}

func TestAudioOutputSupervisorProcessManagement(t *testing.T) {
	supervisor := NewAudioOutputSupervisor()
	require.NotNil(t, supervisor)

	// Start supervisor
	err := supervisor.Start()
	require.NoError(t, err)

	// Give some time for process management to initialize
	time.Sleep(200 * time.Millisecond)

	// Test that supervisor is managing processes
	assert.True(t, supervisor.IsRunning())

	// Cleanup
	supervisor.Stop()

	// Ensure supervisor stopped cleanly
	assert.False(t, supervisor.IsRunning())
}

// Benchmark tests
func BenchmarkAudioOutputSupervisor(b *testing.B) {
	supervisor := NewAudioOutputSupervisor()

	b.Run("Start", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = supervisor.Start()
			supervisor.Stop()
		}
	})

	b.Run("GetProcessMetrics", func(b *testing.B) {
		_ = supervisor.Start()
		defer supervisor.Stop()

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = supervisor.GetProcessMetrics()
		}
	})

	b.Run("IsRunning", func(b *testing.B) {
		_ = supervisor.Start()
		defer supervisor.Stop()

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = supervisor.IsRunning()
		}
	})
}
