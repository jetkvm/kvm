//go:build !linux

package kvm

import (
	"fmt"
	"os/exec"
)

func startNativeBinary(binaryPath string) (*exec.Cmd, error) {
	return nil, fmt.Errorf("not supported")
}
