package kvm

import (
	"errors"
	"time"

	"github.com/jetkvm/kvm/internal/sync"
)

var errSessionInputRetired = errors.New("session input is retired")

var sessionSwitchMu sync.Mutex

func (s *Session) input(fn func() error) error {
	if s == nil {
		return fn()
	}
	s.inputMu.RLock()
	defer s.inputMu.RUnlock()
	if s.inputRetired {
		return errSessionInputRetired
	}
	return fn()
}

func (s *Session) acceptsInput() bool {
	s.inputMu.RLock()
	defer s.inputMu.RUnlock()
	return !s.inputRetired
}

func (s *Session) retireInput() {
	s.inputMu.Lock()
	s.inputRetired = true
	s.inputMu.Unlock()
}

var releaseHeldInput = func() {
	gadget.CancelAllAutoReleaseTimers()
	_ = rpcKeyboardReport(0, keyboardClearStateKeys)
	_ = rpcHidReport(gadget.ReleaseMouseButtons)
}

func takeOverCurrentSession(next *Session) {
	sessionSwitchMu.Lock()
	defer sessionSwitchMu.Unlock()

	prev := currentSession
	currentSession = next
	cancelKeyboardMacro()
	if prev == nil {
		return
	}
	writeJSONRPCEvent("otherSessionConnected", nil, prev)
	prev.retireInput()
	releaseHeldInput()
	peerConn := prev.peerConnection
	go func() {
		time.Sleep(1 * time.Second)
		_ = peerConn.Close()
	}()
}

func endSessionInput(s *Session) {
	sessionSwitchMu.Lock()
	defer sessionSwitchMu.Unlock()

	s.retireInput()
	if s != currentSession {
		return
	}
	cancelKeyboardMacro()
	releaseHeldInput()
	currentSession = nil
}
