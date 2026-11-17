package usbgadget

import (
	"sync"
	"testing"
	"time"
)

func TestLastUserInputTimeConcurrent(t *testing.T) {
	u := &UsbGadget{}
	if got := u.GetLastUserInputTime(); !got.IsZero() {
		t.Fatalf("new gadget input time = %v, want zero", got)
	}
	before := time.Now()
	u.resetUserInputTime()
	start := make(chan struct{})
	var wg sync.WaitGroup
	for worker := 0; worker < 4; worker++ {
		wg.Add(1)
		go func(writer bool) {
			defer wg.Done()
			<-start
			for i := 0; i < 1000; i++ {
				if writer {
					u.resetUserInputTime()
				} else if got := u.GetLastUserInputTime(); got.Before(before) {
					t.Errorf("input time moved before first input: %v", got)
					return
				}
			}
		}(worker%2 == 0)
	}
	close(start)
	wg.Wait()
	if got := u.GetLastUserInputTime(); got.Before(before) || got.After(time.Now()) {
		t.Fatalf("input time outside input interval: %v", got)
	}
}
