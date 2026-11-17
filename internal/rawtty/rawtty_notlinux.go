//go:build !linux

package rawtty

import (
	"errors"
	"os"
)

const openNoCTTY = 0

func makeRaw(f *os.File) error {
	return errors.New("raw tty mode is only implemented on linux")
}
