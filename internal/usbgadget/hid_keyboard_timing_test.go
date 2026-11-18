package usbgadget

import (
	"os"
	"testing"

	"github.com/jetkvm/kvm/internal/timinglog"
)

func TestTimingLogHasEachKeyboardReportThatWasWritten(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	u := newTestGadgetWithKeyboard(t, w)
	timinglog.Start()
	t.Cleanup(timinglog.Stop)

	press := []byte{0x04, 0x05, 0, 0, 0, 0}
	release := make([]byte, hidKeyBufferSize)
	for _, keys := range [][]byte{press, release} {
		if err := u.keyboardWriteHidFileLocked(0x02, keys); err != nil {
			t.Fatal(err)
		}
	}
	// A report that the host does not take in time is not written.
	fillPipeBuffer(t, w)
	if err := u.keyboardWriteHidFileLocked(0, []byte{0x06, 0, 0, 0, 0, 0}); err != nil {
		t.Fatal(err)
	}

	log := timinglog.Read()
	if len(log.Keys) != 2 || log.Keys[0][1] != 0x04 || log.Keys[1][1] != 0 {
		t.Fatalf("got the keys %v, want the first key codes 4 and 0", log.Keys)
	}
	if log.Keys[1][0] < log.Keys[0][0] {
		t.Errorf("the release at %d µs is before the press at %d µs", log.Keys[1][0], log.Keys[0][0])
	}
}
