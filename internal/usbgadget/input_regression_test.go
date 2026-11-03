package usbgadget

import (
	"bytes"
	"io"
	"os"
	"testing"
	"time"
)

func inputTestGadget(t *testing.T) *UsbGadget {
	t.Helper()
	u := newTestGadgetWithKeyboard(nil)
	u.enabledDevices = defaultUsbGadgetDevices
	u.kbdAutoReleaseTimers = make(map[byte]*time.Timer)
	u.keysDownState.Keys = make([]byte, 6)
	t.Cleanup(u.ResetHIDFiles)
	return u
}
func reportPipe(t *testing.T) (*os.File, *os.File) {
	t.Helper()
	r, w, e := os.Pipe()
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { r.Close(); w.Close() })
	return r, w
}
func readReport(t *testing.T, r *os.File, want []byte) {
	t.Helper()
	r.SetReadDeadline(time.Now().Add(2 * time.Second))
	got := make([]byte, len(want))
	if _, e := io.ReadFull(r, got); e != nil {
		t.Fatal(e)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("report %v, want %v", got, want)
	}
}
func noInputReport(t *testing.T, r *os.File) {
	t.Helper()
	r.SetReadDeadline(time.Now().Add(10 * time.Millisecond))
	n, e := r.Read(make([]byte, 32))
	if n != 0 || !os.IsTimeout(e) {
		t.Fatalf("unexpected report n=%d err=%v", n, e)
	}
}
