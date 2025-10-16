//go:build synctrace

package sync

import (
	gosync "sync"
)

// WaitGroup is a wrapper around the sync.WaitGroup
type WaitGroup struct {
	wg gosync.WaitGroup
}

// Add adds a function to the wait group
func (w *WaitGroup) Add(delta int) {
	logTrace("Adding to wait group")
	w.wg.Add(delta)
}

// Done decrements the wait group counter
func (w *WaitGroup) Done() {
	logTrace("Done with wait group")
	w.wg.Done()
}

// Wait waits for the wait group to finish
func (w *WaitGroup) Wait() {
	logTrace("Waiting for wait group")
	w.wg.Wait()
}
