//go:build linux

package tailscale

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/jetkvm/kvm/internal/meshvpn"
)

type ProcessManager struct {
	mu        sync.RWMutex
	cmd       *exec.Cmd
	cancel    context.CancelFunc
	running   bool
	startedAt time.Time
	tunMode   meshvpn.TUNMode
}

func NewProcessManager(tunMode meshvpn.TUNMode) *ProcessManager {
	if tunMode == "" {
		tunMode = meshvpn.TUNModeUserspace
	}
	return &ProcessManager{tunMode: tunMode}
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

func (p *ProcessManager) Start() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.running {
		return nil
	}

	socketDir := filepath.Dir(SocketPath)
	if err := os.MkdirAll(socketDir, 0755); err != nil {
		return err
	}

	stateDir := filepath.Dir(StatePath)
	if err := os.MkdirAll(stateDir, 0700); err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel

	tunArg := "--tun=userspace-networking"
	if p.tunMode == meshvpn.TUNModeKernel {
		tunArg = "--tun=tailscale0"
	}

	p.cmd = exec.CommandContext(ctx, TailscaledPath,
		"--state="+StatePath,
		"--socket="+SocketPath,
		tunArg,
	)

	logger.Info().Str("tunMode", string(p.tunMode)).Msg("starting tailscaled")

	p.cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := p.cmd.Start(); err != nil {
		p.cancel = nil
		return err
	}

	p.running = true
	p.startedAt = time.Now()

	go p.monitor()

	logger.Info().Int("pid", p.cmd.Process.Pid).Msg("started tailscaled")

	time.Sleep(500 * time.Millisecond)

	return nil
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
			logger.Warn().Err(err).Dur("uptime", time.Since(p.startedAt)).Msg("tailscaled exited unexpectedly")
		} else {
			logger.Info().Dur("uptime", time.Since(p.startedAt)).Msg("tailscaled exited")
		}
	}
}

func (p *ProcessManager) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.running {
		return nil
	}

	logger.Info().Msg("stopping tailscaled")

	if p.cancel != nil {
		p.cancel()
		p.cancel = nil
	}

	if p.cmd != nil && p.cmd.Process != nil {
		done := make(chan struct{})
		go func() {
			_ = p.cmd.Wait() // Error intentionally ignored; we just need to know when it exits
			close(done)
		}()

		// Try graceful SIGTERM first
		if err := p.cmd.Process.Signal(syscall.SIGTERM); err != nil {
			// SIGTERM failed - process may already be dead or we lack permission
			logger.Warn().Err(err).Msg("failed to send SIGTERM, trying SIGKILL")
			if killErr := p.cmd.Process.Kill(); killErr != nil {
				logger.Error().Err(killErr).Msg("failed to kill process")
			}
		} else {
			// Wait for graceful shutdown with timeout
			select {
			case <-done:
				// Process exited gracefully
			case <-time.After(5 * time.Second):
				logger.Warn().Msg("tailscaled did not stop gracefully, killing")
				if killErr := p.cmd.Process.Kill(); killErr != nil {
					logger.Error().Err(killErr).Msg("failed to kill process after timeout")
				}
			}
		}

		// Wait for process to fully exit
		<-done
	}

	p.running = false
	p.cmd = nil

	return nil
}
