package audio

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"time"
)

// AudioInputSupervisor manages the audio input server subprocess
type AudioInputSupervisor struct {
	*BaseSupervisor
	client *AudioInputClient

	// Environment variables for OPUS configuration
	opusEnv []string
}

// NewAudioInputSupervisor creates a new audio input supervisor
func NewAudioInputSupervisor() *AudioInputSupervisor {
	return &AudioInputSupervisor{
		BaseSupervisor: NewBaseSupervisor("audio-input-supervisor"),
		client:         NewAudioInputClient(),
	}
}

// SetOpusConfig sets OPUS configuration parameters as environment variables
// for the audio input subprocess
func (ais *AudioInputSupervisor) SetOpusConfig(bitrate, complexity, vbr, signalType, bandwidth, dtx int) {
	ais.mutex.Lock()
	defer ais.mutex.Unlock()

	// Store OPUS parameters as environment variables
	ais.opusEnv = []string{
		"JETKVM_OPUS_BITRATE=" + strconv.Itoa(bitrate),
		"JETKVM_OPUS_COMPLEXITY=" + strconv.Itoa(complexity),
		"JETKVM_OPUS_VBR=" + strconv.Itoa(vbr),
		"JETKVM_OPUS_SIGNAL_TYPE=" + strconv.Itoa(signalType),
		"JETKVM_OPUS_BANDWIDTH=" + strconv.Itoa(bandwidth),
		"JETKVM_OPUS_DTX=" + strconv.Itoa(dtx),
	}
}

// Start starts the audio input server subprocess
func (ais *AudioInputSupervisor) Start() error {
	ais.mutex.Lock()
	defer ais.mutex.Unlock()

	if ais.IsRunning() {
		return fmt.Errorf("audio input supervisor already running with PID %d", ais.cmd.Process.Pid)
	}

	// Create context for subprocess management
	ais.createContext()

	// Get current executable path
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	// Build command arguments (only subprocess flag)
	args := []string{"--audio-input-server"}

	// Create command for audio input server subprocess
	cmd := exec.CommandContext(ais.ctx, execPath, args...)

	// Set environment variables for IPC and OPUS configuration
	env := append(os.Environ(), "JETKVM_AUDIO_INPUT_IPC=true") // Enable IPC mode
	env = append(env, ais.opusEnv...)                          // Add OPUS configuration
	cmd.Env = env

	// Set process group to allow clean termination
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	ais.cmd = cmd
	ais.setRunning(true)

	// Start the subprocess
	err = cmd.Start()
	if err != nil {
		ais.setRunning(false)
		ais.cancelContext()
		return fmt.Errorf("failed to start audio input server process: %w", err)
	}

	ais.logger.Info().Int("pid", cmd.Process.Pid).Strs("args", args).Strs("opus_env", ais.opusEnv).Msg("Audio input server subprocess started")

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
	ais.mutex.Lock()
	defer ais.mutex.Unlock()

	if !ais.IsRunning() {
		return
	}

	ais.logSupervisorStop()

	// Disconnect client first
	if ais.client != nil {
		ais.client.Disconnect()
	}

	// Cancel context to signal subprocess to stop
	ais.cancelContext()

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
		case <-time.After(GetConfig().InputSupervisorTimeout):
			// Force kill if graceful shutdown failed
			ais.logger.Warn().Msg("Audio input server subprocess did not stop gracefully, force killing")
			err := ais.cmd.Process.Kill()
			if err != nil {
				ais.logger.Error().Err(err).Msg("Failed to kill audio input server subprocess")
			}
		}
	}

	ais.setRunning(false)
	ais.cmd = nil
}

// IsConnected returns whether the client is connected to the audio input server
func (ais *AudioInputSupervisor) IsConnected() bool {
	ais.mutex.Lock()
	defer ais.mutex.Unlock()
	if !ais.IsRunning() {
		return false
	}
	return ais.client.IsConnected()
}

// GetClient returns the IPC client for sending audio frames
func (ais *AudioInputSupervisor) GetClient() *AudioInputClient {
	return ais.client
}

// GetProcessMetrics returns current process metrics with audio-input-server name
func (ais *AudioInputSupervisor) GetProcessMetrics() *ProcessMetrics {
	metrics := ais.BaseSupervisor.GetProcessMetrics()
	metrics.ProcessName = "audio-input-server"
	return metrics
}

// monitorSubprocess monitors the subprocess and handles unexpected exits
func (ais *AudioInputSupervisor) monitorSubprocess() {
	if ais.cmd == nil || ais.cmd.Process == nil {
		return
	}

	pid := ais.cmd.Process.Pid
	err := ais.cmd.Wait()

	// Remove process from monitoring
	ais.processMonitor.RemoveProcess(pid)

	ais.mutex.Lock()
	defer ais.mutex.Unlock()

	if ais.IsRunning() {
		// Unexpected exit
		if err != nil {
			ais.logger.Error().Err(err).Int("pid", pid).Msg("Audio input server subprocess exited unexpectedly")
		} else {
			ais.logger.Warn().Int("pid", pid).Msg("Audio input server subprocess exited unexpectedly")
		}

		// Disconnect client
		if ais.client != nil {
			ais.client.Disconnect()
		}

		// Mark as not running
		ais.setRunning(false)
		ais.cmd = nil

		ais.logger.Info().Int("pid", pid).Msg("Audio input server subprocess monitoring stopped")
	}
}

// connectClient attempts to connect the client to the server
func (ais *AudioInputSupervisor) connectClient() {
	// Wait briefly for the server to start (reduced from 500ms)
	time.Sleep(GetConfig().DefaultSleepDuration)

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

// SendFrameZeroCopy sends a zero-copy frame to the subprocess
func (ais *AudioInputSupervisor) SendFrameZeroCopy(frame *ZeroCopyAudioFrame) error {
	if ais.client == nil {
		return fmt.Errorf("client not initialized")
	}

	if !ais.client.IsConnected() {
		return fmt.Errorf("client not connected")
	}

	return ais.client.SendFrameZeroCopy(frame)
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
