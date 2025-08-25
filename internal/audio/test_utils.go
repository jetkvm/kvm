//go:build integration
// +build integration

package audio

import (
	"context"
	"net"
	"os"
	"sync"
	"time"

	"github.com/jetkvm/kvm/internal/logging"
	"github.com/rs/zerolog"
)

// Test utilities and mock implementations for integration tests

// MockAudioIPCServer provides a mock IPC server for testing
type AudioIPCServer struct {
	socketPath string
	logger     zerolog.Logger
	listener   net.Listener
	connections map[net.Conn]bool
	mu         sync.RWMutex
	running    bool
}

// Start starts the mock IPC server
func (s *AudioIPCServer) Start(ctx context.Context) error {
	// Remove existing socket file
	os.Remove(s.socketPath)

	listener, err := net.Listen("unix", s.socketPath)
	if err != nil {
		return err
	}
	s.listener = listener
	s.connections = make(map[net.Conn]bool)

	s.mu.Lock()
	s.running = true
	s.mu.Unlock()

	go s.acceptConnections(ctx)

	<-ctx.Done()
	s.Stop()
	return ctx.Err()
}

// Stop stops the mock IPC server
func (s *AudioIPCServer) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return
	}

	s.running = false

	if s.listener != nil {
		s.listener.Close()
	}

	// Close all connections
	for conn := range s.connections {
		conn.Close()
	}

	// Clean up socket file
	os.Remove(s.socketPath)
}

// acceptConnections handles incoming connections
func (s *AudioIPCServer) acceptConnections(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return
			default:
				s.logger.Error().Err(err).Msg("Failed to accept connection")
				continue
			}
		}

		s.mu.Lock()
		s.connections[conn] = true
		s.mu.Unlock()

		go s.handleConnection(ctx, conn)
	}
}

// handleConnection handles a single connection
func (s *AudioIPCServer) handleConnection(ctx context.Context, conn net.Conn) {
	defer func() {
		s.mu.Lock()
		delete(s.connections, conn)
		s.mu.Unlock()
		conn.Close()
	}()

	buffer := make([]byte, 4096)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Set read timeout
		conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		n, err := conn.Read(buffer)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			return
		}

		// Process received data (for testing, we just log it)
		s.logger.Debug().Int("bytes", n).Msg("Received data from client")
	}
}

// AudioInputIPCServer provides a mock input IPC server
type AudioInputIPCServer struct {
	*AudioIPCServer
}

// Test message structures
type OutputMessage struct {
	Type      OutputMessageType
	Timestamp int64
	Data      []byte
}

type InputMessage struct {
	Type      InputMessageType
	Timestamp int64
	Data      []byte
}

// Test configuration helpers
func getTestConfig() *AudioConfigConstants {
	return &AudioConfigConstants{
		// Basic audio settings
		SampleRate: 48000,
		Channels:   2,
		MaxAudioFrameSize: 4096,

		// IPC settings
		OutputMagicNumber: 0x4A4B4F55, // "JKOU"
		InputMagicNumber:  0x4A4B4D49, // "JKMI"
		WriteTimeout:      5 * time.Second,
		HeaderSize:        17,
		MaxFrameSize:      4096,
		MessagePoolSize:   100,

		// Supervisor settings
		MaxRestartAttempts:    3,
		InitialRestartDelay:   1 * time.Second,
		MaxRestartDelay:       30 * time.Second,
		HealthCheckInterval:   5 * time.Second,

		// Quality presets
		AudioQualityLowOutputBitrate:    32000,
		AudioQualityMediumOutputBitrate: 96000,
		AudioQualityHighOutputBitrate:   192000,
		AudioQualityUltraOutputBitrate:  320000,

		AudioQualityLowInputBitrate:    16000,
		AudioQualityMediumInputBitrate: 64000,
		AudioQualityHighInputBitrate:   128000,
		AudioQualityUltraInputBitrate:  256000,

		AudioQualityLowSampleRate:    24000,
		AudioQualityMediumSampleRate: 48000,
		AudioQualityHighSampleRate:   48000,
		AudioQualityUltraSampleRate:  48000,

		AudioQualityLowChannels:    1,
		AudioQualityMediumChannels: 2,
		AudioQualityHighChannels:   2,
		AudioQualityUltraChannels:  2,

		AudioQualityLowFrameSize:    20 * time.Millisecond,
		AudioQualityMediumFrameSize: 20 * time.Millisecond,
		AudioQualityHighFrameSize:   20 * time.Millisecond,
		AudioQualityUltraFrameSize:  20 * time.Millisecond,

		AudioQualityMicLowSampleRate: 16000,

		// Metrics settings
		MetricsUpdateInterval: 1 * time.Second,

		// Latency settings
		DefaultTargetLatencyMS:             50,
		DefaultOptimizationIntervalSeconds: 5,
		DefaultAdaptiveThreshold:           0.8,
		DefaultStatsIntervalSeconds:        5,

		// Buffer settings
		DefaultBufferPoolSize:     100,
		DefaultControlPoolSize:    50,
		DefaultFramePoolSize:      200,
		DefaultMaxPooledFrames:    500,
		DefaultPoolCleanupInterval: 30 * time.Second,

		// Process monitoring
		MaxCPUPercent:        100.0,
		MinCPUPercent:        0.0,
		DefaultClockTicks:    100,
		DefaultMemoryGB:      4.0,
		MaxWarmupSamples:     10,
		WarmupCPUSamples:     5,
		MetricsChannelBuffer: 100,
		MinValidClockTicks:   50,
		MaxValidClockTicks:   1000,
		PageSize:             4096,

		// CGO settings (for cgo builds)
		CGOOpusBitrate:       96000,
		CGOOpusComplexity:    3,
		CGOOpusVBR:           1,
		CGOOpusVBRConstraint: 1,
		CGOOpusSignalType:    3,
		CGOOpusBandwidth:     1105,
		CGOOpusDTX:           0,
		CGOSampleRate:        48000,

		// Batch processing
		BatchProcessorFramesPerBatch: 10,
		BatchProcessorTimeout:        100 * time.Millisecond,

		// Granular metrics
		GranularMetricsMaxSamples:    1000,
		GranularMetricsLogInterval:   30 * time.Second,
		GranularMetricsCleanupInterval: 5 * time.Minute,
	}
}

// setupTestEnvironment sets up the test environment
func setupTestEnvironment() {
	// Use test configuration
	UpdateConfig(getTestConfig())

	// Initialize logging for tests
	logging.SetLevel("debug")
}

// cleanupTestEnvironment cleans up after tests
func cleanupTestEnvironment() {
	// Reset to default configuration
	UpdateConfig(DefaultAudioConfig())
}

// createTestLogger creates a logger for testing
func createTestLogger(name string) zerolog.Logger {
	return zerolog.New(os.Stdout).With().
		Timestamp().
		Str("component", name).
		Str("test", "true").
		Logger()
}

// waitForCondition waits for a condition to be true with timeout
func waitForCondition(condition func() bool, timeout time.Duration, checkInterval time.Duration) bool {
	timeout_timer := time.NewTimer(timeout)
	defer timeout_timer.Stop()

	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-timeout_timer.C:
			return false
		case <-ticker.C:
			if condition() {
				return true
			}
		}
	}
}

// TestHelper provides common test functionality
type TestHelper struct {
	tempDir string
	logger  zerolog.Logger
}

// NewTestHelper creates a new test helper
func NewTestHelper(tempDir string) *TestHelper {
	return &TestHelper{
		tempDir: tempDir,
		logger:  createTestLogger("test-helper"),
	}
}

// CreateTempSocket creates a temporary socket path
func (h *TestHelper) CreateTempSocket(name string) string {
	return filepath.Join(h.tempDir, name)
}

// GetLogger returns the test logger
func (h *TestHelper) GetLogger() zerolog.Logger {
	return h.logger
}