// Package rawtty opens ttys in raw mode.
package rawtty

import (
	"fmt"
	"os"
)

// Open opens the tty at path in raw mode, as cfmakeraw would set it, so bytes
// pass through unchanged in both directions. The tty does not become the
// controlling terminal.
func Open(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDWR|openNoCTTY, 0)
	if err != nil {
		return nil, err
	}
	if err := makeRaw(f); err != nil {
		f.Close()
		return nil, fmt.Errorf("set %s to raw mode: %w", path, err)
	}
	return f, nil
}
