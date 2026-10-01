package usbgadget

import (
	"io"
	"os"
	"testing"

	"github.com/rs/zerolog"
)

func newTestRelMouseGadget(t *testing.T) (*UsbGadget, *os.File) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		r.Close()
		w.Close()
	})

	logger := zerolog.Nop()
	u := &UsbGadget{
		log:                   &logger,
		logSuppressionCounter: make(map[string]int),
		relMouseHidFile:       w,
		enabledDevices:        Devices{RelativeMouse: true},
	}
	return u, r
}

// readRelReport reads one 5-byte relative mouse report and returns
// buttons, mx, my.
func readRelReport(t *testing.T, r *os.File) (buttons uint8, mx, my int8) {
	t.Helper()
	buf := make([]byte, 5)
	if _, err := io.ReadFull(r, buf); err != nil {
		t.Fatalf("reading relative mouse report: %v", err)
	}
	return buf[0], int8(buf[1]), int8(buf[2])
}

func TestJiggleRelMouseNetZeroDisplacement(t *testing.T) {
	u, r := newTestRelMouseGadget(t)

	if err := u.JiggleRelMouse(10, 20); err != nil {
		t.Fatalf("JiggleRelMouse: %v", err)
	}

	_, outX, outY := readRelReport(t, r)
	_, backX, backY := readRelReport(t, r)

	if backX != -outX || backY != -outY {
		t.Errorf("out=(%d,%d) back=(%d,%d), want back to be the exact negation of out", outX, outY, backX, backY)
	}
	if outX == 0 && outY == 0 {
		t.Errorf("out=(%d,%d), want a non-zero nudge", outX, outY)
	}
}

func TestJiggleRelMouseStaysWithinMagnitudeBounds(t *testing.T) {
	u, r := newTestRelMouseGadget(t)

	if err := u.JiggleRelMouse(10, 20); err != nil {
		t.Fatalf("JiggleRelMouse: %v", err)
	}

	_, outX, outY := readRelReport(t, r)
	for _, v := range []int8{outX, outY} {
		abs := v
		if abs < 0 {
			abs = -abs
		}
		if abs < 10 || abs > 20 {
			t.Errorf("offset = %d, want magnitude within [10, 20]", v)
		}
	}
}

func TestJiggleRelMousePreservesButtonMask(t *testing.T) {
	u, r := newTestRelMouseGadget(t)

	if err := u.RelMouseReport(0, 0, 1); err != nil {
		t.Fatalf("RelMouseReport: %v", err)
	}
	readRelReport(t, r) // drain the setup report

	if err := u.JiggleRelMouse(10, 20); err != nil {
		t.Fatalf("JiggleRelMouse: %v", err)
	}

	outButtons, _, _ := readRelReport(t, r)
	backButtons, _, _ := readRelReport(t, r)
	if outButtons != 1 || backButtons != 1 {
		t.Errorf("out buttons=%d back buttons=%d, want both 1: jiggling must not release a held button", outButtons, backButtons)
	}
}

func TestJiggleRelMouseDisabledDeviceNoop(t *testing.T) {
	logger := zerolog.Nop()
	u := &UsbGadget{
		log:                   &logger,
		logSuppressionCounter: make(map[string]int),
		enabledDevices:        Devices{RelativeMouse: false},
	}

	if err := u.JiggleRelMouse(10, 20); err != nil {
		t.Fatalf("JiggleRelMouse with disabled device: %v", err)
	}
}

func TestResetHidHandoverClearsRelButtonMask(t *testing.T) {
	u, r := newTestRelMouseGadget(t)

	if err := u.RelMouseReport(0, 0, 1); err != nil {
		t.Fatalf("RelMouseReport: %v", err)
	}
	readRelReport(t, r)

	// A rebind re-enumerates the gadget, which releases every input on the
	// host, so the stale button mask must not survive the reset.
	u.resetHidHandover()

	if err := u.JiggleRelMouse(10, 20); err != nil {
		t.Fatalf("JiggleRelMouse: %v", err)
	}

	outButtons, _, _ := readRelReport(t, r)
	backButtons, _, _ := readRelReport(t, r)
	if outButtons != 0 || backButtons != 0 {
		t.Errorf("out buttons=%d back buttons=%d after jiggling post-rebind, want both 0", outButtons, backButtons)
	}
}
