package kvm

import (
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/jetkvm/kvm/internal/hidrpc"
	"github.com/pion/webrtc/v4"
)

type inputLog struct {
	mu     sync.Mutex
	events []string
}

func (l *inputLog) add(event string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, event)
}

func (l *inputLog) get() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.events...)
}

func newInputTestSession(t *testing.T) *Session {
	t.Helper()
	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pc.Close() })
	return &Session{done: make(chan struct{}), peerConnection: pc, RPCChannel: &webrtc.DataChannel{}}
}

func setupSessionInputTest(t *testing.T, log *inputLog, names map[*Session]string, hold func()) {
	t.Helper()
	oldCurrent, oldRelease, oldHandler := currentSession, releaseHeldInput, rpcHandlers["relMouseReport"]
	t.Cleanup(func() {
		currentSession, releaseHeldInput = oldCurrent, oldRelease
		rpcHandlers["relMouseReport"] = oldHandler
	})
	currentSession = nil
	releaseHeldInput = func() { log.add("release") }
	rpcHandlers["relMouseReport"] = RPCHandler{
		Func: func(s *Session, dx int8, dy int8, buttons uint8) error {
			if dx == 1 {
				hold()
			}
			log.add(names[s])
			return nil
		},
		Params: []string{"dx", "dy", "buttons"},
		Input:  oldHandler.Input,
	}
}

func sendRelMouse(s *Session, dx string) {
	onRPCMessage(webrtc.DataChannelMessage{Data: []byte(
		`{"jsonrpc":"2.0","method":"relMouseReport","params":{"dx":` + dx + `,"dy":0,"buttons":1},"id":1}`,
	)}, s)
}

func sendKeepAlive(s *Session) {
	onHidMessage(hidQueueMessage{DataChannelMessage: webrtc.DataChannelMessage{
		Data: []byte{byte(hidrpc.TypeKeypressKeepAliveReport)},
	}}, s)
}

func TestHandoffDropsOldSessionInputAndAdmitsNewSession(t *testing.T) {
	var log inputLog
	a, b := newInputTestSession(t), newInputTestSession(t)
	entered, proceed := make(chan struct{}), make(chan struct{})
	setupSessionInputTest(t, &log, map[*Session]string{a: "a", b: "b"}, func() {
		close(entered)
		<-proceed
	})
	takeOverCurrentSession(a)

	inFlight := make(chan struct{})
	go func() {
		defer close(inFlight)
		sendRelMouse(a, "1")
	}()
	<-entered
	tookOver := make(chan struct{})
	go func() {
		defer close(tookOver)
		takeOverCurrentSession(b)
	}()
	sendRelMouse(b, "0")
	time.Sleep(20 * time.Millisecond)
	close(proceed)
	<-inFlight
	<-tookOver

	sendRelMouse(a, "0")
	sendKeepAlive(a)
	sendRelMouse(b, "0")
	sendKeepAlive(b)

	if want := []string{"b", "a", "release", "b"}; !reflect.DeepEqual(log.get(), want) {
		t.Fatalf("events = %v, want %v", log.get(), want)
	}
	if !a.lastKeepAliveArrivalTime.IsZero() {
		t.Fatal("replaced session's keepalive reached the auto-release timers")
	}
	if b.lastKeepAliveArrivalTime.IsZero() {
		t.Fatal("current session's keepalive was dropped")
	}
}

func TestLateCloseOfReplacedSessionKeepsNewSessionInput(t *testing.T) {
	var log inputLog
	a, b := newInputTestSession(t), newInputTestSession(t)
	setupSessionInputTest(t, &log, map[*Session]string{a: "a", b: "b"}, nil)
	takeOverCurrentSession(a)
	takeOverCurrentSession(b)

	endSessionInput(a)
	a.close()
	if currentSession != b {
		t.Fatal("late close of the replaced session cleared the current session")
	}
	sendRelMouse(b, "0")

	endSessionInput(b)
	b.close()
	sendRelMouse(b, "0")

	if want := []string{"release", "b", "release"}; !reflect.DeepEqual(log.get(), want) {
		t.Fatalf("events = %v, want %v", log.get(), want)
	}
	if currentSession != nil {
		t.Fatal("close of the current session left it current")
	}
}
