//go:build linux && arm

package kvm

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"
)

// newRPCTestSession returns a session with its queues running and a real but
// unopened RPC data channel. Responses written to it fail with
// io.ErrClosedPipe, which writeJSONRPCResponse logs and swallows — enough to
// exercise dispatch without negotiating a peer connection.
func newRPCTestSession(t *testing.T) *Session {
	t.Helper()

	peerConnection, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = peerConnection.Close() })

	channel, err := peerConnection.CreateDataChannel("rpc", nil)
	if err != nil {
		t.Fatal(err)
	}

	session := &Session{RPCChannel: channel, done: make(chan struct{})}
	session.initOrderedRPCQueue()
	t.Cleanup(session.close)

	return session
}

func rpcTestMessage(t *testing.T, method string, params map[string]any) webrtc.DataChannelMessage {
	t.Helper()

	data, err := json.Marshal(JSONRPCRequest{JSONRPC: "2.0", Method: method, Params: params, ID: 1})
	if err != nil {
		t.Fatal(err)
	}

	return webrtc.DataChannelMessage{Data: data}
}

// registerTestRPCHandler installs a handler for the duration of the test.
func registerTestRPCHandler(t *testing.T, method string, handler RPCHandler) {
	t.Helper()

	rpcHandlers[method] = handler
	t.Cleanup(func() { delete(rpcHandlers, method) })
}

// A handler that assigns shared state must see its messages in the order they
// were sent. Giving every message its own goroutine let a slow first message
// finish after the second, leaving the wrong value behind — which is how a
// setVideoStreamPaused(true)/(false) pair could strand the stream paused.
func TestOrderedRPCHandlerAppliesMessagesInOrder(t *testing.T) {
	const method = "testOrderedToggle"

	applied := make(chan bool, 4)
	registerTestRPCHandler(t, method, RPCHandler{
		Func: func(value bool) error {
			// The first message is the slow one. Under goroutine-per-message
			// dispatch the second overtakes it here and the order inverts.
			if value {
				time.Sleep(50 * time.Millisecond)
			}
			applied <- value
			return nil
		},
		Params:  []string{"value"},
		Ordered: true,
	})

	session := newRPCTestSession(t)
	want := []bool{true, false, true, false}
	for _, value := range want {
		onRPCMessage(rpcTestMessage(t, method, map[string]any{"value": value}), session)
	}

	var seen []bool
	deadline := time.After(10 * time.Second)
	for range want {
		select {
		case value := <-applied:
			seen = append(seen, value)
		case <-deadline:
			t.Fatalf("timed out waiting for handlers; saw %v, want %v", seen, want)
		}
	}

	if !reflect.DeepEqual(seen, want) {
		t.Fatalf("handler saw %v, want %v", seen, want)
	}
}

// Dispatch must never run a handler on the pump goroutine. setVideoStreamPaused
// reaches the native process over gRPC with no deadline, and a resume that
// wakes the capture chip sleeps for seconds by design, so running it there
// would stall every other RPC for the session behind it.
func TestRPCDispatchDoesNotBlockOnSlowHandlers(t *testing.T) {
	for _, tt := range []struct {
		name    string
		ordered bool
	}{
		{name: "ordered", ordered: true},
		{name: "default", ordered: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			method := "testBlockingHandler_" + tt.name

			release := make(chan struct{})
			registerTestRPCHandler(t, method, RPCHandler{
				Func: func() error {
					<-release
					return nil
				},
				Ordered: tt.ordered,
			})
			// Registered before the session so it runs last: the handler is
			// released during teardown rather than leaking a blocked goroutine.
			t.Cleanup(func() { close(release) })

			session := newRPCTestSession(t)
			returned := make(chan struct{})
			go func() {
				defer close(returned)
				onRPCMessage(rpcTestMessage(t, method, nil), session)
			}()

			select {
			case <-returned:
			case <-time.After(5 * time.Second):
				t.Fatal("dispatch blocked on the handler instead of handing it off")
			}
		})
	}
}
