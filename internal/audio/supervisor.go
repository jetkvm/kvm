//go:build cgo
// +build cgo

package audio

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"sync/atomic"
	"syscall"
	"time"
)

// Restart configuration is now retrieved from centralized config
func getMaxRestartAttempts() int {
	return GetConfig().MaxRestartAttempts
}

func getRestartWindow() time.Duration {
	return GetConfig().RestartWindow
}

func getRestartDelay() time.Duration {
	return GetConfig().RestartDelay
}

func getMaxRestartDelay() time.Duration {
	return GetConfig().MaxRestartDelay
}

// AudioOutputSupervisor manages the audio output server subprocess lifecycle
type AudioOutputSupervisor struct {
	*BaseSupervisor

	// Restart management
	restartAttempts []time.Time

	// Channel management
	stopChan          chan struct{}
	processDone       chan struct{}
	stopChanClosed    bool // Track if stopChan is closed
	processDoneClosed bool // Track if processDone is closed

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
		stopChan:        make(chan struct{}),
		processDone:     make(chan struct{}),
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
	s.onProcessExit = onExit
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
	s.mutex.Lock()
	s.processDone = make(chan struct{})
	s.stopChan = make(chan struct{})
	s.stopChanClosed = false    // Reset channel closed flag
	s.processDoneClosed = false // Reset channel closed flag
	// Reset restart tracking on start
	s.restartAttempts = s.restartAttempts[:0]
	s.mutex.Unlock()

	// Start the supervision loop
	go s.supervisionLoop()

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
	s.mutex.Lock()
	if !s.stopChanClosed {
		close(s.stopChan)
		s.stopChanClosed = true
	}
	s.mutex.Unlock()
	s.cancelContext()

	// Wait for process to exit
	select {
	case <-s.processDone:
		s.logger.Info().Str("component", AudioOutputSupervisorComponent).Msg("component stopped gracefully")
	case <-time.After(GetConfig().SupervisorTimeout):
		s.logger.Warn().Str("component", AudioOutputSupervisorComponent).Msg("component did not stop gracefully, forcing termination")
		s.forceKillProcess()
	}

	s.logger.Info().Str("component", AudioOutputSupervisorComponent).Msg("component stopped")
}

// GetProcessMetrics returns current process metrics with audio-output-server name
func (s *AudioOutputSupervisor) GetProcessMetrics() *ProcessMetrics {
	metrics := s.BaseSupervisor.GetProcessMetrics()
	metrics.ProcessName = "audio-output-server"
	return metrics
}

// supervisionLoop is the main supervision loop
func (s *AudioOutputSupervisor) supervisionLoop() {
	defer func() {
		s.mutex.Lock()
		if !s.processDoneClosed {
			close(s.processDone)
			s.processDoneClosed = true
		}
		s.mutex.Unlock()
		s.logger.Info().Msg("audio server supervision ended")
	}()

	for atomic.LoadInt32(&s.running) == 1 {
		select {
		case <-s.stopChan:
			s.logger.Info().Msg("received stop signal")
			s.terminateProcess()
			return
		case <-s.ctx.Done():
			s.logger.Info().Msg("context cancelled")
			s.terminateProcess()
			return
		default:
			// Start or restart the process
			if err := s.startProcess(); err != nil {
				s.logger.Error().Err(err).Msg("failed to start audio server process")

				// Check if we should attempt restart
				if !s.shouldRestart() {
					s.logger.Error().Msg("maximum restart attempts exceeded, stopping supervisor")
					return
				}

				delay := s.calculateRestartDelay()
				s.logger.Warn().Dur("delay", delay).Msg("retrying process start after delay")

				if s.onRestart != nil {
					s.onRestart(len(s.restartAttempts), delay)
				}

				select {
				case <-time.After(delay):
				case <-s.stopChan:
					return
				case <-s.ctx.Done():
					return
				}
				continue
			}

			// Wait for process to exit
			s.waitForProcessExit()

			// Check if we should restart
			if !s.shouldRestart() {
				s.logger.Error().Msg("maximum restart attempts exceeded, stopping supervisor")
				return
			}

			// Calculate restart delay
			delay := s.calculateRestartDelay()
			s.logger.Info().Dur("delay", delay).Msg("restarting audio server process after delay")

			if s.onRestart != nil {
				s.onRestart(len(s.restartAttempts), delay)
			}

			// Wait for restart delay
			select {
			case <-time.After(delay):
			case <-s.stopChan:
				return
			case <-s.ctx.Done():
				return
			}
		}
	}
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
	s.processMonitor.AddProcess(s.processPID, "audio-output-server")

	if s.onProcessStart != nil {
		s.onProcessStart(s.processPID)
	}

	return nil
}

// waitForProcessExit waits for the current process to exit and logs the result
func (s *AudioOutputSupervisor) waitForProcessExit() {
	s.mutex.RLock()
	cmd := s.cmd
	pid := s.processPID
	s.mutex.RUnlock()

	if cmd == nil {
		return
	}

	// Wait for process to exit
	err := cmd.Wait()

	s.mutex.Lock()
	s.lastExitTime = time.Now()
	s.processPID = 0

	var exitCode int
	var crashed bool

	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			exitCode = exitError.ExitCode()
			crashed = exitCode != 0
		} else {
			// Process was killed or other error
			exitCode = -1
			crashed = true
		}
	} else {
		exitCode = 0
		crashed = false
	}

	s.lastExitCode = exitCode
	s.mutex.Unlock()

	// Remove process from monitoring
	s.processMonitor.RemoveProcess(pid)

	if crashed {
		s.logger.Error().Int("pid", pid).Int("exit_code", exitCode).Msg("audio output server process crashed")
		s.recordRestartAttempt()
	} else {
		s.logger.Info().Int("pid", pid).Msg("audio output server process exited gracefully")
	}

	if s.onProcessExit != nil {
		s.onProcessExit(pid, exitCode, crashed)
	}
}

// terminateProcess gracefully terminates the current process
func (s *AudioOutputSupervisor) terminateProcess() {
	s.mutex.RLock()
	cmd := s.cmd
	pid := s.processPID
	s.mutex.RUnlock()

	if cmd == nil || cmd.Process == nil {
		return
	}

	s.logger.Info().Int("pid", pid).Msg("terminating audio output server process")

	// Send SIGTERM first
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		s.logger.Warn().Err(err).Int("pid", pid).Msg("failed to send SIGTERM to audio output server process")
	}

	// Wait for graceful shutdown
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()

	select {
	case <-done:
		s.logger.Info().Int("pid", pid).Msg("audio server process terminated gracefully")
	case <-time.After(GetConfig().OutputSupervisorTimeout):
		s.logger.Warn().Int("pid", pid).Msg("process did not terminate gracefully, sending SIGKILL")
		s.forceKillProcess()
	}
}

// forceKillProcess forcefully kills the current process
func (s *AudioOutputSupervisor) forceKillProcess() {
	s.mutex.RLock()
	cmd := s.cmd
	pid := s.processPID
	s.mutex.RUnlock()

	if cmd == nil || cmd.Process == nil {
		return
	}

	s.logger.Warn().Int("pid", pid).Msg("force killing audio server process")
	if err := cmd.Process.Kill(); err != nil {
		s.logger.Error().Err(err).Int("pid", pid).Msg("failed to kill process")
	}
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
