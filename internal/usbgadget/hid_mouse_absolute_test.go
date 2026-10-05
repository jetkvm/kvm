package usbgadget

import (
	"os"
	"testing"

	"github.com/rs/zerolog"
)

func TestAbsMousePositionTracksLastReport(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()

	logger := zerolog.Nop()
	u := &UsbGadget{
		log:                   &logger,
		logSuppressionCounter: make(map[string]int),
		absMouseHidFile:       w,
		enabledDevices:        Devices{AbsoluteMouse: true},
	}

	if x, y, known := u.GetAbsMousePosition(); x != 0 || y != 0 || known {
		t.Fatalf("initial GetAbsMousePosition = (%d, %d, %v), want (0, 0, false)", x, y, known)
	}

	if err := u.AbsMouseReport(1234, 5678, 0); err != nil {
		t.Fatalf("AbsMouseReport: %v", err)
	}
	if x, y, known := u.GetAbsMousePosition(); x != 1234 || y != 5678 || !known {
		t.Fatalf("GetAbsMousePosition = (%d, %d, %v), want (1234, 5678, true)", x, y, known)
	}

	// Position tracking must not depend on a button-state change.
	if err := u.AbsMouseReport(42, 99, 0); err != nil {
		t.Fatalf("AbsMouseReport: %v", err)
	}
	if x, y, known := u.GetAbsMousePosition(); x != 42 || y != 99 || !known {
		t.Fatalf("GetAbsMousePosition = (%d, %d, %v), want (42, 99, true)", x, y, known)
	}
}

func TestAbsMousePositionDisabledDeviceNoop(t *testing.T) {
	logger := zerolog.Nop()
	u := &UsbGadget{
		log:                   &logger,
		logSuppressionCounter: make(map[string]int),
		enabledDevices:        Devices{AbsoluteMouse: false},
	}

	if err := u.AbsMouseReport(1234, 5678, 0); err != nil {
		t.Fatalf("AbsMouseReport with disabled device: %v", err)
	}
	if x, y, known := u.GetAbsMousePosition(); x != 0 || y != 0 || known {
		t.Fatalf("GetAbsMousePosition = (%d, %d, %v), want (0, 0, false) since the device is disabled", x, y, known)
	}
}

func newTestAbsMouseGadget(t *testing.T) *UsbGadget {
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
	return &UsbGadget{
		log:                   &logger,
		logSuppressionCounter: make(map[string]int),
		absMouseHidFile:       w,
		enabledDevices:        Devices{AbsoluteMouse: true},
	}
}

func TestJiggleAbsMouseSeedsFromCenterWhenUnknown(t *testing.T) {
	u := newTestAbsMouseGadget(t)

	if err := u.JiggleAbsMouse(100, 300); err != nil {
		t.Fatalf("JiggleAbsMouse: %v", err)
	}

	x, y, known := u.GetAbsMousePosition()
	if !known {
		t.Fatalf("GetAbsMousePosition known = false, want true after jiggling")
	}
	center := absMouseMaxCoord / 2
	if d := x - center; d < -300 || d > 300 {
		t.Errorf("x = %d, want within 300 of center %d", x, center)
	}
	if d := y - center; d < -300 || d > 300 {
		t.Errorf("y = %d, want within 300 of center %d", y, center)
	}
}

func TestJiggleAbsMouseNudgesFromKnownPosition(t *testing.T) {
	u := newTestAbsMouseGadget(t)

	if err := u.AbsMouseReport(1000, 2000, 0); err != nil {
		t.Fatalf("AbsMouseReport: %v", err)
	}
	if err := u.JiggleAbsMouse(100, 300); err != nil {
		t.Fatalf("JiggleAbsMouse: %v", err)
	}

	x, y, _ := u.GetAbsMousePosition()
	if d := x - 1000; d < -300 || d > 300 || d == 0 {
		t.Errorf("x = %d, want a non-zero nudge within 300 of 1000", x)
	}
	if d := y - 2000; d < -300 || d > 300 || d == 0 {
		t.Errorf("y = %d, want a non-zero nudge within 300 of 2000", y)
	}
}

func TestJiggleAbsMousePreservesButtonMask(t *testing.T) {
	u := newTestAbsMouseGadget(t)

	if err := u.AbsMouseReport(1000, 2000, 1); err != nil {
		t.Fatalf("AbsMouseReport: %v", err)
	}
	if err := u.JiggleAbsMouse(100, 300); err != nil {
		t.Fatalf("JiggleAbsMouse: %v", err)
	}

	if !u.absMousePressed {
		t.Errorf("absMousePressed = false, want true: jiggling must not release a held button")
	}
	if u.lastAbsButtons != 1 {
		t.Errorf("lastAbsButtons = %d, want 1: jiggling must preserve the exact button mask", u.lastAbsButtons)
	}
}

func TestJiggleAbsMouseAfterHandoverResetDoesNotResurrectStalePress(t *testing.T) {
	u := newTestAbsMouseGadget(t)

	if err := u.AbsMouseReport(1000, 2000, 1); err != nil {
		t.Fatalf("AbsMouseReport: %v", err)
	}

	// A rebind re-enumerates the gadget, which releases every input on the
	// host, so the stale button mask must not survive the reset.
	u.resetHidHandover()

	if err := u.JiggleAbsMouse(100, 300); err != nil {
		t.Fatalf("JiggleAbsMouse: %v", err)
	}
	if u.absMousePressed {
		t.Errorf("absMousePressed = true after jiggling post-rebind, want false: a stale button mask must not resurrect a press the host never made")
	}
	if u.lastAbsButtons != 0 {
		t.Errorf("lastAbsButtons = %d after jiggling post-rebind, want 0", u.lastAbsButtons)
	}
}

func TestClampAbsCoord(t *testing.T) {
	cases := []struct{ in, want int }{
		{-100, 0},
		{0, 0},
		{100, 100},
		{absMouseMaxCoord, absMouseMaxCoord},
		{absMouseMaxCoord + 100, absMouseMaxCoord},
	}
	for _, c := range cases {
		if got := clampAbsCoord(c.in); got != c.want {
			t.Errorf("clampAbsCoord(%d) = %d, want %d", c.in, got, c.want)
		}
	}
}
