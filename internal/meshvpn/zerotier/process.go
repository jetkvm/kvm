//go:build linux

package zerotier

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// ProcessManager manages the zerotier-one daemon process.
type ProcessManager struct {
	mu        sync.RWMutex
	cmd       *exec.Cmd
	cancel    context.CancelFunc
	running   bool
	startedAt time.Time
}

func NewProcessManager() *ProcessManager {
	return &ProcessManager{}
}

func (p *ProcessManager) IsRunning() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if !p.running {
		return false
	}

	if p.cmd != nil && p.cmd.Process != nil {
		if err := p.cmd.Process.Signal(syscall.Signal(0)); err != nil {
			return false
		}
	}

	return p.running
}

// ensureTUN loads the TUN kernel module if not already loaded.
// ZeroTier requires TUN for creating virtual network interfaces.
func (p *ProcessManager) ensureTUN() error {
	// Check if TUN device already exists
	if _, err := os.Stat("/dev/net/tun"); err == nil {
		return nil
	}

	// Try to load the TUN module
	logger.Debug().Msg("loading TUN kernel module")
	cmd := exec.Command("modprobe", "tun")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("modprobe tun failed: %w (output: %s)", err, string(output))
	}

	// Verify TUN device exists after loading
	if _, err := os.Stat("/dev/net/tun"); err != nil {
		return fmt.Errorf("TUN device not available after loading module: %w", err)
	}

	logger.Debug().Msg("TUN kernel module loaded successfully")
	return nil
}

func (p *ProcessManager) Start() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.running {
		return nil
	}

	// Ensure TUN module is loaded (required for ZeroTier virtual network interface)
	if err := p.ensureTUN(); err != nil {
		return fmt.Errorf("TUN module required for ZeroTier: %w", err)
	}

	// Ensure working directory exists
	if err := os.MkdirAll(WorkingDirectory, 0700); err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel

	// zerotier-one runs as a daemon when given a working directory argument
	// It stores all state (identity, networks, auth token) in this directory
	p.cmd = exec.CommandContext(ctx, ZeroTierOnePath, WorkingDirectory)
	p.cmd.Dir = WorkingDirectory

	logger.Info().Msg("starting zerotier-one")

	p.cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := p.cmd.Start(); err != nil {
		p.cancel = nil
		return err
	}

	p.running = true
	p.startedAt = time.Now()

	go p.monitor()

	logger.Info().Int("pid", p.cmd.Process.Pid).Msg("started zerotier-one")

	// Wait for daemon to initialize (indicated by port file creation)
	if err := p.waitForReady(10 * time.Second); err != nil {
		// Stop the process since it failed to initialize properly
		_ = p.cmd.Process.Kill()
		p.running = false
		p.cmd = nil
		return fmt.Errorf("daemon failed to initialize: %w", err)
	}

	return nil
}

// waitForReady waits for the daemon to create its port file, indicating it's ready.
func (p *ProcessManager) waitForReady(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(PortFilePath); err == nil {
			logger.Debug().Msg("daemon ready (port file exists)")
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	return fmt.Errorf("timeout waiting for daemon to initialize")
}

func (p *ProcessManager) monitor() {
	if p.cmd == nil {
		return
	}

	err := p.cmd.Wait()

	p.mu.Lock()
	defer p.mu.Unlock()

	wasRunning := p.running
	p.running = false

	if wasRunning {
		if err != nil {
			logger.Warn().Err(err).Dur("uptime", time.Since(p.startedAt)).Msg("zerotier-one exited unexpectedly")
		} else {
			logger.Info().Dur("uptime", time.Since(p.startedAt)).Msg("zerotier-one exited")
		}
	}
}

func (p *ProcessManager) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.running {
		return nil
	}

	logger.Info().Msg("stopping zerotier-one")

	if p.cancel != nil {
		p.cancel()
		p.cancel = nil
	}

	if p.cmd != nil && p.cmd.Process != nil {
		// Try graceful SIGTERM first
		if err := p.cmd.Process.Signal(syscall.SIGTERM); err != nil {
			logger.Debug().Err(err).Msg("SIGTERM failed, forcing kill")
			if killErr := p.cmd.Process.Kill(); killErr != nil {
				logger.Warn().Err(killErr).Msg("failed to kill zerotier-one process")
			}
		}

		// Wait for process to exit with timeout
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if err := p.cmd.Process.Signal(syscall.Signal(0)); err != nil {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}

		// Force kill if still running
		if err := p.cmd.Process.Signal(syscall.Signal(0)); err == nil {
			logger.Debug().Msg("process still running after timeout, forcing kill")
			if killErr := p.cmd.Process.Kill(); killErr != nil {
				logger.Warn().Err(killErr).Msg("failed to force kill zerotier-one process")
			}
			time.Sleep(500 * time.Millisecond)
		}
	}

	p.running = false
	p.cmd = nil
	return nil
}
