package audio

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync/atomic"
	"time"

	"github.com/jetkvm/kvm/internal/logging"
	"github.com/rs/zerolog"
)

// Supervisor manages a subprocess lifecycle with automatic restart
type Supervisor struct {
	name       string
	binaryPath string
	socketPath string
	env        []string

	cmd     *exec.Cmd
	ctx     context.Context
	cancel  context.CancelFunc
	running atomic.Bool
	done    chan struct{} // Closed when supervision loop exits
	logger  zerolog.Logger

	// Restart state
	restartCount   uint8
	lastRestartAt  time.Time
	restartBackoff time.Duration
}

const (
	minRestartDelay = 1 * time.Second
	maxRestartDelay = 30 * time.Second
	restartWindow   = 5 * time.Minute // Reset backoff if process runs this long
)

// NewSupervisor creates a new subprocess supervisor
func NewSupervisor(name, binaryPath, socketPath string, env []string) *Supervisor {
	ctx, cancel := context.WithCancel(context.Background())
	logger := logging.GetDefaultLogger().With().Str("component", name).Logger()

	return &Supervisor{
		name:           name,
		binaryPath:     binaryPath,
		socketPath:     socketPath,
		env:            env,
		ctx:            ctx,
		cancel:         cancel,
		done:           make(chan struct{}),
		logger:         logger,
		restartBackoff: minRestartDelay,
	}
}

// Start begins supervising the subprocess
func (s *Supervisor) Start() error {
	if s.running.Load() {
		return fmt.Errorf("%s: already running", s.name)
	}

	s.running.Store(true)
	go s.supervisionLoop()
	s.logger.Debug().Msg("supervisor started")
	return nil
}

// Stop gracefully stops the subprocess
func (s *Supervisor) Stop() {
	if !s.running.Swap(false) {
		return // Already stopped
	}

	s.logger.Debug().Msg("stopping supervisor")
	s.cancel()

	// Kill process if running
	if s.cmd != nil && s.cmd.Process != nil {
		_ = s.cmd.Process.Kill() // Ignore error, process may already be dead
	}

	// Wait for supervision loop to exit
	<-s.done

	// Clean up socket file
	os.Remove(s.socketPath)
	s.logger.Debug().Msg("supervisor stopped")
}

// supervisionLoop manages the subprocess lifecycle
func (s *Supervisor) supervisionLoop() {
	defer close(s.done)

	for s.running.Load() {
		// Check if we should reset backoff (process ran long enough)
		if !s.lastRestartAt.IsZero() && time.Since(s.lastRestartAt) > restartWindow {
			s.restartCount = 0
			s.restartBackoff = minRestartDelay
			s.logger.Debug().Msg("reset restart backoff after stable run")
		}

		// Start the process
		if err := s.startProcess(); err != nil {
			s.logger.Error().Err(err).Msg("failed to start process")
		} else {
			// Wait for process to exit
			err := s.cmd.Wait()

			if s.running.Load() {
				// Process crashed (not intentional shutdown)
				s.logger.Warn().
					Err(err).
					Uint8("restart_count", s.restartCount).
					Dur("backoff", s.restartBackoff).
					Msg("process exited unexpectedly, will restart")

				s.restartCount++
				s.lastRestartAt = time.Now()

				// Calculate next backoff (exponential: 1s, 2s, 4s, 8s, 16s, 30s)
				s.restartBackoff *= 2
				if s.restartBackoff > maxRestartDelay {
					s.restartBackoff = maxRestartDelay
				}

				// Wait before restart
				select {
				case <-time.After(s.restartBackoff):
					// Continue to next iteration
				case <-s.ctx.Done():
					return // Shutting down
				}
			} else {
				// Intentional shutdown
				s.logger.Debug().Msg("process exited cleanly")
				return
			}
		}
	}
}

// logPipe reads from a pipe and logs each line at debug level
func (s *Supervisor) logPipe(reader io.ReadCloser, stream string) {
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line := scanner.Text()
		s.logger.Debug().Str("stream", stream).Msg(line)
	}
	reader.Close()
}

// startProcess starts the subprocess
func (s *Supervisor) startProcess() error {
	s.cmd = exec.CommandContext(s.ctx, s.binaryPath)
	s.cmd.Env = append(os.Environ(), s.env...)

	// Create pipes for subprocess output
	stdout, err := s.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}
	stderr, err := s.cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	if err := s.cmd.Start(); err != nil {
		return fmt.Errorf("failed to start %s: %w", s.name, err)
	}

	// Start goroutines to log subprocess output at debug level
	go s.logPipe(stdout, "stdout")
	go s.logPipe(stderr, "stderr")

	s.logger.Debug().
		Int("pid", s.cmd.Process.Pid).
		Str("binary", s.binaryPath).
		Strs("custom_env", s.env).
		Msg("process started")

	return nil
}
