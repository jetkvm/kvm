//go:build cgo
// +build cgo

package audio

import (
	"context"
	"os/exec"
	"sync"
	"sync/atomic"
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
	processMonitor *ProcessMonitor

	// Exit tracking
	lastExitCode int
	lastExitTime time.Time
}

// NewBaseSupervisor creates a new base supervisor
func NewBaseSupervisor(componentName string) *BaseSupervisor {
	logger := logging.GetDefaultLogger().With().Str("component", componentName).Logger()
	return &BaseSupervisor{
		logger:         &logger,
		processMonitor: GetProcessMonitor(),
	}
}

// IsRunning returns whether the supervisor is currently running
func (bs *BaseSupervisor) IsRunning() bool {
	return atomic.LoadInt32(&bs.running) == 1
}

// setRunning atomically sets the running state
func (bs *BaseSupervisor) setRunning(running bool) {
	if running {
		atomic.StoreInt32(&bs.running, 1)
	} else {
		atomic.StoreInt32(&bs.running, 0)
	}
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

// GetProcessMetrics returns process metrics if available
func (bs *BaseSupervisor) GetProcessMetrics() *ProcessMetrics {
	bs.mutex.RLock()
	defer bs.mutex.RUnlock()

	if bs.cmd == nil || bs.cmd.Process == nil {
		return &ProcessMetrics{
			PID:           0,
			CPUPercent:    0.0,
			MemoryRSS:     0,
			MemoryVMS:     0,
			MemoryPercent: 0.0,
			Timestamp:     time.Now(),
			ProcessName:   "audio-server",
		}
	}

	pid := bs.cmd.Process.Pid
	if bs.processMonitor != nil {
		metrics := bs.processMonitor.GetCurrentMetrics()
		for _, metric := range metrics {
			if metric.PID == pid {
				return &metric
			}
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
		ProcessName:   "audio-server",
	}
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
