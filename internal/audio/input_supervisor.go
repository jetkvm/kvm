package audio

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/jetkvm/kvm/internal/logging"
	"github.com/rs/zerolog"
)

// AudioInputSupervisor manages the audio input server subprocess
type AudioInputSupervisor struct {
	cmd            *exec.Cmd
	cancel         context.CancelFunc
	mtx            sync.Mutex
	running        bool
	logger         zerolog.Logger
	client         *AudioInputClient
	processMonitor *ProcessMonitor
}

// NewAudioInputSupervisor creates a new audio input supervisor
func NewAudioInputSupervisor() *AudioInputSupervisor {
	return &AudioInputSupervisor{
		logger:         logging.GetDefaultLogger().With().Str("component", "audio-input-supervisor").Logger(),
		client:         NewAudioInputClient(),
		processMonitor: GetProcessMonitor(),
	}
}

// Start starts the audio input server subprocess
func (ais *AudioInputSupervisor) Start() error {
	ais.mtx.Lock()
	defer ais.mtx.Unlock()

	if ais.running {
		return fmt.Errorf("audio input supervisor already running")
	}

	// Create context for subprocess management
	ctx, cancel := context.WithCancel(context.Background())
	ais.cancel = cancel

	// Get current executable path
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	// Create command for audio input server subprocess
	cmd := exec.CommandContext(ctx, execPath)
	cmd.Env = append(os.Environ(),
		"JETKVM_AUDIO_INPUT_SERVER=true", // Flag to indicate this is the input server process
		"JETKVM_AUDIO_INPUT_IPC=true",    // Enable IPC mode
	)

	// Set process group to allow clean termination
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	ais.cmd = cmd
	ais.running = true

	// Start the subprocess
	err = cmd.Start()
	if err != nil {
		ais.running = false
		cancel()
		return fmt.Errorf("failed to start audio input server: %w", err)
	}

	ais.logger.Info().Int("pid", cmd.Process.Pid).Msg("Audio input server subprocess started")

	// Add process to monitoring
	ais.processMonitor.AddProcess(cmd.Process.Pid, "audio-input-server")

	// Monitor the subprocess in a goroutine
	go ais.monitorSubprocess()

	// Connect client to the server
	go ais.connectClient()

	return nil
}

// Stop stops the audio input server subprocess
func (ais *AudioInputSupervisor) Stop() {
	ais.mtx.Lock()
	defer ais.mtx.Unlock()

	if !ais.running {
		return
	}

	ais.running = false

	// Disconnect client first
	if ais.client != nil {
		ais.client.Disconnect()
	}

	// Cancel context to signal subprocess to stop
	if ais.cancel != nil {
		ais.cancel()
	}

	// Try graceful termination first
	if ais.cmd != nil && ais.cmd.Process != nil {
		ais.logger.Info().Int("pid", ais.cmd.Process.Pid).Msg("Stopping audio input server subprocess")

		// Send SIGTERM
		err := ais.cmd.Process.Signal(syscall.SIGTERM)
		if err != nil {
			ais.logger.Warn().Err(err).Msg("Failed to send SIGTERM to audio input server")
		}

		// Wait for graceful shutdown with timeout
		done := make(chan error, 1)
		go func() {
			done <- ais.cmd.Wait()
		}()

		select {
		case <-done:
			ais.logger.Info().Msg("Audio input server subprocess stopped gracefully")
		case <-time.After(5 * time.Second):
			// Force kill if graceful shutdown failed
			ais.logger.Warn().Msg("Audio input server subprocess did not stop gracefully, force killing")
			err := ais.cmd.Process.Kill()
			if err != nil {
				ais.logger.Error().Err(err).Msg("Failed to kill audio input server subprocess")
			}
		}
	}

	ais.cmd = nil
	ais.cancel = nil
}

// IsRunning returns whether the supervisor is running
func (ais *AudioInputSupervisor) IsRunning() bool {
	ais.mtx.Lock()
	defer ais.mtx.Unlock()
	return ais.running
}

// IsConnected returns whether the client is connected to the audio input server
func (ais *AudioInputSupervisor) IsConnected() bool {
	if !ais.IsRunning() {
		return false
	}
	return ais.client.IsConnected()
}

// GetClient returns the IPC client for sending audio frames
func (ais *AudioInputSupervisor) GetClient() *AudioInputClient {
	return ais.client
}

// GetProcessMetrics returns current process metrics if the process is running
func (ais *AudioInputSupervisor) GetProcessMetrics() *ProcessMetrics {
	ais.mtx.Lock()
	defer ais.mtx.Unlock()

	if ais.cmd == nil || ais.cmd.Process == nil {
		return nil
	}

	pid := ais.cmd.Process.Pid
	metrics := ais.processMonitor.GetCurrentMetrics()
	for _, metric := range metrics {
		if metric.PID == pid {
			return &metric
		}
	}
	return nil
}

// monitorSubprocess monitors the subprocess and handles unexpected exits
func (ais *AudioInputSupervisor) monitorSubprocess() {
	if ais.cmd == nil {
		return
	}

	pid := ais.cmd.Process.Pid
	err := ais.cmd.Wait()

	// Remove process from monitoring
	ais.processMonitor.RemoveProcess(pid)

	ais.mtx.Lock()
	defer ais.mtx.Unlock()

	if ais.running {
		// Unexpected exit
		if err != nil {
			ais.logger.Error().Err(err).Msg("Audio input server subprocess exited unexpectedly")
		} else {
			ais.logger.Warn().Msg("Audio input server subprocess exited unexpectedly")
		}

		// Disconnect client
		if ais.client != nil {
			ais.client.Disconnect()
		}

		// Mark as not running
		ais.running = false
		ais.cmd = nil

		ais.logger.Info().Msg("Audio input server subprocess monitoring stopped")
	}
}

// connectClient attempts to connect the client to the server
func (ais *AudioInputSupervisor) connectClient() {
	// Wait briefly for the server to start (reduced from 500ms)
	time.Sleep(100 * time.Millisecond)

	err := ais.client.Connect()
	if err != nil {
		ais.logger.Error().Err(err).Msg("Failed to connect to audio input server")
		return
	}

	ais.logger.Info().Msg("Connected to audio input server")
}

// SendFrame sends an audio frame to the subprocess (convenience method)
func (ais *AudioInputSupervisor) SendFrame(frame []byte) error {
	if ais.client == nil {
		return fmt.Errorf("client not initialized")
	}

	if !ais.client.IsConnected() {
		return fmt.Errorf("client not connected")
	}

	return ais.client.SendFrame(frame)
}

// SendConfig sends a configuration update to the subprocess (convenience method)
func (ais *AudioInputSupervisor) SendConfig(config InputIPCConfig) error {
	if ais.client == nil {
		return fmt.Errorf("client not initialized")
	}

	if !ais.client.IsConnected() {
		return fmt.Errorf("client not connected")
	}

	return ais.client.SendConfig(config)
}
