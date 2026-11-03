package kvm

import (
	"context"
	"sync"
)

// Serialize ordinary input with ownership changes and the final neutral report.
// Acquire this before a session's hidInputLock. Macros take admission per write.
var sessionInputMu sync.Mutex

func withSessionInput(s *Session, input func() error) error {
	if s == nil {
		return context.Canceled
	}
	sessionInputMu.Lock()
	defer sessionInputMu.Unlock()
	s.hidInputLock.Lock()
	defer s.hidInputLock.Unlock()
	if s.isClosed() {
		return context.Canceled
	}
	return input()
}

func (s *Session) closeWithRelease(release func() error) {
	sessionInputMu.Lock()
	defer sessionInputMu.Unlock()
	s.retireInputLocked(release)
}

// Caller holds sessionInputMu; admitted writes finish before retirement.
func (s *Session) retireInputLocked(release func() error) {
	s.hidInputLock.Lock()
	defer s.hidInputLock.Unlock()
	s.closeOnce.Do(func() { close(s.done) })
	if currentSession != s {
		return
	}
	cancelKeyboardMacro()
	if gadget != nil {
		gadget.CancelAllAutoReleaseTimers()
	}
	if err := release(); err != nil {
		hidRPCLogger.Warn().Err(err).Msg("failed to release session input")
	}
	currentSession = nil
}

// Retire the previous input owner before admitting the replacement. The old
// peer connection may still need its signaling grace period before it closes.
func replaceSessionInputs(next *Session, release func() error) *Session {
	sessionInputMu.Lock()
	defer sessionInputMu.Unlock()
	previous := currentSession
	if previous != nil {
		previous.retireInputLocked(release)
	}
	currentSession = next
	return previous
}
