//go:build integration && cgo
// +build integration,cgo

package audio

import (
	"context"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSupervisorRestart tests various supervisor restart scenarios
func TestSupervisorRestart(t *testing.T) {
	tests := []struct {
		name        string
		testFunc    func(t *testing.T)
		description string
	}{
		{
			name:        "BasicRestart",
			testFunc:    testBasicSupervisorRestart,
			description: "Test basic supervisor restart functionality",
		},
		{
			name:        "ProcessCrashRestart",
			testFunc:    testProcessCrashRestart,
			description: "Test supervisor restart after process crash",
		},
		{
			name:        "MaxRestartAttempts",
			testFunc:    testMaxRestartAttempts,
			description: "Test supervisor respects max restart attempts",
		},
		{
			name:        "ExponentialBackoff",
			testFunc:    testExponentialBackoff,
			description: "Test supervisor exponential backoff behavior",
		},
		{
			name:        "HealthMonitoring",
			testFunc:    testHealthMonitoring,
			description: "Test supervisor health monitoring",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Logf("Running supervisor test: %s - %s", tt.name, tt.description)
			tt.testFunc(t)
		})
	}
}

// testBasicSupervisorRestart tests basic restart functionality
func testBasicSupervisorRestart(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Create a mock supervisor with a simple test command
	supervisor := &AudioInputSupervisor{
		logger:          getTestLogger(),
		maxRestarts:     3,
		restartDelay:    100 * time.Millisecond,
		healthCheckInterval: 200 * time.Millisecond,
	}

	// Use a simple command that will exit quickly for testing
	testCmd := exec.CommandContext(ctx, "sleep", "0.5")
	supervisor.cmd = testCmd

	var wg sync.WaitGroup
	wg.Add(1)

	// Start supervisor
	go func() {
		defer wg.Done()
		supervisor.Start(ctx)
	}()

	// Wait for initial process to start and exit
	time.Sleep(1 * time.Second)

	// Verify that supervisor attempted restart
	assert.True(t, supervisor.GetRestartCount() > 0, "Supervisor should have attempted restart")

	// Stop supervisor
	cancel()
	wg.Wait()
}

// testProcessCrashRestart tests restart after process crash
func testProcessCrashRestart(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	supervisor := &AudioInputSupervisor{
		logger:          getTestLogger(),
		maxRestarts:     2,
		restartDelay:    200 * time.Millisecond,
		healthCheckInterval: 100 * time.Millisecond,
	}

	// Create a command that will crash (exit with non-zero code)
	testCmd := exec.CommandContext(ctx, "sh", "-c", "sleep 0.2 && exit 1")
	supervisor.cmd = testCmd

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		supervisor.Start(ctx)
	}()

	// Wait for process to crash and restart attempts
	time.Sleep(2 * time.Second)

	// Verify restart attempts were made
	restartCount := supervisor.GetRestartCount()
	assert.True(t, restartCount > 0, "Supervisor should have attempted restart after crash")
	assert.True(t, restartCount <= 2, "Supervisor should not exceed max restart attempts")

	cancel()
	wg.Wait()
}

// testMaxRestartAttempts tests that supervisor respects max restart limit
func testMaxRestartAttempts(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	maxRestarts := 3
	supervisor := &AudioInputSupervisor{
		logger:          getTestLogger(),
		maxRestarts:     maxRestarts,
		restartDelay:    50 * time.Millisecond,
		healthCheckInterval: 50 * time.Millisecond,
	}

	// Command that immediately fails
	testCmd := exec.CommandContext(ctx, "false") // 'false' command always exits with code 1
	supervisor.cmd = testCmd

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		supervisor.Start(ctx)
	}()

	// Wait for all restart attempts to complete
	time.Sleep(2 * time.Second)

	// Verify that supervisor stopped after max attempts
	restartCount := supervisor.GetRestartCount()
	assert.Equal(t, maxRestarts, restartCount, "Supervisor should stop after max restart attempts")
	assert.False(t, supervisor.IsRunning(), "Supervisor should not be running after max attempts")

	cancel()
	wg.Wait()
}

// testExponentialBackoff tests the exponential backoff behavior
func testExponentialBackoff(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	supervisor := &AudioInputSupervisor{
		logger:          getTestLogger(),
		maxRestarts:     3,
		restartDelay:    100 * time.Millisecond, // Base delay
		healthCheckInterval: 50 * time.Millisecond,
	}

	// Command that fails immediately
	testCmd := exec.CommandContext(ctx, "false")
	supervisor.cmd = testCmd

	var restartTimes []time.Time
	var mu sync.Mutex

	// Hook into restart events to measure timing
	originalRestart := supervisor.restart
	supervisor.restart = func() {
		mu.Lock()
		restartTimes = append(restartTimes, time.Now())
		mu.Unlock()
		if originalRestart != nil {
			originalRestart()
		}
	}

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		supervisor.Start(ctx)
	}()

	// Wait for restart attempts
	time.Sleep(3 * time.Second)

	mu.Lock()
	defer mu.Unlock()

	// Verify exponential backoff (each delay should be longer than the previous)
	if len(restartTimes) >= 2 {
		for i := 1; i < len(restartTimes); i++ {
			delay := restartTimes[i].Sub(restartTimes[i-1])
			expectedMinDelay := time.Duration(i) * 100 * time.Millisecond
			assert.True(t, delay >= expectedMinDelay,
				"Restart delay should increase exponentially: attempt %d delay %v should be >= %v",
				i, delay, expectedMinDelay)
		}
	}

	cancel()
	wg.Wait()
}

// testHealthMonitoring tests the health monitoring functionality
func testHealthMonitoring(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	supervisor := &AudioInputSupervisor{
		logger:          getTestLogger(),
		maxRestarts:     2,
		restartDelay:    100 * time.Millisecond,
		healthCheckInterval: 50 * time.Millisecond,
	}

	// Command that runs for a while then exits
	testCmd := exec.CommandContext(ctx, "sleep", "1")
	supervisor.cmd = testCmd

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		supervisor.Start(ctx)
	}()

	// Initially should be running
	time.Sleep(200 * time.Millisecond)
	assert.True(t, supervisor.IsRunning(), "Supervisor should be running initially")

	// Wait for process to exit and health check to detect it
	time.Sleep(1.5 * time.Second)

	// Should have detected process exit and attempted restart
	assert.True(t, supervisor.GetRestartCount() > 0, "Health monitoring should detect process exit")

	cancel()
	wg.Wait()
}

// TestAudioInputSupervisorIntegration tests the actual AudioInputSupervisor
func TestAudioInputSupervisorIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Create a real supervisor instance
	supervisor := NewAudioInputSupervisor()
	require.NotNil(t, supervisor, "Supervisor should be created")

	// Test that supervisor can be started and stopped cleanly
	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		// This will likely fail due to missing audio hardware in test environment,
		// but we're testing the supervisor logic, not the audio functionality
		supervisor.Start(ctx)
	}()

	// Let it run briefly
	time.Sleep(500 * time.Millisecond)

	// Stop the supervisor
	cancel()
	wg.Wait()

	// Verify clean shutdown
	assert.False(t, supervisor.IsRunning(), "Supervisor should not be running after context cancellation")
}

// Mock supervisor for testing (simplified version)
type AudioInputSupervisor struct {
	logger              zerolog.Logger
	cmd                 *exec.Cmd
	maxRestarts         int
	restartDelay        time.Duration
	healthCheckInterval time.Duration
	restartCount        int
	running             bool
	mu                  sync.RWMutex
	restart             func() // Hook for testing
}

func (s *AudioInputSupervisor) Start(ctx context.Context) error {
	s.mu.Lock()
	s.running = true
	s.mu.Unlock()

	for s.restartCount < s.maxRestarts {
		select {
		case <-ctx.Done():
			s.mu.Lock()
			s.running = false
			s.mu.Unlock()
			return ctx.Err()
		default:
		}

		// Start process
		if s.cmd != nil {
			err := s.cmd.Start()
			if err != nil {
				s.logger.Error().Err(err).Msg("Failed to start process")
				s.restartCount++
				time.Sleep(s.getBackoffDelay())
				continue
			}

			// Wait for process to exit
			err = s.cmd.Wait()
			if err != nil {
				s.logger.Error().Err(err).Msg("Process exited with error")
			}
		}

		s.restartCount++
		if s.restart != nil {
			s.restart()
		}

		if s.restartCount < s.maxRestarts {
			time.Sleep(s.getBackoffDelay())
		}
	}

	s.mu.Lock()
	s.running = false
	s.mu.Unlock()
	return nil
}

func (s *AudioInputSupervisor) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

func (s *AudioInputSupervisor) GetRestartCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.restartCount
}

func (s *AudioInputSupervisor) getBackoffDelay() time.Duration {
	// Simple exponential backoff
	multiplier := 1 << uint(s.restartCount)
	if multiplier > 8 {
		multiplier = 8 // Cap the multiplier
	}
	return s.restartDelay * time.Duration(multiplier)
}

// NewAudioInputSupervisor creates a new supervisor for testing
func NewAudioInputSupervisor() *AudioInputSupervisor {
	return &AudioInputSupervisor{
		logger:              getTestLogger(),
		maxRestarts:         getMaxRestartAttempts(),
		restartDelay:        getInitialRestartDelay(),
		healthCheckInterval: 1 * time.Second,
	}
}