//go:build linux

package kvm

import (
	"fmt"
	"os/exec"
	"sync"
	"syscall"

	"github.com/rs/zerolog"
)

type nativeOutput struct {
	mu     *sync.Mutex
	logger *zerolog.Event
}

func (w *nativeOutput) Write(p []byte) (n int, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.logger.Msg(string(p))
	return len(p), nil
}

func startNativeBinary(binaryPath string) (*exec.Cmd, error) {
	// Run the binary in the background
	cmd := exec.Command(binaryPath)

	nativeOutputLock := sync.Mutex{}
	nativeStdout := &nativeOutput{
		mu:     &nativeOutputLock,
		logger: nativeLogger.Info().Str("pipe", "stdout"),
	}
	nativeStderr := &nativeOutput{
		mu:     &nativeOutputLock,
		logger: nativeLogger.Info().Str("pipe", "stderr"),
	}

	// Redirect stdout and stderr to the current process
	cmd.Stdout = nativeStdout
	cmd.Stderr = nativeStderr

	// Set the process group ID so we can kill the process and its children when this process exits
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid:   true,
		Pdeathsig: syscall.SIGKILL,
	}

	// Start the command
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start binary: %w", err)
	}

	return cmd, nil
}
