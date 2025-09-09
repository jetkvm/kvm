//go:build cgo
// +build cgo

package audio

import (
	"context"
	"os/exec"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/jetkvm/kvm/internal/logging"
	"github.com/rs/zerolog"
)

// BaseSupervisor provides common functionality for audio supervisors
type BaseSupervisor struct {
	ctx     context.Context
	cancel  context.CancelFunc
	logger  *zerolog.Logger
	mutex   sync.RWMutex
	running int32

	// Process management
	cmd        *exec.Cmd
	processPID int

	// Process monitoring

	// Exit tracking
	lastExitCode int
	lastExitTime time.Time

	// Channel management
	stopChan          chan struct{}
	processDone       chan struct{}
	stopChanClosed    bool
	processDoneClosed bool
}

// NewBaseSupervisor creates a new base supervisor
func NewBaseSupervisor(componentName string) *BaseSupervisor {
	logger := logging.GetDefaultLogger().With().Str("component", componentName).Logger()
	return &BaseSupervisor{
		logger: &logger,

		stopChan:    make(chan struct{}),
		processDone: make(chan struct{}),
	}
}

// IsRunning returns whether the supervisor is currently running
func (bs *BaseSupervisor) IsRunning() bool {
	return atomic.LoadInt32(&bs.running) == 1
}

// GetProcessPID returns the current process PID
func (bs *BaseSupervisor) GetProcessPID() int {
	bs.mutex.RLock()
	defer bs.mutex.RUnlock()
	return bs.processPID
}

// GetLastExitInfo returns the last exit code and time
func (bs *BaseSupervisor) GetLastExitInfo() (exitCode int, exitTime time.Time) {
	bs.mutex.RLock()
	defer bs.mutex.RUnlock()
	return bs.lastExitCode, bs.lastExitTime
}

// logSupervisorStart logs supervisor start event
func (bs *BaseSupervisor) logSupervisorStart() {
	bs.logger.Info().Msg("Supervisor starting")
}

// logSupervisorStop logs supervisor stop event
func (bs *BaseSupervisor) logSupervisorStop() {
	bs.logger.Info().Msg("Supervisor stopping")
}

// createContext creates a new context for the supervisor
func (bs *BaseSupervisor) createContext() {
	bs.ctx, bs.cancel = context.WithCancel(context.Background())
}

// cancelContext cancels the supervisor context
func (bs *BaseSupervisor) cancelContext() {
	if bs.cancel != nil {
		bs.cancel()
	}
}

// initializeChannels recreates channels for a new supervision cycle
func (bs *BaseSupervisor) initializeChannels() {
	bs.mutex.Lock()
	defer bs.mutex.Unlock()

	bs.stopChan = make(chan struct{})
	bs.processDone = make(chan struct{})
	bs.stopChanClosed = false
	bs.processDoneClosed = false
}

// closeStopChan safely closes the stop channel
func (bs *BaseSupervisor) closeStopChan() {
	bs.mutex.Lock()
	defer bs.mutex.Unlock()

	if !bs.stopChanClosed {
		close(bs.stopChan)
		bs.stopChanClosed = true
	}
}

// closeProcessDone safely closes the process done channel
func (bs *BaseSupervisor) closeProcessDone() {
	bs.mutex.Lock()
	defer bs.mutex.Unlock()

	if !bs.processDoneClosed {
		close(bs.processDone)
		bs.processDoneClosed = true
	}
}

// terminateProcess gracefully terminates the current process with configurable timeout
func (bs *BaseSupervisor) terminateProcess(timeout time.Duration, processType string) {
	bs.mutex.RLock()
	cmd := bs.cmd
	pid := bs.processPID
	bs.mutex.RUnlock()

	if cmd == nil || cmd.Process == nil {
		return
	}

	bs.logger.Info().Int("pid", pid).Msgf("terminating %s process", processType)

	// Send SIGTERM first
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		bs.logger.Warn().Err(err).Int("pid", pid).Msgf("failed to send SIGTERM to %s process", processType)
	}

	// Wait for graceful shutdown
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()

	select {
	case <-done:
		bs.logger.Info().Int("pid", pid).Msgf("%s process terminated gracefully", processType)
	case <-time.After(timeout):
		bs.logger.Warn().Int("pid", pid).Msg("process did not terminate gracefully, sending SIGKILL")
		bs.forceKillProcess(processType)
	}
}

// forceKillProcess forcefully kills the current process
func (bs *BaseSupervisor) forceKillProcess(processType string) {
	bs.mutex.RLock()
	cmd := bs.cmd
	pid := bs.processPID
	bs.mutex.RUnlock()

	if cmd == nil || cmd.Process == nil {
		return
	}

	bs.logger.Warn().Int("pid", pid).Msgf("force killing %s process", processType)
	if err := cmd.Process.Kill(); err != nil {
		bs.logger.Error().Err(err).Int("pid", pid).Msg("failed to kill process")
	}
}

// waitForProcessExit waits for the current process to exit and logs the result
func (bs *BaseSupervisor) waitForProcessExit(processType string) {
	bs.mutex.RLock()
	cmd := bs.cmd
	pid := bs.processPID
	bs.mutex.RUnlock()

	if cmd == nil {
		return
	}

	// Wait for process to exit
	err := cmd.Wait()

	bs.mutex.Lock()
	bs.lastExitTime = time.Now()
	bs.processPID = 0

	var exitCode int
	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			exitCode = exitError.ExitCode()
		} else {
			// Process was killed or other error
			exitCode = -1
		}
	} else {
		exitCode = 0
	}

	bs.lastExitCode = exitCode
	bs.mutex.Unlock()

	// Remove process from monitoring

	if exitCode != 0 {
		bs.logger.Error().Int("pid", pid).Int("exit_code", exitCode).Msgf("%s process exited with error", processType)
	} else {
		bs.logger.Info().Int("pid", pid).Msgf("%s process exited gracefully", processType)
	}
}

// SupervisionConfig holds configuration for the supervision loop
type SupervisionConfig struct {
	ProcessType        string
	Timeout            time.Duration
	EnableRestart      bool
	MaxRestartAttempts int
	RestartWindow      time.Duration
	RestartDelay       time.Duration
	MaxRestartDelay    time.Duration
}

// ProcessCallbacks holds callback functions for process lifecycle events
type ProcessCallbacks struct {
	OnProcessStart func(pid int)
	OnProcessExit  func(pid int, exitCode int, crashed bool)
	OnRestart      func(attempt int, delay time.Duration)
}

// SupervisionLoop provides a template for supervision loops that can be extended by specific supervisors
func (bs *BaseSupervisor) SupervisionLoop(
	config SupervisionConfig,
	callbacks ProcessCallbacks,
	startProcessFunc func() error,
	shouldRestartFunc func() bool,
	calculateDelayFunc func() time.Duration,
) {
	defer func() {
		bs.closeProcessDone()
		bs.logger.Info().Msgf("%s supervision ended", config.ProcessType)
	}()

	for atomic.LoadInt32(&bs.running) == 1 {
		select {
		case <-bs.stopChan:
			bs.logger.Info().Msg("received stop signal")
			bs.terminateProcess(config.Timeout, config.ProcessType)
			return
		case <-bs.ctx.Done():
			bs.logger.Info().Msg("context cancelled")
			bs.terminateProcess(config.Timeout, config.ProcessType)
			return
		default:
			// Start or restart the process
			if err := startProcessFunc(); err != nil {
				bs.logger.Error().Err(err).Msgf("failed to start %s process", config.ProcessType)

				// Check if we should attempt restart (only if restart is enabled)
				if !config.EnableRestart || !shouldRestartFunc() {
					bs.logger.Error().Msgf("maximum restart attempts exceeded or restart disabled, stopping %s supervisor", config.ProcessType)
					return
				}

				delay := calculateDelayFunc()
				bs.logger.Warn().Dur("delay", delay).Msgf("retrying %s process start after delay", config.ProcessType)

				if callbacks.OnRestart != nil {
					callbacks.OnRestart(0, delay) // 0 indicates start failure, not exit restart
				}

				select {
				case <-time.After(delay):
				case <-bs.stopChan:
					return
				case <-bs.ctx.Done():
					return
				}
				continue
			}

			// Wait for process to exit
			bs.waitForProcessExitWithCallback(config.ProcessType, callbacks)

			// Check if we should restart (only if restart is enabled)
			if !config.EnableRestart {
				bs.logger.Info().Msgf("%s process completed, restart disabled", config.ProcessType)
				return
			}

			if !shouldRestartFunc() {
				bs.logger.Error().Msgf("maximum restart attempts exceeded, stopping %s supervisor", config.ProcessType)
				return
			}

			// Calculate restart delay
			delay := calculateDelayFunc()
			bs.logger.Info().Dur("delay", delay).Msgf("restarting %s process after delay", config.ProcessType)

			if callbacks.OnRestart != nil {
				callbacks.OnRestart(1, delay) // 1 indicates restart after exit
			}

			// Wait for restart delay
			select {
			case <-time.After(delay):
			case <-bs.stopChan:
				return
			case <-bs.ctx.Done():
				return
			}
		}
	}
}

// waitForProcessExitWithCallback extends waitForProcessExit with callback support
func (bs *BaseSupervisor) waitForProcessExitWithCallback(processType string, callbacks ProcessCallbacks) {
	bs.mutex.RLock()
	pid := bs.processPID
	bs.mutex.RUnlock()

	// Use the base waitForProcessExit logic
	bs.waitForProcessExit(processType)

	// Handle callbacks if provided
	if callbacks.OnProcessExit != nil {
		bs.mutex.RLock()
		exitCode := bs.lastExitCode
		bs.mutex.RUnlock()

		crashed := exitCode != 0
		callbacks.OnProcessExit(pid, exitCode, crashed)
	}
}
