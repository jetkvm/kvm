package nativeedid_test

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// Exercise the actual C implementation without the device native library.
// Every system call in the fixture is replaced; no capture device is opened.
func TestNativeEDIDReadCapacity(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("requires Linux V4L2 headers")
	}
	cc, err := exec.LookPath("cc")
	if err != nil {
		t.Skip("C compiler unavailable")
	}
	binary := filepath.Join(t.TempDir(), "edid-capacity")
	cmd := exec.Command(cc, "-std=gnu11", "-Wall", "-Wextra", "-Werror", "-I", "../cgo", "testdata/edid_capacity.c", "-o", binary)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("compile actual C implementation: %v\n%s", err, output)
	}
	for _, name := range []string{"capacity128", "capacity256", "one_block_in256", "empty_result", "oversize128", "oversize256", "invalid_capacity", "null_buffer", "open_error", "ioctl_error"} {
		t.Run(name, func(t *testing.T) {
			if output, err := exec.Command(binary, name).CombinedOutput(); err != nil {
				t.Fatalf("native boundary: %v\n%s", err, output)
			}
		})
	}
}
