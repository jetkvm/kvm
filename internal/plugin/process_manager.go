package plugin

import (
	"fmt"
	"log"
	"os/exec"
	"syscall"
	"time"
)

// TODO: this can probably be defaulted to this, but overwritten on a per-plugin basis
const (
	gracefulShutdownDelay = 30 * time.Second
	maxRestartBackoff     = 30 * time.Second
)

type ProcessManager struct {
	cmdGen    func() *exec.Cmd
	cmd       *exec.Cmd
	enabled   bool
	backoff   time.Duration
	shutdown  chan struct{}
	restartCh chan struct{}
	LastError error
}

func NewProcessManager(commandGenerator func() *exec.Cmd) *ProcessManager {
	return &ProcessManager{
		cmdGen:    commandGenerator,
		enabled:   true,
		backoff:   250 * time.Millisecond,
		shutdown:  make(chan struct{}),
		restartCh: make(chan struct{}, 1),
	}
}

func (pm *ProcessManager) StartMonitor() {
	go pm.monitor()
}

func (pm *ProcessManager) monitor() {
	for {
		select {
		case <-pm.shutdown:
			pm.terminate()
			return
		case <-pm.restartCh:
			if pm.enabled {
				go pm.runProcess()
			}
		}
	}
}

func (pm *ProcessManager) runProcess() {
	pm.LastError = nil
	pm.cmd = pm.cmdGen()
	log.Printf("Starting process: %v", pm.cmd)
	err := pm.cmd.Start()
	if err != nil {
		log.Printf("Failed to start process: %v", err)
		pm.LastError = fmt.Errorf("failed to start process: %w", err)
		pm.scheduleRestart()
		return
	}

	err = pm.cmd.Wait()
	if err != nil {
		log.Printf("Process exited: %v", err)
		pm.LastError = fmt.Errorf("process exited with error: %w", err)
		pm.scheduleRestart()
	}
}

func (pm *ProcessManager) scheduleRestart() {
	if pm.enabled {
		log.Printf("Restarting process in %v...", pm.backoff)
		time.Sleep(pm.backoff)
		pm.backoff *= 2 // Exponential backoff
		if pm.backoff > maxRestartBackoff {
			pm.backoff = maxRestartBackoff
		}
		pm.restartCh <- struct{}{}
	}
}

func (pm *ProcessManager) terminate() {
	if pm.cmd.Process != nil {
		log.Printf("Sending SIGTERM...")
		pm.cmd.Process.Signal(syscall.SIGTERM)
		select {
		case <-time.After(gracefulShutdownDelay):
			log.Printf("Forcing process termination...")
			pm.cmd.Process.Kill()
		case <-pm.waitForExit():
			log.Printf("Process exited gracefully.")
		}
	}
}

func (pm *ProcessManager) waitForExit() <-chan struct{} {
	done := make(chan struct{})
	go func() {
		pm.cmd.Wait()
		close(done)
	}()
	return done
}

func (pm *ProcessManager) Enable() {
	pm.enabled = true
	pm.restartCh <- struct{}{}
}

func (pm *ProcessManager) Disable() {
	pm.enabled = false
	close(pm.shutdown)
	pm.cmd.Wait()
}
