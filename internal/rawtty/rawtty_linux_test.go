//go:build linux

package rawtty

import (
	"errors"
	"os"
	"testing"
	"time"

	"github.com/creack/pty"
	"golang.org/x/sys/unix"
)

// openPair stands in for the CDC-ACM gadget with a pty: what host writes
// arrives on dev, as the USB host's writes arrive on /dev/ttyGS0.
func openPair(t *testing.T) (host *os.File, dev *os.File) {
	t.Helper()
	ptmx, tty, err := pty.Open()
	if err != nil {
		t.Skipf("pty unavailable: %v", err)
	}
	t.Cleanup(func() { ptmx.Close(); tty.Close() })

	// pty.Open returns ptmx in blocking mode, which ignores read deadlines,
	// so read through a non-blocking duplicate.
	hostFd, err := unix.Dup(int(ptmx.Fd()))
	if err != nil {
		t.Fatalf("dup: %v", err)
	}
	if err := unix.SetNonblock(hostFd, true); err != nil {
		t.Fatalf("set nonblock: %v", err)
	}
	host = os.NewFile(uintptr(hostFd), "ptmx")
	t.Cleanup(func() { host.Close() })

	dev, err = Open(tty.Name())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { dev.Close() })
	return host, dev
}

func readWithin(t *testing.T, f *os.File, d time.Duration) (string, error) {
	t.Helper()
	if err := f.SetReadDeadline(time.Now().Add(d)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	buf := make([]byte, 256)
	n, err := f.Read(buf)
	return string(buf[:n]), err
}

// readN reads until it has n bytes or the deadline passes.
func readN(t *testing.T, f *os.File, n int, d time.Duration) (string, error) {
	t.Helper()
	if err := f.SetReadDeadline(time.Now().Add(d)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	got := make([]byte, 0, n)
	buf := make([]byte, 256)
	for len(got) < n {
		m, err := f.Read(buf)
		got = append(got, buf[:m]...)
		if err != nil {
			return string(got), err
		}
	}
	return string(got), nil
}

func TestDeliversOutputWithoutNewline(t *testing.T) {
	host, dev := openPair(t)

	const prompt = "boxy login: "
	if _, err := host.WriteString(prompt); err != nil {
		t.Fatalf("host write: %v", err)
	}
	got, err := readN(t, dev, len(prompt), time.Second)
	if err != nil {
		t.Fatalf("prompt without newline was not delivered: %v", err)
	}
	if got != prompt {
		t.Fatalf("got %q, want %q", got, prompt)
	}
}

func TestDoesNotEchoHostOutput(t *testing.T) {
	host, dev := openPair(t)

	if _, err := host.WriteString("ECHOTEST\r\n"); err != nil {
		t.Fatalf("host write: %v", err)
	}
	if got, err := readN(t, dev, len("ECHOTEST\r\n"), time.Second); err != nil || got != "ECHOTEST\r\n" {
		t.Fatalf("device read = %q, %v; want %q unchanged", got, err, "ECHOTEST\r\n")
	}
	if got, err := readWithin(t, host, 200*time.Millisecond); !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("host read back %q (err %v); want no echo", got, err)
	}
}

// controlBytes holds characters the line discipline would otherwise change or
// act on: CR (ICRNL, OPOST), ^C ^Z ^\ (ISIG) and XON/XOFF (IXON). Linux only
// treats ^V specially (IEXTEN) in canonical mode, so it can't test IEXTEN.
const controlBytes = "root\r\x03\x1a\x1c\x11\x13\x16\x1b[A\n"

// Output path: what the browser sends to the USB host.
func TestDeviceOutputUnchanged(t *testing.T) {
	host, dev := openPair(t)

	if _, err := dev.WriteString(controlBytes); err != nil {
		t.Fatalf("device write: %v", err)
	}
	if got, err := readN(t, host, len(controlBytes), time.Second); err != nil || got != controlBytes {
		t.Fatalf("host read = %q, %v; want %q", got, err, controlBytes)
	}
}

// Input path: what the USB host sends to the browser.
func TestHostInputUnchanged(t *testing.T) {
	host, dev := openPair(t)

	if _, err := host.WriteString(controlBytes); err != nil {
		t.Fatalf("host write: %v", err)
	}
	if got, err := readN(t, dev, len(controlBytes), time.Second); err != nil || got != controlBytes {
		t.Fatalf("device read = %q, %v; want %q", got, err, controlBytes)
	}
}

// The CDC-ACM relay stops its reader by closing the file, so Close must
// interrupt a pending Read.
func TestCloseUnblocksRead(t *testing.T) {
	_, dev := openPair(t)

	done := make(chan error, 1)
	go func() {
		_, err := dev.Read(make([]byte, 16))
		done <- err
	}()
	time.Sleep(50 * time.Millisecond)
	dev.Close()

	select {
	case err := <-done:
		if !errors.Is(err, os.ErrClosed) {
			t.Fatalf("Read returned %v, want os.ErrClosed", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Close did not unblock a pending Read")
	}
}
