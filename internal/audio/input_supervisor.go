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
	"sync/atomic"
	"syscall"
	"time"
)

// AudioInputSupervisor manages the audio input server subprocess
type AudioInputSupervisor struct {
	*BaseSupervisor
	client *AudioInputClient

	// Channel management
	stopChan          chan struct{}
	processDone       chan struct{}
	stopChanClosed    bool // Track if stopChan is closed
	processDoneClosed bool // Track if processDone is closed

	// Environment variables for OPUS configuration
	opusEnv []string
}

// NewAudioInputSupervisor creates a new audio input supervisor
func NewAudioInputSupervisor() *AudioInputSupervisor {
	return &AudioInputSupervisor{
		BaseSupervisor: NewBaseSupervisor("audio-input-supervisor"),
		client:         NewAudioInputClient(),
		stopChan:       make(chan struct{}),
		processDone:    make(chan struct{}),
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

// Start begins supervising the audio input server process
func (ais *AudioInputSupervisor) Start() error {
	if !atomic.CompareAndSwapInt32(&ais.running, 0, 1) {
		return fmt.Errorf("audio input supervisor is already running")
	}

	ais.logSupervisorStart()
	ais.createContext()

	// Recreate channels in case they were closed by a previous Stop() call
	ais.mutex.Lock()
	ais.processDone = make(chan struct{})
	ais.stopChan = make(chan struct{})
	ais.stopChanClosed = false    // Reset channel closed flag
	ais.processDoneClosed = false // Reset channel closed flag
	ais.mutex.Unlock()

	// Start the supervision loop
	go ais.supervisionLoop()

	ais.logger.Info().Str("component", "audio-input-supervisor").Msg("component started successfully")
	return nil
}

// supervisionLoop is the main supervision loop
func (ais *AudioInputSupervisor) supervisionLoop() {
	defer func() {
		ais.mutex.Lock()
		if !ais.processDoneClosed {
			close(ais.processDone)
			ais.processDoneClosed = true
		}
		ais.mutex.Unlock()
		ais.logger.Info().Msg("audio input server supervision ended")
	}()

	for atomic.LoadInt32(&ais.running) == 1 {
		select {
		case <-ais.stopChan:
			ais.logger.Info().Msg("received stop signal")
			ais.terminateProcess()
			return
		case <-ais.ctx.Done():
			ais.logger.Info().Msg("context cancelled")
			ais.terminateProcess()
			return
		default:
			// Start the process
			if err := ais.startProcess(); err != nil {
				ais.logger.Error().Err(err).Msg("failed to start audio input server process")
				return
			}

			// Wait for process to exit
			ais.waitForProcessExit()
			return // Single run, no restart logic for now
		}
	}
}

// startProcess starts the audio input server process
func (ais *AudioInputSupervisor) startProcess() error {
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	ais.mutex.Lock()
	defer ais.mutex.Unlock()

	// Build command arguments (only subprocess flag)
	args := []string{"--audio-input-server"}

	// Create new command
	ais.cmd = exec.CommandContext(ais.ctx, execPath, args...)
	ais.cmd.Stdout = os.Stdout
	ais.cmd.Stderr = os.Stderr

	// Set environment variables for IPC and OPUS configuration
	env := append(os.Environ(), "JETKVM_AUDIO_INPUT_IPC=true") // Enable IPC mode
	env = append(env, ais.opusEnv...)                          // Add OPUS configuration
	ais.cmd.Env = env

	// Set process group to allow clean termination
	ais.cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	// Start the process
	if err := ais.cmd.Start(); err != nil {
		return fmt.Errorf("failed to start audio input server process: %w", err)
	}

	ais.processPID = ais.cmd.Process.Pid
	ais.logger.Info().Int("pid", ais.processPID).Strs("args", args).Strs("opus_env", ais.opusEnv).Msg("audio input server process started")

	// Add process to monitoring
	ais.processMonitor.AddProcess(ais.processPID, "audio-input-server")

	// Connect client to the server
	go ais.connectClient()

	return nil
}

// waitForProcessExit waits for the current process to exit and logs the result
func (ais *AudioInputSupervisor) waitForProcessExit() {
	ais.mutex.RLock()
	cmd := ais.cmd
	pid := ais.processPID
	ais.mutex.RUnlock()

	if cmd == nil {
		return
	}

	// Wait for process to exit
	err := cmd.Wait()

	ais.mutex.Lock()
	ais.processPID = 0
	ais.mutex.Unlock()

	// Remove process from monitoring
	ais.processMonitor.RemoveProcess(pid)

	if err != nil {
		ais.logger.Error().Int("pid", pid).Err(err).Msg("audio input server process exited with error")
	} else {
		ais.logger.Info().Int("pid", pid).Msg("audio input server process exited gracefully")
	}
}

// terminateProcess gracefully terminates the current process
func (ais *AudioInputSupervisor) terminateProcess() {
	ais.mutex.RLock()
	cmd := ais.cmd
	pid := ais.processPID
	ais.mutex.RUnlock()

	if cmd == nil || cmd.Process == nil {
		return
	}

	ais.logger.Info().Int("pid", pid).Msg("terminating audio input server process")

	// Send SIGTERM first
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		ais.logger.Warn().Err(err).Int("pid", pid).Msg("failed to send SIGTERM to audio input server process")
	}

	// Wait for graceful shutdown
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()

	select {
	case <-done:
		ais.logger.Info().Int("pid", pid).Msg("audio input server process terminated gracefully")
	case <-time.After(GetConfig().InputSupervisorTimeout):
		ais.logger.Warn().Int("pid", pid).Msg("process did not terminate gracefully, sending SIGKILL")
		ais.forceKillProcess()
	}
}

// forceKillProcess forcefully kills the current process
func (ais *AudioInputSupervisor) forceKillProcess() {
	ais.mutex.RLock()
	cmd := ais.cmd
	pid := ais.processPID
	ais.mutex.RUnlock()

	if cmd == nil || cmd.Process == nil {
		return
	}

	ais.logger.Warn().Int("pid", pid).Msg("force killing audio input server process")
	if err := cmd.Process.Kill(); err != nil {
		ais.logger.Error().Err(err).Int("pid", pid).Msg("failed to kill process")
	}
}

// Stop gracefully stops the audio input server and supervisor
func (ais *AudioInputSupervisor) Stop() {
	if !atomic.CompareAndSwapInt32(&ais.running, 1, 0) {
		return // Already stopped
	}

	ais.logSupervisorStop()

	// Disconnect client first
	if ais.client != nil {
		ais.client.Disconnect()
	}

	// Signal stop and wait for cleanup
	ais.mutex.Lock()
	if !ais.stopChanClosed {
		close(ais.stopChan)
		ais.stopChanClosed = true
	}
	ais.mutex.Unlock()
	ais.cancelContext()

	// Wait for process to exit
	select {
	case <-ais.processDone:
		ais.logger.Info().Str("component", "audio-input-supervisor").Msg("component stopped gracefully")
	case <-time.After(GetConfig().InputSupervisorTimeout):
		ais.logger.Warn().Str("component", "audio-input-supervisor").Msg("component did not stop gracefully, forcing termination")
		ais.forceKillProcess()
	}

	ais.logger.Info().Str("component", "audio-input-supervisor").Msg("component stopped")
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

// connectClient attempts to connect the client to the server
func (ais *AudioInputSupervisor) connectClient() {
	// Wait briefly for the server to start and create socket
	time.Sleep(GetConfig().DefaultSleepDuration)

	// Additional small delay to ensure socket is ready after restart
	time.Sleep(20 * time.Millisecond)

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
				// PID is the first field
				if pid, err := strconv.Atoi(fields[0]); err == nil {
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
