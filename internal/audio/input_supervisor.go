//go:build cgo
// +build cgo

package audio

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// AudioInputSupervisor manages the audio input server subprocess
type AudioInputSupervisor struct {
	*BaseSupervisor
	client *AudioInputClient

	// Environment variables for OPUS configuration
	opusEnv []string

	// Pre-warming state
	prewarmed   bool
	prewarmTime time.Time
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

// PrewarmSubprocess starts a subprocess in advance to reduce activation latency
func (ais *AudioInputSupervisor) PrewarmSubprocess() error {
	ais.mutex.Lock()
	defer ais.mutex.Unlock()

	// Don't prewarm if already running or prewarmed
	if ais.IsRunning() || ais.prewarmed {
		return nil
	}

	// Check for existing audio input server process first
	if existingPID, err := ais.findExistingAudioInputProcess(); err == nil {
		ais.logger.Info().Int("existing_pid", existingPID).Msg("Found existing audio input server process for prewarming")
		ais.prewarmed = true
		ais.prewarmTime = time.Now()
		return nil
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

	// Start the subprocess
	err = cmd.Start()
	if err != nil {
		ais.cancelContext()
		return fmt.Errorf("failed to prewarm audio input server process: %w", err)
	}

	ais.logger.Info().Int("pid", cmd.Process.Pid).Strs("args", args).Strs("opus_env", ais.opusEnv).Msg("Audio input server subprocess prewarmed")

	// Add process to monitoring
	ais.processMonitor.AddProcess(cmd.Process.Pid, "audio-input-server")

	// Monitor the subprocess in a goroutine
	go ais.monitorSubprocess()

	// Mark as prewarmed
	ais.prewarmed = true
	ais.prewarmTime = time.Now()

	return nil
}

// Start starts the audio input server subprocess
func (ais *AudioInputSupervisor) Start() error {
	ais.mutex.Lock()
	defer ais.mutex.Unlock()

	if ais.IsRunning() {
		if ais.cmd != nil && ais.cmd.Process != nil {
			return fmt.Errorf("audio input supervisor already running with PID %d", ais.cmd.Process.Pid)
		}
		return fmt.Errorf("audio input supervisor already running")
	}

	// Use prewarmed subprocess if available
	if ais.prewarmed && ais.cmd != nil && ais.cmd.Process != nil {
		ais.logger.Info().Int("pid", ais.cmd.Process.Pid).Dur("prewarm_age", time.Since(ais.prewarmTime)).Msg("Using prewarmed audio input server subprocess")
		ais.setRunning(true)
		ais.prewarmed = false // Reset prewarmed state
		// Connect client to the server
		go ais.connectClient()
		return nil
	}

	// Check for existing audio input server process
	if existingPID, err := ais.findExistingAudioInputProcess(); err == nil {
		ais.logger.Info().Int("existing_pid", existingPID).Msg("Found existing audio input server process, connecting to it")

		// Try to connect to the existing process
		ais.setRunning(true)
		go ais.connectClient()
		return nil
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

// IsPrewarmed returns whether a subprocess is prewarmed and ready
func (ais *AudioInputSupervisor) IsPrewarmed() bool {
	ais.mutex.RLock()
	defer ais.mutex.RUnlock()
	return ais.prewarmed
}

// GetPrewarmAge returns how long ago the subprocess was prewarmed
func (ais *AudioInputSupervisor) GetPrewarmAge() time.Duration {
	ais.mutex.RLock()
	defer ais.mutex.RUnlock()
	if !ais.prewarmed {
		return 0
	}
	return time.Since(ais.prewarmTime)
}

// Stop stops the audio input server subprocess
func (ais *AudioInputSupervisor) Stop() {
	ais.mutex.Lock()
	defer ais.mutex.Unlock()

	// Reset prewarmed state
	ais.prewarmed = false

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
		pid := ais.cmd.Process.Pid
		ais.logger.Info().Int("pid", pid).Msg("Stopping audio input server subprocess")

		// Send SIGTERM
		err := ais.cmd.Process.Signal(syscall.SIGTERM)
		if err != nil {
			ais.logger.Warn().Err(err).Msg("Failed to send SIGTERM to audio input server")
		}

		// Wait for graceful shutdown with timeout
		done := make(chan error, 1)
		var waitErr error
		go func() {
			waitErr = ais.cmd.Wait()
			done <- waitErr
		}()

		select {
		case <-done:
			if waitErr != nil {
				ais.logger.Info().Err(waitErr).Msg("Audio input server subprocess stopped with error")
			} else {
				ais.logger.Info().Msg("Audio input server subprocess stopped gracefully")
			}
		case <-time.After(GetConfig().InputSupervisorTimeout):
			// Force kill if graceful shutdown failed
			ais.logger.Warn().Msg("Audio input server subprocess did not stop gracefully, force killing")
			// Use a more robust approach to check if process is still alive
			if ais.cmd != nil && ais.cmd.Process != nil {
				// Try to send signal 0 to check if process exists
				if err := ais.cmd.Process.Signal(syscall.Signal(0)); err == nil {
					// Process is still alive, force kill it
					if killErr := ais.cmd.Process.Kill(); killErr != nil {
						// Only log error if it's not "process already finished"
						if !strings.Contains(killErr.Error(), "process already finished") {
							ais.logger.Error().Err(killErr).Msg("Failed to kill audio input server subprocess")
						} else {
							ais.logger.Debug().Msg("Audio input server subprocess already finished during kill attempt")
						}
					} else {
						ais.logger.Info().Msg("Audio input server subprocess force killed successfully")
					}
				} else {
					ais.logger.Debug().Msg("Audio input server subprocess already finished")
				}
				// Wait a bit for the kill to take effect and collect the exit status
				go func() {
					select {
					case <-done:
						// Process finished
					case <-time.After(1 * time.Second):
						// Give up waiting
					}
				}()
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

		// Mark as not running first to prevent race conditions
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

// SendOpusConfig sends a complete Opus encoder configuration to the audio input server
func (ais *AudioInputSupervisor) SendOpusConfig(config InputIPCOpusConfig) error {
	if ais.client == nil {
		return fmt.Errorf("client not initialized")
	}

	if !ais.client.IsConnected() {
		return fmt.Errorf("client not connected")
	}

	return ais.client.SendOpusConfig(config)
}

// findExistingAudioInputProcess checks if there's already an audio input server process running
func (ais *AudioInputSupervisor) findExistingAudioInputProcess() (int, error) {
	// Get current executable path
	execPath, err := os.Executable()
	if err != nil {
		return 0, fmt.Errorf("failed to get executable path: %w", err)
	}

	execName := filepath.Base(execPath)

	// Use ps to find processes with our executable name and audio-input-server argument
	cmd := exec.Command("ps", "aux")
	output, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("failed to run ps command: %w", err)
	}

	// Parse ps output to find audio input server processes
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.Contains(line, execName) && strings.Contains(line, "--audio-input-server") {
			// Extract PID from ps output (second column)
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				if pid, err := strconv.Atoi(fields[1]); err == nil {
					// Verify the process is still running and accessible
					if ais.isProcessRunning(pid) {
						return pid, nil
					}
				}
			}
		}
	}

	return 0, fmt.Errorf("no existing audio input server process found")
}

// isProcessRunning checks if a process with the given PID is still running
func (ais *AudioInputSupervisor) isProcessRunning(pid int) bool {
	// Try to send signal 0 to check if process exists
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}

	err = process.Signal(syscall.Signal(0))
	return err == nil
}

// HasExistingProcess checks if there's already an audio input server process running
// This is a public wrapper around findExistingAudioInputProcess for external access
func (ais *AudioInputSupervisor) HasExistingProcess() (int, bool) {
	pid, err := ais.findExistingAudioInputProcess()
	return pid, err == nil
}
