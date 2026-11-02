package kvm

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/jetkvm/kvm/internal/native"
	"github.com/pion/webrtc/v4"
)

type pauseRecordingNative struct {
	native.EmptyNativeInterface
	calls []string
}

func (n *pauseRecordingNative) VideoStop() error  { n.calls = append(n.calls, "stop"); return nil }
func (n *pauseRecordingNative) VideoStart() error { n.calls = append(n.calls, "start"); return nil }
func (n *pauseRecordingNative) VideoSetCodecType(codec int) error {
	if codec == 1 {
		n.calls = append(n.calls, "h265")
	} else {
		n.calls = append(n.calls, "h264")
	}
	return nil
}

// Removing session injection would let a replacement resume another peer's
// held pause. Native calls are isolated at the actual hardware boundary.
func TestVideoPauseRetainsSessionOwnership(t *testing.T) {
	a := &Session{done: make(chan struct{}), RPCChannel: &webrtc.DataChannel{}}
	b := &Session{done: make(chan struct{}), RPCChannel: &webrtc.DataChannel{}, codecMimeType: webrtc.MimeTypeH265}
	oldNative, oldCurrent, oldPause := nativeInstance, currentSession, videoPausedBy
	n := &pauseRecordingNative{}
	nativeInstance, currentSession, videoPausedBy = n, b, nil
	t.Cleanup(func() { nativeInstance, currentSession, videoPausedBy = oldNative, oldCurrent, oldPause })
	pause := func(s *Session, value string) {
		onRPCMessage(webrtc.DataChannelMessage{Data: []byte(`{"jsonrpc":"2.0","method":"setVideoStreamPaused","params":{"paused":` + value + `},"id":1}`)}, s)
	}
	pause(a, "true")
	pause(b, "false")
	if videoPausedBy != a || !reflect.DeepEqual(n.calls, []string{"stop"}) {
		t.Fatalf("replacement resumed another session's pause: owner=%p calls=%v", videoPausedBy, n.calls)
	}
	pause(a, "false")
	if videoPausedBy != nil || !reflect.DeepEqual(n.calls, []string{"stop", "h265", "start"}) {
		t.Fatalf("owner resume did not use current session codec: owner=%p calls=%v", videoPausedBy, n.calls)
	}
	pause(b, "true")
	releaseVideoPause(a)
	if videoPausedBy != b || len(n.calls) != 4 {
		t.Fatal("late old-session teardown released replacement pause")
	}
}

// Reproduce the admission/cleanup/handler ordering without scheduler timing:
// onRPCMessage admits the open caller, then cleanup completes before the real
// session-aware handler acquires its ownership mutex.
func TestAdmittedVideoPauseCannotOutliveClosedCaller(t *testing.T) {
	for _, paused := range []bool{true, false} {
		t.Run(map[bool]string{true: "pause", false: "resume"}[paused], func(t *testing.T) {
			a := &Session{done: make(chan struct{}), RPCChannel: &webrtc.DataChannel{}}
			b := &Session{done: make(chan struct{}), codecMimeType: webrtc.MimeTypeH265}
			oldNative, oldCurrent, oldPause := nativeInstance, currentSession, videoPausedBy
			n := &pauseRecordingNative{}
			nativeInstance, currentSession, videoPausedBy = n, b, nil
			oldHandler := rpcHandlers["setVideoStreamPaused"]
			t.Cleanup(func() {
				nativeInstance, currentSession, videoPausedBy = oldNative, oldCurrent, oldPause
				rpcHandlers["setVideoStreamPaused"] = oldHandler
			})
			admitted, cleanupDone, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
			rpcHandlers["setVideoStreamPaused"] = RPCHandler{
				Func: func(caller *Session, value bool) error {
					close(admitted)
					<-cleanupDone
					return rpcSetVideoStreamPaused(caller, value)
				}, Params: []string{"paused"},
			}
			value := "false"
			if paused {
				value = "true"
			}
			go func() {
				defer close(finished)
				onRPCMessage(webrtc.DataChannelMessage{Data: []byte(`{"jsonrpc":"2.0","method":"setVideoStreamPaused","params":{"paused":` + value + `},"id":1}`)}, a)
			}()
			select {
			case <-admitted:
			case <-time.After(time.Second):
				close(cleanupDone)
				<-finished
				t.Fatal("request was not admitted while caller was open")
			}
			a.close()
			releaseVideoPause(a)
			close(cleanupDone)
			<-finished
			if videoPausedBy != nil || len(n.calls) != 0 {
				t.Fatalf("late closed caller mutated video: owner=%p calls=%v", videoPausedBy, n.calls)
			}
			if err := rpcSetVideoStreamPaused(b, false); err != nil || !reflect.DeepEqual(n.calls, []string{"h265", "start"}) {
				t.Fatalf("replacement could not resume: err=%v calls=%v", err, n.calls)
			}
		})
	}
}

func TestNilOrClosedVideoPauseHasNoSideEffects(t *testing.T) {
	closed := &Session{done: make(chan struct{})}
	closed.close()
	b := &Session{done: make(chan struct{}), codecMimeType: webrtc.MimeTypeH265}
	oldNative, oldCurrent, oldPause := nativeInstance, currentSession, videoPausedBy
	n := &pauseRecordingNative{}
	nativeInstance, currentSession, videoPausedBy = n, b, nil
	t.Cleanup(func() { nativeInstance, currentSession, videoPausedBy = oldNative, oldCurrent, oldPause })
	for _, caller := range []*Session{nil, closed} {
		for _, paused := range []bool{true, false} {
			if err := rpcSetVideoStreamPaused(caller, paused); !errors.Is(err, context.Canceled) {
				t.Errorf("invalid caller error: caller=%p paused=%v err=%v", caller, paused, err)
			}
			if videoPausedBy != nil || len(n.calls) != 0 {
				t.Errorf("invalid caller changed owner/native state: owner=%p calls=%v", videoPausedBy, n.calls)
			}
			videoPausedBy, n.calls = nil, nil
		}
	}
}
