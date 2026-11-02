package usbgadget

import (
	"bytes"
	"io"
	"os"
	"testing"
	"time"
)

func wheelTestGadget(t *testing.T) *UsbGadget {
	t.Helper()
	u := newTestGadgetWithKeyboard(nil)
	u.enabledDevices = defaultUsbGadgetDevices
	u.kbdAutoReleaseTimers = make(map[byte]*time.Timer)
	u.keysDownState.Keys = make([]byte, 6)
	t.Cleanup(u.ResetHIDFiles)
	return u
}
func wheelReportPipe(t *testing.T) (*os.File, *os.File) {
	t.Helper()
	r, w, e := os.Pipe()
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { r.Close(); w.Close() })
	return r, w
}
func readWheelReport(t *testing.T, r *os.File, want []byte) {
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
