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

// Start begins supervising the audio input server process
func (ais *AudioInputSupervisor) Start() error {
	if !atomic.CompareAndSwapInt32(&ais.running, 0, 1) {
		return fmt.Errorf("audio input supervisor is already running")
	}

	ais.logSupervisorStart()
	ais.createContext()

	// Recreate channels in case they were closed by a previous Stop() call
	ais.initializeChannels()

	// Start the supervision loop
	go ais.supervisionLoop()

	ais.logger.Info().Str("component", "audio-input-supervisor").Msg("component started successfully")
	return nil
}

// supervisionLoop is the main supervision loop
func (ais *AudioInputSupervisor) supervisionLoop() {
	// Configure supervision parameters (no restart for input supervisor)
	config := SupervisionConfig{
		ProcessType:        "audio input server",
		Timeout:            Config.InputSupervisorTimeout,
		EnableRestart:      false, // Input supervisor doesn't restart
		MaxRestartAttempts: 0,
		RestartWindow:      0,
		RestartDelay:       0,
		MaxRestartDelay:    0,
	}

	// Configure callbacks (input supervisor doesn't have callbacks currently)
	callbacks := ProcessCallbacks{
		OnProcessStart: nil,
		OnProcessExit:  nil,
		OnRestart:      nil,
	}

	// Use the base supervision loop template
	ais.SupervisionLoop(
		config,
		callbacks,
		ais.startProcess,
		func() bool { return false },      // Never restart
		func() time.Duration { return 0 }, // No restart delay needed
	)
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

	// Connect client to the server synchronously to avoid race condition
	ais.connectClient()

	return nil
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
	ais.closeStopChan()
	ais.cancelContext()

	// Wait for process to exit
	select {
	case <-ais.processDone:
		ais.logger.Info().Str("component", "audio-input-supervisor").Msg("component stopped gracefully")
	case <-time.After(Config.InputSupervisorTimeout):
		ais.logger.Warn().Str("component", "audio-input-supervisor").Msg("component did not stop gracefully, forcing termination")
		ais.forceKillProcess("audio input server")
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
	time.Sleep(Config.DefaultSleepDuration)

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
