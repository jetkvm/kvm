//go:build linux

package rawtty

import (
	"os"

	"golang.org/x/sys/unix"
)

const openNoCTTY = unix.O_NOCTTY

// makeRaw uses SyscallConn because f.Fd() would put the file in blocking mode,
// and Close could then no longer interrupt a pending Read.
func makeRaw(f *os.File) error {
	rc, err := f.SyscallConn()
	if err != nil {
		return err
	}
	var termErr error
	if err := rc.Control(func(fd uintptr) {
		t, err := unix.IoctlGetTermios(int(fd), unix.TCGETS)
		if err != nil {
			termErr = err
			return
		}
		t.Iflag &^= unix.IGNBRK | unix.BRKINT | unix.PARMRK | unix.ISTRIP | unix.INLCR | unix.IGNCR | unix.ICRNL | unix.IXON
		t.Oflag &^= unix.OPOST
		t.Lflag &^= unix.ECHO | unix.ECHONL | unix.ICANON | unix.ISIG | unix.IEXTEN
		t.Cflag &^= unix.CSIZE | unix.PARENB
		t.Cflag |= unix.CS8
		t.Cc[unix.VMIN] = 1
		t.Cc[unix.VTIME] = 0
		termErr = unix.IoctlSetTermios(int(fd), unix.TCSETS, t)
	}); err != nil {
		return err
	}
	return termErr
}
