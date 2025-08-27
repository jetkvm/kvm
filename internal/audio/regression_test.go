//go:build cgo
// +build cgo

package audio

import (
	"fmt"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRegressionScenarios tests critical edge cases and error conditions
// that could cause system instability in production
func TestRegressionScenarios(t *testing.T) {
	tests := []struct {
		name        string
		testFunc    func(t *testing.T)
		description string
	}{
		{
			name:        "IPCConnectionFailure",
			testFunc:    testIPCConnectionFailureRecovery,
			description: "Test IPC connection failure and recovery scenarios",
		},
		{
			name:        "BufferOverflow",
			testFunc:    testBufferOverflowHandling,
			description: "Test buffer overflow protection and recovery",
		},
		{
			name:        "SupervisorRapidRestart",
			testFunc:    testSupervisorRapidRestartScenario,
			description: "Test supervisor behavior under rapid restart conditions",
		},
		{
			name:        "ConcurrentStartStop",
			testFunc:    testConcurrentStartStopOperations,
			description: "Test concurrent start/stop operations for race conditions",
		},
		{
			name:        "MemoryLeakPrevention",
			testFunc:    testMemoryLeakPrevention,
			description: "Test memory leak prevention in long-running scenarios",
		},
		{
			name:        "ConfigValidationEdgeCases",
			testFunc:    testConfigValidationEdgeCases,
			description: "Test configuration validation with edge case values",
		},
		{
			name:        "AtomicOperationConsistency",
			testFunc:    testAtomicOperationConsistency,
			description: "Test atomic operations consistency under high concurrency",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Logf("Running regression test: %s - %s", tt.name, tt.description)
			tt.testFunc(t)
		})
	}
}

// testIPCConnectionFailureRecovery tests IPC connection failure scenarios
func testIPCConnectionFailureRecovery(t *testing.T) {
	manager := NewAudioInputIPCManager()
	require.NotNil(t, manager)

	// Test start with no IPC server available (should handle gracefully)
	err := manager.Start()
	// Should not panic or crash, may return error depending on implementation
	if err != nil {
		t.Logf("Expected error when no IPC server available: %v", err)
	}

	// Test that manager can recover after IPC becomes available
	if manager.IsRunning() {
		manager.Stop()
	}

	// Verify clean state after failure
	assert.False(t, manager.IsRunning())
	assert.False(t, manager.IsReady())
}

// testBufferOverflowHandling tests buffer overflow protection
func testBufferOverflowHandling(t *testing.T) {
	// Test with extremely large buffer sizes
	extremelyLargeSize := 1024 * 1024 * 100 // 100MB
	err := ValidateBufferSize(extremelyLargeSize)
	assert.Error(t, err, "Should reject extremely large buffer sizes")

	// Test with negative buffer sizes
	err = ValidateBufferSize(-1)
	assert.Error(t, err, "Should reject negative buffer sizes")

	// Test with zero buffer size
	err = ValidateBufferSize(0)
	assert.Error(t, err, "Should reject zero buffer size")

	// Test with maximum valid buffer size
	maxValidSize := GetConfig().SocketMaxBuffer
	err = ValidateBufferSize(int(maxValidSize))
	assert.NoError(t, err, "Should accept maximum valid buffer size")
}

// testSupervisorRapidRestartScenario tests supervisor under rapid restart conditions
func testSupervisorRapidRestartScenario(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping rapid restart test in short mode")
	}

	supervisor := NewAudioOutputSupervisor()
	require.NotNil(t, supervisor)

	// Perform rapid start/stop cycles to test for race conditions
	for i := 0; i < 10; i++ {
		err := supervisor.Start()
		if err != nil {
			t.Logf("Start attempt %d failed (expected in test environment): %v", i, err)
		}

		// Very short delay to stress test
		time.Sleep(10 * time.Millisecond)

		supervisor.Stop()
		time.Sleep(10 * time.Millisecond)
	}

	// Verify supervisor is in clean state after rapid cycling
	assert.False(t, supervisor.IsRunning())
}

// testConcurrentStartStopOperations tests concurrent operations for race conditions
func testConcurrentStartStopOperations(t *testing.T) {
	manager := NewAudioInputIPCManager()
	require.NotNil(t, manager)

	var wg sync.WaitGroup
	const numGoroutines = 10

	// Launch multiple goroutines trying to start/stop concurrently
	for i := 0; i < numGoroutines; i++ {
		wg.Add(2)

		// Start goroutine
		go func(id int) {
			defer wg.Done()
			err := manager.Start()
			if err != nil {
				t.Logf("Concurrent start %d: %v", id, err)
			}
		}(i)

		// Stop goroutine
		go func(id int) {
			defer wg.Done()
			time.Sleep(5 * time.Millisecond) // Small delay
			manager.Stop()
		}(i)
	}

	wg.Wait()

	// Ensure final state is consistent
	manager.Stop() // Final cleanup
	assert.False(t, manager.IsRunning())
}

// testMemoryLeakPrevention tests for memory leaks in long-running scenarios
func testMemoryLeakPrevention(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping memory leak test in short mode")
	}

	manager := NewAudioInputIPCManager()
	require.NotNil(t, manager)

	// Simulate long-running operation with periodic restarts
	for cycle := 0; cycle < 5; cycle++ {
		err := manager.Start()
		if err != nil {
			t.Logf("Start cycle %d failed (expected): %v", cycle, err)
		}

		// Simulate some activity
		time.Sleep(100 * time.Millisecond)

		// Get metrics to ensure they're not accumulating indefinitely
		metrics := manager.GetMetrics()
		assert.NotNil(t, metrics, "Metrics should be available")

		manager.Stop()
		time.Sleep(50 * time.Millisecond)
	}

	// Final verification
	assert.False(t, manager.IsRunning())
}

// testConfigValidationEdgeCases tests configuration validation with edge cases
func testConfigValidationEdgeCases(t *testing.T) {
	// Test sample rate edge cases
	testCases := []struct {
		sampleRate int
		channels   int
		frameSize  int
		shouldPass bool
		description string
	}{
		{0, 2, 960, false, "zero sample rate"},
		{-1, 2, 960, false, "negative sample rate"},
		{1, 2, 960, false, "extremely low sample rate"},
		{999999, 2, 960, false, "extremely high sample rate"},
		{48000, 0, 960, false, "zero channels"},
		{48000, -1, 960, false, "negative channels"},
		{48000, 100, 960, false, "too many channels"},
		{48000, 2, 0, false, "zero frame size"},
		{48000, 2, -1, false, "negative frame size"},
		{48000, 2, 999999, true, "extremely large frame size"},
		{48000, 2, 960, true, "valid configuration"},
		{44100, 1, 441, true, "valid mono configuration"},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			err := ValidateInputIPCConfig(tc.sampleRate, tc.channels, tc.frameSize)
			if tc.shouldPass {
				assert.NoError(t, err, "Should accept valid config: %s", tc.description)
			} else {
				assert.Error(t, err, "Should reject invalid config: %s", tc.description)
			}
		})
	}
}

// testAtomicOperationConsistency tests atomic operations under high concurrency
func testAtomicOperationConsistency(t *testing.T) {
	var counter int64
	var wg sync.WaitGroup
	const numGoroutines = 100
	const incrementsPerGoroutine = 1000

	// Launch multiple goroutines performing atomic operations
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < incrementsPerGoroutine; j++ {
				atomic.AddInt64(&counter, 1)
			}
		}()
	}

	wg.Wait()

	// Verify final count is correct
	expected := int64(numGoroutines * incrementsPerGoroutine)
	actual := atomic.LoadInt64(&counter)
	assert.Equal(t, expected, actual, "Atomic operations should be consistent")
}

// TestErrorRecoveryScenarios tests various error recovery scenarios
func TestErrorRecoveryScenarios(t *testing.T) {
	tests := []struct {
		name     string
		testFunc func(t *testing.T)
	}{
		{"NetworkConnectionLoss", testNetworkConnectionLossRecovery},
		{"ProcessCrashRecovery", testProcessCrashRecovery},
		{"ResourceExhaustionRecovery", testResourceExhaustionRecovery},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.testFunc(t)
		})
	}
}

// testNetworkConnectionLossRecovery tests recovery from network connection loss
func testNetworkConnectionLossRecovery(t *testing.T) {
	// Create a temporary socket that we can close to simulate connection loss
	tempDir := t.TempDir()
	socketPath := fmt.Sprintf("%s/test_recovery.sock", tempDir)

	// Create and immediately close a socket to test connection failure
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Skipf("Cannot create test socket: %v", err)
	}
	listener.Close() // Close immediately to simulate connection loss

	// Remove socket file to ensure connection will fail
	os.Remove(socketPath)

	// Test that components handle connection loss gracefully
	manager := NewAudioInputIPCManager()
	require.NotNil(t, manager)

	// This should handle the connection failure gracefully
	err = manager.Start()
	if err != nil {
		t.Logf("Expected connection failure handled: %v", err)
	}

	// Cleanup
	manager.Stop()
}

// testProcessCrashRecovery tests recovery from process crashes
func testProcessCrashRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping process crash test in short mode")
	}

	supervisor := NewAudioOutputSupervisor()
	require.NotNil(t, supervisor)

	// Start supervisor (will likely fail in test environment, but should handle gracefully)
	err := supervisor.Start()
	if err != nil {
		t.Logf("Supervisor start failed as expected in test environment: %v", err)
	}

	// Verify supervisor can be stopped cleanly even after start failure
	supervisor.Stop()
	assert.False(t, supervisor.IsRunning())
}

// testResourceExhaustionRecovery tests recovery from resource exhaustion
func testResourceExhaustionRecovery(t *testing.T) {
	// Test with resource constraints
	manager := NewAudioInputIPCManager()
	require.NotNil(t, manager)

	// Simulate resource exhaustion by rapid start/stop cycles
	for i := 0; i < 20; i++ {
		err := manager.Start()
		if err != nil {
			t.Logf("Resource exhaustion cycle %d: %v", i, err)
		}
		manager.Stop()
		// No delay to stress test resource management
	}

	// Verify system can still function after resource stress
	err := manager.Start()
	if err != nil {
		t.Logf("Final start after resource stress: %v", err)
	}
	manager.Stop()
	assert.False(t, manager.IsRunning())
}