//go:build cgo
// +build cgo

package audio

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"sync/atomic"
	"time"
)

// Component name constants for logging
const (
	AudioOutputSupervisorComponent = "audio-output-supervisor"
)

// Restart configuration is now retrieved from centralized config
func getMaxRestartAttempts() int {
	return Config.MaxRestartAttempts
}

func getRestartWindow() time.Duration {
	return Config.RestartWindow
}

func getRestartDelay() time.Duration {
	return Config.RestartDelay
}

func getMaxRestartDelay() time.Duration {
	return Config.MaxRestartDelay
}

// AudioOutputSupervisor manages the audio output server subprocess lifecycle
type AudioOutputSupervisor struct {
	*BaseSupervisor

	// Restart management
	restartAttempts []time.Time

	// Environment variables for OPUS configuration
	opusEnv []string

	// Callbacks
	onProcessStart func(pid int)
	onProcessExit  func(pid int, exitCode int, crashed bool)
	onRestart      func(attempt int, delay time.Duration)
}

// NewAudioOutputSupervisor creates a new audio output server supervisor
func NewAudioOutputSupervisor() *AudioOutputSupervisor {
	return &AudioOutputSupervisor{
		BaseSupervisor:  NewBaseSupervisor("audio-output-supervisor"),
		restartAttempts: make([]time.Time, 0),
	}
}

// SetCallbacks sets optional callbacks for process lifecycle events
func (s *AudioOutputSupervisor) SetCallbacks(
	onStart func(pid int),
	onExit func(pid int, exitCode int, crashed bool),
	onRestart func(attempt int, delay time.Duration),
) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	s.onProcessStart = onStart

	// Wrap the exit callback to include restart tracking
	if onExit != nil {
		s.onProcessExit = func(pid int, exitCode int, crashed bool) {
			if crashed {
				s.recordRestartAttempt()
			}
			onExit(pid, exitCode, crashed)
		}
	} else {
		s.onProcessExit = func(pid int, exitCode int, crashed bool) {
			if crashed {
				s.recordRestartAttempt()
			}
		}
	}

	s.onRestart = onRestart
}

// SetOpusConfig sets OPUS configuration parameters as environment variables
// for the audio output subprocess
func (s *AudioOutputSupervisor) SetOpusConfig(bitrate, complexity, vbr, signalType, bandwidth, dtx int) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	// Store OPUS parameters as environment variables
	s.opusEnv = []string{
		"JETKVM_OPUS_BITRATE=" + strconv.Itoa(bitrate),
		"JETKVM_OPUS_COMPLEXITY=" + strconv.Itoa(complexity),
		"JETKVM_OPUS_VBR=" + strconv.Itoa(vbr),
		"JETKVM_OPUS_SIGNAL_TYPE=" + strconv.Itoa(signalType),
		"JETKVM_OPUS_BANDWIDTH=" + strconv.Itoa(bandwidth),
		"JETKVM_OPUS_DTX=" + strconv.Itoa(dtx),
	}
}

// Start begins supervising the audio output server process
func (s *AudioOutputSupervisor) Start() error {
	if !atomic.CompareAndSwapInt32(&s.running, 0, 1) {
		return fmt.Errorf("audio output supervisor is already running")
	}

	s.logSupervisorStart()
	s.createContext()

	// Recreate channels in case they were closed by a previous Stop() call
	s.initializeChannels()

	// Reset restart tracking on start
	s.mutex.Lock()
	s.restartAttempts = s.restartAttempts[:0]
	s.mutex.Unlock()

	// Start the supervision loop
	go s.supervisionLoop()

	// Establish IPC connection to subprocess after a brief delay
	go func() {
		time.Sleep(500 * time.Millisecond) // Wait for subprocess to start
		s.connectClient()
	}()

	s.logger.Info().Str("component", AudioOutputSupervisorComponent).Msg("component started successfully")
	return nil
}

// Stop gracefully stops the audio server and supervisor
func (s *AudioOutputSupervisor) Stop() {
	if !atomic.CompareAndSwapInt32(&s.running, 1, 0) {
		return // Already stopped
	}

	s.logSupervisorStop()

	// Signal stop and wait for cleanup
	s.closeStopChan()
	s.cancelContext()

	// Wait for process to exit
	select {
	case <-s.processDone:
		s.logger.Info().Str("component", AudioOutputSupervisorComponent).Msg("component stopped gracefully")
	case <-time.After(Config.OutputSupervisorTimeout):
		s.logger.Warn().Str("component", AudioOutputSupervisorComponent).Msg("component did not stop gracefully, forcing termination")
		s.forceKillProcess("audio output server")
	}

	// Ensure socket file cleanup even if subprocess didn't clean up properly
	// This prevents "address already in use" errors on restart
	outputSocketPath := getOutputSocketPath()
	if err := os.Remove(outputSocketPath); err != nil && !os.IsNotExist(err) {
		s.logger.Warn().Err(err).Str("socket_path", outputSocketPath).Msg("failed to remove output socket file during supervisor stop")
	} else if err == nil {
		s.logger.Debug().Str("socket_path", outputSocketPath).Msg("cleaned up output socket file")
	}

	s.logger.Info().Str("component", AudioOutputSupervisorComponent).Msg("component stopped")
}

// supervisionLoop is the main loop that manages the audio output process
func (s *AudioOutputSupervisor) supervisionLoop() {
	// Configure supervision parameters
	config := SupervisionConfig{
		ProcessType:        "audio output server",
		Timeout:            Config.OutputSupervisorTimeout,
		EnableRestart:      true,
		MaxRestartAttempts: getMaxRestartAttempts(),
		RestartWindow:      getRestartWindow(),
		RestartDelay:       getRestartDelay(),
		MaxRestartDelay:    getMaxRestartDelay(),
	}

	// Configure callbacks
	callbacks := ProcessCallbacks{
		OnProcessStart: s.onProcessStart,
		OnProcessExit:  s.onProcessExit,
		OnRestart:      s.onRestart,
	}

	// Use the base supervision loop template
	s.SupervisionLoop(
		config,
		callbacks,
		s.startProcess,
		s.shouldRestart,
		s.calculateRestartDelay,
	)
}

// startProcess starts the audio server process
func (s *AudioOutputSupervisor) startProcess() error {
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	s.mutex.Lock()
	defer s.mutex.Unlock()

	// Build command arguments (only subprocess flag)
	args := []string{"--audio-output-server"}

	// Create new command
	s.cmd = exec.CommandContext(s.ctx, execPath, args...)
	s.cmd.Stdout = os.Stdout
	s.cmd.Stderr = os.Stderr

	// Set environment variables for OPUS configuration
	s.cmd.Env = append(os.Environ(), s.opusEnv...)

	// Start the process
	if err := s.cmd.Start(); err != nil {
		return fmt.Errorf("failed to start audio output server process: %w", err)
	}

	s.processPID = s.cmd.Process.Pid
	s.logger.Info().Int("pid", s.processPID).Strs("args", args).Strs("opus_env", s.opusEnv).Msg("audio server process started")

	// Add process to monitoring

	if s.onProcessStart != nil {
		s.onProcessStart(s.processPID)
	}

	return nil
}

// shouldRestart determines if the process should be restarted
func (s *AudioOutputSupervisor) shouldRestart() bool {
	if atomic.LoadInt32(&s.running) == 0 {
		return false // Supervisor is stopping
	}

	s.mutex.RLock()
	defer s.mutex.RUnlock()

	// Clean up old restart attempts outside the window
	now := time.Now()
	var recentAttempts []time.Time
	for _, attempt := range s.restartAttempts {
		if now.Sub(attempt) < getRestartWindow() {
			recentAttempts = append(recentAttempts, attempt)
		}
	}
	s.restartAttempts = recentAttempts

	return len(s.restartAttempts) < getMaxRestartAttempts()
}

// recordRestartAttempt records a restart attempt
func (s *AudioOutputSupervisor) recordRestartAttempt() {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	s.restartAttempts = append(s.restartAttempts, time.Now())
}

// calculateRestartDelay calculates the delay before next restart attempt
func (s *AudioOutputSupervisor) calculateRestartDelay() time.Duration {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	// Exponential backoff based on recent restart attempts
	attempts := len(s.restartAttempts)
	if attempts == 0 {
		return getRestartDelay()
	}

	// Calculate exponential backoff: 2^attempts * base delay
	delay := getRestartDelay()
	for i := 0; i < attempts && delay < getMaxRestartDelay(); i++ {
		delay *= 2
	}

	if delay > getMaxRestartDelay() {
		delay = getMaxRestartDelay()
	}

	return delay
}

// client holds the IPC client for communicating with the subprocess
var outputClient *AudioOutputClient

// IsConnected returns whether the supervisor has an active connection to the subprocess
func (s *AudioOutputSupervisor) IsConnected() bool {
	return outputClient != nil && outputClient.IsConnected()
}

// GetClient returns the IPC client for the subprocess
func (s *AudioOutputSupervisor) GetClient() *AudioOutputClient {
	return outputClient
}

// connectClient establishes connection to the audio output subprocess
func (s *AudioOutputSupervisor) connectClient() {
	if outputClient == nil {
		outputClient = NewAudioOutputClient()
	}

	// Try to connect to the subprocess
	if err := outputClient.Connect(); err != nil {
		s.logger.Warn().Err(err).Msg("Failed to connect to audio output subprocess")
	} else {
		s.logger.Info().Msg("Connected to audio output subprocess")
	}
}

// SendOpusConfig sends Opus configuration to the audio output subprocess
func (s *AudioOutputSupervisor) SendOpusConfig(config OutputIPCOpusConfig) error {
	if outputClient == nil {
		return fmt.Errorf("client not initialized")
	}

	if !outputClient.IsConnected() {
		return fmt.Errorf("client not connected")
	}

	return outputClient.SendOpusConfig(config)
}
