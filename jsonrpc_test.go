//go:build linux && arm

package kvm

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"
)

// newRPCTestSession returns a session with a real but unopened RPC data
// channel. Responses written to it fail with io.ErrClosedPipe, which
// writeJSONRPCResponse logs and swallows — enough to exercise dispatch without
// negotiating a peer connection.
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

	return &Session{RPCChannel: channel}
}

func rpcTestMessage(t *testing.T, method string, params map[string]any) webrtc.DataChannelMessage {
	t.Helper()

	data, err := json.Marshal(JSONRPCRequest{JSONRPC: "2.0", Method: method, Params: params, ID: 1})
	if err != nil {
		t.Fatal(err)
	}

	return webrtc.DataChannelMessage{Data: data}
}

// A handler that toggles shared state must see its messages in the order they
// were dequeued. Dispatching every message to its own goroutine let the
// scheduler run a tight pause/resume pair backwards and leave the wrong value
// behind, so Synchronous handlers run inline on the pump instead.
func TestSynchronousRPCHandlerRunsInlineInOrder(t *testing.T) {
	const method = "testSynchronousToggle"

	var seen []bool
	rpcHandlers[method] = RPCHandler{
		Func: func(value bool) error {
			seen = append(seen, value)
			return nil
		},
		Params:      []string{"value"},
		Synchronous: true,
	}
	t.Cleanup(func() { delete(rpcHandlers, method) })

	session := newRPCTestSession(t)
	want := []bool{true, false, true, false}
	for i, value := range want {
		onRPCMessage(rpcTestMessage(t, method, map[string]any{"value": value}), session)
		// Inline dispatch means the handler has already run by the time the
		// dispatcher returns; a goroutine would still be pending here.
		if len(seen) != i+1 {
			t.Fatalf("after dispatching %d messages the handler had run %d times, want %d", i+1, len(seen), i+1)
		}
	}

	if !reflect.DeepEqual(seen, want) {
		t.Fatalf("handler saw %v, want %v", seen, want)
	}
}

// Handlers that do not opt in keep the goroutine-per-message dispatch, so a
// slow one cannot head-of-line block everything queued behind it.
func TestDefaultRPCHandlerStillDispatchesAsynchronously(t *testing.T) {
	const method = "testAsyncHandler"

	called := make(chan struct{})
	release := make(chan struct{})
	rpcHandlers[method] = RPCHandler{
		Func: func() error {
			close(called)
			<-release
			return nil
		},
	}
	t.Cleanup(func() {
		close(release)
		delete(rpcHandlers, method)
	})

	session := newRPCTestSession(t)
	// Returns while the handler is still blocked on release; an inline call
	// would deadlock here instead.
	onRPCMessage(rpcTestMessage(t, method, nil), session)

	select {
	case <-called:
	case <-time.After(5 * time.Second):
		t.Fatal("asynchronous handler was never dispatched")
	}
}
