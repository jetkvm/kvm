//go:build !linux

package kvm

import (
	"fmt"
	"os/exec"
)

func startNativeBinary(binaryPath string) (*exec.Cmd, error) {
	return nil, fmt.Errorf("startNativeBinary is only supported on Linux")
}

func ExtractAndRunNativeBin() error {
	return fmt.Errorf("ExtractAndRunNativeBin is only supported on Linux")
}