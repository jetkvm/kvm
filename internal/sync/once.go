//go:build synctrace

package sync

import (
	gosync "sync"
)

// Once is a wrapper around the sync.Once
type Once struct {
	mu gosync.Once
}

// Do calls the function f if and only if Do has not been called before for this instance of Once.
func (o *Once) Do(f func()) {
	logTrace("Doing once")
	o.mu.Do(f)
}
