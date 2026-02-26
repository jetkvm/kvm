//go:build !synctrace

package sync

import (
	gosync "sync"
)

// Mutex is a wrapper around the sync.Mutex
type Mutex struct {
	mu gosync.Mutex
}

// Lock locks the mutex
func (m *Mutex) Lock() {
	m.mu.Lock()
}

// Unlock unlocks the mutex
func (m *Mutex) Unlock() {
	m.mu.Unlock()
}

// TryLock tries to lock the mutex
func (m *Mutex) TryLock() bool {
	return m.mu.TryLock()
}

// RWMutex is a wrapper around the sync.RWMutex
type RWMutex struct {
	mu gosync.RWMutex
}

// Lock locks the mutex
func (m *RWMutex) Lock() {
	m.mu.Lock()
}

// Unlock unlocks the mutex
func (m *RWMutex) Unlock() {
	m.mu.Unlock()
}

// RLock locks the mutex for reading
func (m *RWMutex) RLock() {
	m.mu.RLock()
}

// RUnlock unlocks the mutex for reading
func (m *RWMutex) RUnlock() {
	m.mu.RUnlock()
}

// TryRLock tries to lock the mutex for reading
func (m *RWMutex) TryRLock() bool {
	return m.mu.TryRLock()
}

// TryLock tries to lock the mutex
func (m *RWMutex) TryLock() bool {
	return m.mu.TryLock()
}

// WaitGroup is a wrapper around the sync.WaitGroup
type WaitGroup struct {
	wg gosync.WaitGroup
}

// Add adds a function to the wait group
func (w *WaitGroup) Add(delta int) {
	w.wg.Add(delta)
}

// Done decrements the wait group counter
func (w *WaitGroup) Done() {
	w.wg.Done()
}

// Wait waits for the wait group to finish
func (w *WaitGroup) Wait() {
	w.wg.Wait()
}

// Once is a wrapper around the sync.Once
type Once struct {
	mu gosync.Once
}

// Do calls the function f if and only if Do has not been called before for this instance of Once.
func (o *Once) Do(f func()) {
	o.mu.Do(f)
}

// LogDangledLocks is a no-op in non-synctrace builds
func LogDangledLocks() {
}
