//go:build linux

package kvm

import (
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/rs/zerolog"
)

type nativeOutput struct {
	logger *zerolog.Logger
}

func (n *nativeOutput) Write(p []byte) (int, error) {
	n.logger.Debug().Str("output", string(p)).Msg("native binary output")
	return len(p), nil
}

var (
	nativeCmd     *exec.Cmd
	nativeCmdLock = &sync.Mutex{}
)

func startNativeBinary(binaryPath string) (*exec.Cmd, error) {
	cmd := exec.Command(binaryPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Pdeathsig: syscall.SIGTERM,
	}
	cmd.Stdout = &nativeOutput{logger: nativeLogger}
	cmd.Stderr = &nativeOutput{logger: nativeLogger}

	err := cmd.Start()
	if err != nil {
		return nil, err
	}

	return cmd, nil
}

func startNativeBinaryWithLock(binaryPath string) (*exec.Cmd, error) {
	nativeCmdLock.Lock()
	defer nativeCmdLock.Unlock()

	cmd, err := startNativeBinary(binaryPath)
	if err != nil {
		return nil, err
	}
	nativeCmd = cmd
	return cmd, nil
}

func restartNativeBinary(binaryPath string) error {
	time.Sleep(10 * time.Second)
	// restart the binary
	nativeLogger.Info().Msg("restarting jetkvm_native binary")
	cmd, err := startNativeBinary(binaryPath)
	if err != nil {
		nativeLogger.Warn().Err(err).Msg("failed to restart binary")
	}
	nativeCmd = cmd

	// reset the display state
	time.Sleep(1 * time.Second)
	clearDisplayState()
	updateStaticContents()
	requestDisplayUpdate(true)

	return err
}

func superviseNativeBinary(binaryPath string) error {
	nativeCmdLock.Lock()
	defer nativeCmdLock.Unlock()

	if nativeCmd == nil || nativeCmd.Process == nil {
		return restartNativeBinary(binaryPath)
	}

	err := nativeCmd.Wait()

	if err == nil {
		nativeLogger.Info().Err(err).Msg("jetkvm_native binary exited with no error")
	} else if exiterr, ok := err.(*exec.ExitError); ok {
		nativeLogger.Warn().Int("exit_code", exiterr.ExitCode()).Msg("jetkvm_native binary exited with error")
	} else {
		nativeLogger.Warn().Err(err).Msg("jetkvm_native binary exited with unknown error")
	}

	return restartNativeBinary(binaryPath)
}

func ExtractAndRunNativeBin() error {
	binaryPath := "/userdata/jetkvm/bin/jetkvm_native"
	if err := ensureBinaryUpdated(binaryPath); err != nil {
		return fmt.Errorf("failed to extract binary: %w", err)
	}

	// Make the binary executable
	if err := os.Chmod(binaryPath, 0755); err != nil {
		return fmt.Errorf("failed to make binary executable: %w", err)
	}
	// Run the binary in the background
	cmd, err := startNativeBinaryWithLock(binaryPath)
	if err != nil {
		return fmt.Errorf("failed to start binary: %w", err)
	}

	// check if the binary is still running every 10 seconds
	go func() {
		for {
			select {
			case <-appCtx.Done():
				nativeLogger.Info().Msg("stopping native binary supervisor")
				return
			default:
				err := superviseNativeBinary(binaryPath)
				if err != nil {
					nativeLogger.Warn().Err(err).Msg("failed to supervise native binary")
					time.Sleep(1 * time.Second) // Add a short delay to prevent rapid successive calls
				}
			}
		}
	}()

	go func() {
		<-appCtx.Done()
		nativeLogger.Info().Int("pid", cmd.Process.Pid).Msg("killing process")
		err := cmd.Process.Kill()
		if err != nil {
			nativeLogger.Warn().Err(err).Msg("failed to kill process")
			return
		}
	}()

	nativeLogger.Info().Int("pid", cmd.Process.Pid).Msg("jetkvm_native binary started")

	return nil
}
