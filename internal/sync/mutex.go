//go:build synctrace

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
	logLock(m)
	m.mu.Lock()
}

// Unlock unlocks the mutex
func (m *Mutex) Unlock() {
	logUnlock(m)
	m.mu.Unlock()
}

// TryLock tries to lock the mutex
func (m *Mutex) TryLock() bool {
	logTryLock(m)
	l := m.mu.TryLock()
	logTryLockResult(m, l)
	return l
}

// RWMutex is a wrapper around the sync.RWMutex
type RWMutex struct {
	mu gosync.RWMutex
}

// Lock locks the mutex
func (m *RWMutex) Lock() {
	logLock(m)
	m.mu.Lock()
}

// Unlock unlocks the mutex
func (m *RWMutex) Unlock() {
	logUnlock(m)
	m.mu.Unlock()
}

// RLock locks the mutex for reading
func (m *RWMutex) RLock() {
	logRLock(m)
	m.mu.RLock()
}

// RUnlock unlocks the mutex for reading
func (m *RWMutex) RUnlock() {
	logRUnlock(m)
	m.mu.RUnlock()
}

// TryRLock tries to lock the mutex for reading
func (m *RWMutex) TryRLock() bool {
	logTryRLock(m)
	l := m.mu.TryRLock()
	logTryRLockResult(m, l)
	return l
}
