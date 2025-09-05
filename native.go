//go:build linux

package kvm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/jetkvm/kvm/resource"
)

var ctrlSocketConn net.Conn

type CtrlAction struct {
	Action string         `json:"action"`
	Seq    int32          `json:"seq,omitempty"`
	Params map[string]any `json:"params,omitempty"`
}

type CtrlResponse struct {
	Seq    int32           `json:"seq,omitempty"`
	Error  string          `json:"error,omitempty"`
	Errno  int32           `json:"errno,omitempty"`
	Result map[string]any  `json:"result,omitempty"`
	Event  string          `json:"event,omitempty"`
	Data   json.RawMessage `json:"data,omitempty"`
}

type EventHandler func(event CtrlResponse)

var seq int32 = 1

var ongoingRequests = make(map[int32]chan *CtrlResponse)

var lock = &sync.Mutex{}

var (
	nativeCmd     *exec.Cmd
	nativeCmdLock = &sync.Mutex{}
)

func CallCtrlAction(action string, params map[string]any) (*CtrlResponse, error) {
	lock.Lock()
	defer lock.Unlock()
	ctrlAction := CtrlAction{
		Action: action,
		Seq:    seq,
		Params: params,
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

func shouldOverwrite(destPath string, srcHash []byte) bool {
	if srcHash == nil {
		nativeLogger.Debug().Msg("error reading embedded jetkvm_native.sha256, doing overwriting")
		return true
	}

	dstHash, err := os.ReadFile(destPath + ".sha256")
	if err != nil {
		nativeLogger.Debug().Msg("error reading existing jetkvm_native.sha256, doing overwriting")
		return true
	}

	return !bytes.Equal(srcHash, dstHash)
}

func getNativeSha256() ([]byte, error) {
	version, err := resource.ResourceFS.ReadFile("jetkvm_native.sha256")
	if err != nil {
		return nil, err
	}
	return version, nil
}

func GetNativeVersion() (string, error) {
	version, err := getNativeSha256()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(version)), nil
}

func ensureBinaryUpdated(destPath string) error {
	srcFile, err := resource.ResourceFS.Open("jetkvm_native")
	if err != nil {
		return err
	}
	defer srcFile.Close()

	srcHash, err := getNativeSha256()
	if err != nil {
		nativeLogger.Debug().Msg("error reading embedded jetkvm_native.sha256, proceeding with update")
		srcHash = nil
	}

	_, err = os.Stat(destPath)
	if shouldOverwrite(destPath, srcHash) || err != nil {
		nativeLogger.Info().
			Interface("hash", srcHash).
			Msg("writing jetkvm_native")

		_ = os.Remove(destPath)
		destFile, err := os.OpenFile(destPath, os.O_CREATE|os.O_RDWR, 0755)
		if err != nil {
			return err
		}
		_, err = io.Copy(destFile, srcFile)
		destFile.Close()
		if err != nil {
			return err
		}
		if srcHash != nil {
			err = os.WriteFile(destPath+".sha256", srcHash, 0644)
			if err != nil {
				return err
			}
		}
		nativeLogger.Info().Msg("jetkvm_native updated")
	}

	return nil
}

// Restore the HDMI EDID value from the config.
// Called after successful connection to jetkvm_native.
func restoreHdmiEdid() {
	if config.EdidString != "" {
		nativeLogger.Info().Str("edid", config.EdidString).Msg("Restoring HDMI EDID")
		_, err := CallCtrlAction("set_edid", map[string]any{"edid": config.EdidString})
		if err != nil {
			nativeLogger.Warn().Err(err).Msg("Failed to restore HDMI EDID")
		}
	}
}
