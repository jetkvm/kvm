package usbgadget

import (
	"bytes"
	"io"
	"os"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func mouseReleaseGadget(t *testing.T) (*UsbGadget, *os.File, *os.File) {
	t.Helper()
	absR, absW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	relR, relW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, f := range []*os.File{absR, absW, relR, relW} {
			_ = f.Close()
		}
	})
	logger := zerolog.Nop()
	return &UsbGadget{
		log:                   &logger,
		logSuppressionCounter: make(map[string]int),
		enabledDevices:        Devices{AbsoluteMouse: true, RelativeMouse: true},
		absMouseHidFile:       absW,
		relMouseHidFile:       relW,
	}, absR, relR
}

func expectReport(t *testing.T, r *os.File, want []byte) {
	t.Helper()
	if err := r.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, len(want))
	if _, err := io.ReadFull(r, got); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("report = %v, want %v", got, want)
	}
}

func expectNoReport(t *testing.T, r *os.File) {
	t.Helper()
	if err := r.SetReadDeadline(time.Now().Add(20 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 16)
	if n, err := r.Read(buf); !os.IsTimeout(err) {
		t.Fatalf("unexpected report %v (err %v)", buf[:n], err)
	}
}

func TestReleaseMouseButtonsSendsNothingWhenNoButtonIsDown(t *testing.T) {
	u, absR, relR := mouseReleaseGadget(t)
	if err := u.AbsMouseReport(100, 200, 0); err != nil {
		t.Fatal(err)
	}
	expectReport(t, absR, []byte{1, 0, 100, 0, 200, 0})
	if err := u.RelMouseReport(5, 5, 0); err != nil {
		t.Fatal(err)
	}
	expectReport(t, relR, []byte{0, 5, 5, 0, 0})

	if err := u.ReleaseMouseButtons(); err != nil {
		t.Fatal(err)
	}
	expectNoReport(t, absR)
	expectNoReport(t, relR)
}

func TestReleaseMouseButtonsReleasesHeldButtonsAtLastPosition(t *testing.T) {
	u, absR, relR := mouseReleaseGadget(t)
	if err := u.AbsMouseReport(100, 200, 1); err != nil {
		t.Fatal(err)
	}
	expectReport(t, absR, []byte{1, 1, 100, 0, 200, 0})
	if err := u.AbsMouseReport(0x1234, 0x0567, 1); err != nil {
		t.Fatal(err)
	}
	expectReport(t, absR, []byte{1, 1, 0x34, 0x12, 0x67, 0x05})
	if err := u.RelMouseReport(-3, 4, 2); err != nil {
		t.Fatal(err)
	}
	expectReport(t, relR, []byte{2, 0xfd, 4, 0, 0})

	if err := u.ReleaseMouseButtons(); err != nil {
		t.Fatal(err)
	}
	expectReport(t, absR, []byte{1, 0, 0x34, 0x12, 0x67, 0x05})
	expectReport(t, relR, []byte{0, 0, 0, 0, 0})

	if err := u.ReleaseMouseButtons(); err != nil {
		t.Fatal(err)
	}
	expectNoReport(t, absR)
	expectNoReport(t, relR)
}
