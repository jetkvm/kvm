package kvm

import (
	"bytes"
	"testing"

	"github.com/jetkvm/kvm/internal/osservices/installer"
)

// The file that the mass storage function opens has the smallest size the
// kernel takes for a CD-ROM, and starts with the disc.
func TestBuiltinInstallerFileIsLargeEnoughForACDROM(t *testing.T) {
	disc := installer.Image("0.5.11")
	file := builtinInstallerFile("0.5.11")
	if len(file) < 300*2048 {
		t.Fatalf("the file has %d bytes; the kernel refuses a CD-ROM under %d", len(file), 300*2048)
	}
	if len(file)%2048 != 0 {
		t.Fatalf("the file has %d bytes, which is not whole sectors", len(file))
	}
	if !bytes.HasPrefix(file, disc) {
		t.Fatalf("the file does not start with the disc")
	}
	if rest := file[len(disc):]; len(bytes.Trim(rest, "\x00")) != 0 {
		t.Fatalf("the bytes after the disc are not zeros")
	}
}
