//go:build cgo
// +build cgo

package audio

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/jetkvm/kvm/internal/logging"
	"github.com/rs/zerolog"
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
	ctx     context.Context
	cancel  context.CancelFunc
	logger  *zerolog.Logger
	mutex   sync.RWMutex
	running int32

	// Process management
	cmd        *exec.Cmd
	processPID int

	// Restart management
	restartAttempts []time.Time
	lastExitCode    int
	lastExitTime    time.Time

	// Channels for coordination
	processDone       chan struct{}
	stopChan          chan struct{}
	stopChanClosed    bool // Track if stopChan is closed
	processDoneClosed bool // Track if processDone is closed

	// Process monitoring
	processMonitor *ProcessMonitor

	// Callbacks
	onProcessStart func(pid int)
	onProcessExit  func(pid int, exitCode int, crashed bool)
	onRestart      func(attempt int, delay time.Duration)
}

// NewAudioOutputSupervisor creates a new audio output server supervisor
func NewAudioOutputSupervisor() *AudioOutputSupervisor {
	ctx, cancel := context.WithCancel(context.Background())
	logger := logging.GetDefaultLogger().With().Str("component", AudioOutputSupervisorComponent).Logger()

	return &AudioOutputSupervisor{
		ctx:            ctx,
		cancel:         cancel,
		logger:         &logger,
		processDone:    make(chan struct{}),
		stopChan:       make(chan struct{}),
		processMonitor: GetProcessMonitor(),
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

// Start begins supervising the audio output server process
func (s *AudioOutputSupervisor) Start() error {
	if !atomic.CompareAndSwapInt32(&s.running, 0, 1) {
		return fmt.Errorf("audio output supervisor is already running")
	}

	s.logger.Info().Str("component", AudioOutputSupervisorComponent).Msg("starting component")

	// Recreate channels in case they were closed by a previous Stop() call
	s.mutex.Lock()
	s.processDone = make(chan struct{})
	s.stopChan = make(chan struct{})
	s.stopChanClosed = false    // Reset channel closed flag
	s.processDoneClosed = false // Reset channel closed flag
	// Recreate context as well since it might have been cancelled
	s.ctx, s.cancel = context.WithCancel(context.Background())
	// Reset restart tracking on start
	s.restartAttempts = s.restartAttempts[:0]
	s.lastExitCode = 0
	s.lastExitTime = time.Time{}
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

	s.logger.Info().Str("component", AudioOutputSupervisorComponent).Msg("stopping component")

	// Signal stop and wait for cleanup
	s.mutex.Lock()
	if !s.stopChanClosed {
		close(s.stopChan)
		s.stopChanClosed = true
	}
	s.mutex.Unlock()
	s.cancel()

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

// IsRunning returns true if the supervisor is running
func (s *AudioOutputSupervisor) IsRunning() bool {
	return atomic.LoadInt32(&s.running) == 1
}

// GetProcessPID returns the current process PID (0 if not running)
func (s *AudioOutputSupervisor) GetProcessPID() int {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.processPID
}

// GetLastExitInfo returns information about the last process exit
func (s *AudioOutputSupervisor) GetLastExitInfo() (exitCode int, exitTime time.Time) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.lastExitCode, s.lastExitTime
}

// GetProcessMetrics returns current process metrics if the process is running
func (s *AudioOutputSupervisor) GetProcessMetrics() *ProcessMetrics {
	s.mutex.RLock()
	pid := s.processPID
	s.mutex.RUnlock()

	if pid == 0 {
		// Return default metrics when no process is running
		return &ProcessMetrics{
			PID:           0,
			CPUPercent:    0.0,
			MemoryRSS:     0,
			MemoryVMS:     0,
			MemoryPercent: 0.0,
			Timestamp:     time.Now(),
			ProcessName:   "audio-output-server",
		}
	}

	metrics := s.processMonitor.GetCurrentMetrics()
	for _, metric := range metrics {
		if metric.PID == pid {
			return &metric
		}
	}

	// Return default metrics if process not found in monitor
	return &ProcessMetrics{
		PID:           pid,
		CPUPercent:    0.0,
		MemoryRSS:     0,
		MemoryVMS:     0,
		MemoryPercent: 0.0,
		Timestamp:     time.Now(),
		ProcessName:   "audio-output-server",
	}
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

	// Create new command
	s.cmd = exec.CommandContext(s.ctx, execPath, "--audio-output-server")
	s.cmd.Stdout = os.Stdout
	s.cmd.Stderr = os.Stderr

	// Start the process
	if err := s.cmd.Start(); err != nil {
		return fmt.Errorf("failed to start audio output server process: %w", err)
	}

	s.processPID = s.cmd.Process.Pid
	s.logger.Info().Int("pid", s.processPID).Msg("audio server process started")

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
		s.logger.Error().Int("pid", pid).Int("exit_code", exitCode).Msg("audio server process crashed")
		s.recordRestartAttempt()
	} else {
		s.logger.Info().Int("pid", pid).Msg("audio server process exited gracefully")
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

	s.logger.Info().Int("pid", pid).Msg("terminating audio server process")

	// Send SIGTERM first
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		s.logger.Warn().Err(err).Int("pid", pid).Msg("failed to send SIGTERM")
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
