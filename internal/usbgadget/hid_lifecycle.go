package usbgadget

import (
	"fmt"
	"os"
	"time"

	"github.com/jetkvm/kvm/internal/sync"
)

const hidOpenDrainTimeout = 3 * time.Second

// Each group of outstanding opens shares a completion channel. Recovery waits
// directly on it, so repeated timeouts do not leave waiter goroutines behind.
type hidOpenTracker struct {
	mu      sync.Mutex
	pending int
	drained chan struct{}
}

func (h *hidOpenTracker) begin() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.pending == 0 {
		h.drained = make(chan struct{})
	}
	h.pending++
}

func (h *hidOpenTracker) end() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.pending--
	if h.pending == 0 {
		close(h.drained)
	}
}

// Caller holds hidLifecycle exclusively, preventing new admissions while waiting.
func (h *hidOpenTracker) wait(timeout time.Duration) error {
	h.mu.Lock()
	pending, drained := h.pending, h.drained
	h.mu.Unlock()
	if pending == 0 {
		return nil
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-drained:
		return nil
	case <-timer.C:
		return fmt.Errorf("USB rebind aborted: HID opens did not drain within %s", timeout)
	}
}

func (u *UsbGadget) openHIDFile(name string, flag int, perm os.FileMode) (*os.File, error) {
	if u.hidOpenFile != nil {
		return u.hidOpenFile(name, flag, perm)
	}
	return os.OpenFile(name, flag, perm)
}
