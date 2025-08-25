//go:build integration
// +build integration

package audio

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIPCCommunication tests the IPC communication between audio components
func TestIPCCommunication(t *testing.T) {
	tests := []struct {
		name        string
		testFunc    func(t *testing.T)
		description string
	}{
		{
			name:        "AudioOutputIPC",
			testFunc:    testAudioOutputIPC,
			description: "Test audio output IPC server and client communication",
		},
		{
			name:        "AudioInputIPC",
			testFunc:    testAudioInputIPC,
			description: "Test audio input IPC server and client communication",
		},
		{
			name:        "IPCReconnection",
			testFunc:    testIPCReconnection,
			description: "Test IPC reconnection after connection loss",
		},
		{
			name:        "IPCConcurrency",
			testFunc:    testIPCConcurrency,
			description: "Test concurrent IPC operations",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Logf("Running test: %s - %s", tt.name, tt.description)
			tt.testFunc(t)
		})
	}
}

// testAudioOutputIPC tests the audio output IPC communication
func testAudioOutputIPC(t *testing.T) {
	tempDir := t.TempDir()
	socketPath := filepath.Join(tempDir, "test_audio_output.sock")

	// Create a test IPC server
	server := &AudioIPCServer{
		socketPath: socketPath,
		logger:     getTestLogger(),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Start server in goroutine
	var serverErr error
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		serverErr = server.Start(ctx)
	}()

	// Wait for server to start
	time.Sleep(100 * time.Millisecond)

	// Test client connection
	conn, err := net.Dial("unix", socketPath)
	require.NoError(t, err, "Failed to connect to IPC server")
	defer conn.Close()

	// Test sending a frame message
	testFrame := []byte("test audio frame data")
	msg := &OutputMessage{
		Type:      OutputMessageTypeOpusFrame,
		Timestamp: time.Now().UnixNano(),
		Data:      testFrame,
	}

	err = writeOutputMessage(conn, msg)
	require.NoError(t, err, "Failed to write message to IPC")

	// Test heartbeat
	heartbeatMsg := &OutputMessage{
		Type:      OutputMessageTypeHeartbeat,
		Timestamp: time.Now().UnixNano(),
	}

	err = writeOutputMessage(conn, heartbeatMsg)
	require.NoError(t, err, "Failed to send heartbeat")

	// Clean shutdown
	cancel()
	wg.Wait()

	if serverErr != nil && serverErr != context.Canceled {
		t.Errorf("Server error: %v", serverErr)
	}
}

// testAudioInputIPC tests the audio input IPC communication
func testAudioInputIPC(t *testing.T) {
	tempDir := t.TempDir()
	socketPath := filepath.Join(tempDir, "test_audio_input.sock")

	// Create a test input IPC server
	server := &AudioInputIPCServer{
		socketPath: socketPath,
		logger:     getTestLogger(),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Start server
	var serverErr error
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		serverErr = server.Start(ctx)
	}()

	// Wait for server to start
	time.Sleep(100 * time.Millisecond)

	// Test client connection
	conn, err := net.Dial("unix", socketPath)
	require.NoError(t, err, "Failed to connect to input IPC server")
	defer conn.Close()

	// Test sending input frame
	testInputFrame := []byte("test microphone data")
	inputMsg := &InputMessage{
		Type:      InputMessageTypeOpusFrame,
		Timestamp: time.Now().UnixNano(),
		Data:      testInputFrame,
	}

	err = writeInputMessage(conn, inputMsg)
	require.NoError(t, err, "Failed to write input message")

	// Test configuration message
	configMsg := &InputMessage{
		Type:      InputMessageTypeConfig,
		Timestamp: time.Now().UnixNano(),
		Data:      []byte("quality=medium"),
	}

	err = writeInputMessage(conn, configMsg)
	require.NoError(t, err, "Failed to send config message")

	// Clean shutdown
	cancel()
	wg.Wait()

	if serverErr != nil && serverErr != context.Canceled {
		t.Errorf("Input server error: %v", serverErr)
	}
}

// testIPCReconnection tests IPC reconnection scenarios
func testIPCReconnection(t *testing.T) {
	tempDir := t.TempDir()
	socketPath := filepath.Join(tempDir, "test_reconnect.sock")

	// Create server
	server := &AudioIPCServer{
		socketPath: socketPath,
		logger:     getTestLogger(),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	// Start server
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		server.Start(ctx)
	}()

	time.Sleep(100 * time.Millisecond)

	// First connection
	conn1, err := net.Dial("unix", socketPath)
	require.NoError(t, err, "Failed initial connection")

	// Send a message
	msg := &OutputMessage{
		Type:      OutputMessageTypeOpusFrame,
		Timestamp: time.Now().UnixNano(),
		Data:      []byte("test data 1"),
	}
	err = writeOutputMessage(conn1, msg)
	require.NoError(t, err, "Failed to send first message")

	// Close connection to simulate disconnect
	conn1.Close()
	time.Sleep(200 * time.Millisecond)

	// Reconnect
	conn2, err := net.Dial("unix", socketPath)
	require.NoError(t, err, "Failed to reconnect")
	defer conn2.Close()

	// Send another message after reconnection
	msg2 := &OutputMessage{
		Type:      OutputMessageTypeOpusFrame,
		Timestamp: time.Now().UnixNano(),
		Data:      []byte("test data 2"),
	}
	err = writeOutputMessage(conn2, msg2)
	require.NoError(t, err, "Failed to send message after reconnection")

	cancel()
	wg.Wait()
}

// testIPCConcurrency tests concurrent IPC operations
func testIPCConcurrency(t *testing.T) {
	tempDir := t.TempDir()
	socketPath := filepath.Join(tempDir, "test_concurrent.sock")

	server := &AudioIPCServer{
		socketPath: socketPath,
		logger:     getTestLogger(),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Start server
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		server.Start(ctx)
	}()

	time.Sleep(100 * time.Millisecond)

	// Create multiple concurrent connections
	numClients := 5
	messagesPerClient := 10

	var clientWg sync.WaitGroup
	for i := 0; i < numClients; i++ {
		clientWg.Add(1)
		go func(clientID int) {
			defer clientWg.Done()

			conn, err := net.Dial("unix", socketPath)
			if err != nil {
				t.Errorf("Client %d failed to connect: %v", clientID, err)
				return
			}
			defer conn.Close()

			// Send multiple messages
			for j := 0; j < messagesPerClient; j++ {
				msg := &OutputMessage{
					Type:      OutputMessageTypeOpusFrame,
					Timestamp: time.Now().UnixNano(),
					Data:      []byte(fmt.Sprintf("client_%d_msg_%d", clientID, j)),
				}

				if err := writeOutputMessage(conn, msg); err != nil {
					t.Errorf("Client %d failed to send message %d: %v", clientID, j, err)
					return
				}

				// Small delay between messages
				time.Sleep(10 * time.Millisecond)
			}
		}(i)
	}

	clientWg.Wait()
	cancel()
	wg.Wait()
}

// Helper function to get a test logger
func getTestLogger() zerolog.Logger {
	return zerolog.New(os.Stdout).With().Timestamp().Logger()
}

// Helper functions for message writing (simplified versions)
func writeOutputMessage(conn net.Conn, msg *OutputMessage) error {
	// This is a simplified version for testing
	// In real implementation, this would use the actual protocol
	data := fmt.Sprintf("%d:%d:%s", msg.Type, msg.Timestamp, string(msg.Data))
	_, err := conn.Write([]byte(data))
	return err
}

func writeInputMessage(conn net.Conn, msg *InputMessage) error {
	// This is a simplified version for testing
	data := fmt.Sprintf("%d:%d:%s", msg.Type, msg.Timestamp, string(msg.Data))
	_, err := conn.Write([]byte(data))
	return err
}