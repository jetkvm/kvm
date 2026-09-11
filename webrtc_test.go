//go:build linux && arm

package kvm

import (
	"reflect"
	"strconv"
	"testing"

	"github.com/jetkvm/kvm/internal/native"
	"github.com/jetkvm/kvm/internal/usbgadget"
	"github.com/pion/webrtc/v4"
)

type recordingNative struct {
	native.EmptyNativeInterface
	calls []string
}

func (n *recordingNative) VideoSetCodecType(codecType int) error {
	n.calls = append(n.calls, "set-codec:"+strconv.Itoa(codecType))
	return nil
}

func (n *recordingNative) VideoStart() error {
	n.calls = append(n.calls, "start")
	return nil
}

func TestStartNativeVideoForSessionSetsCodecBeforeStart(t *testing.T) {
	tests := []struct {
		name  string
		codec string
		want  []string
	}{
		{
			name:  "h264",
			codec: webrtc.MimeTypeH264,
			want:  []string{"set-codec:0", "start"},
		},
		{
			name:  "h265",
			codec: webrtc.MimeTypeH265,
			want:  []string{"set-codec:1", "start"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalNative := nativeInstance
			recorder := &recordingNative{}
			nativeInstance = recorder
			t.Cleanup(func() {
				nativeInstance = originalNative
			})

			startNativeVideoForSession(&Session{codecMimeType: tt.codec})

			if !reflect.DeepEqual(recorder.calls, tt.want) {
				t.Fatalf("native calls = %v, want %v", recorder.calls, tt.want)
			}
		})
	}
}

func TestLastSessionDisconnectedPreservesReplacementAudio(t *testing.T) {
	// Do not run in parallel: this exercises the package's global session state.
	track, err := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypePCMU, ClockRate: 8000}, "audio", "replacement")
	if err != nil {
		t.Fatal(err)
	}

	originalNative, originalConfig := nativeInstance, config
	nativeInstance = &recordingNative{} // EmptyNativeInterface disables sleep-mode support.
	config = &Config{VideoSleepAfterSec: -1}
	hostDisplayAdvertiseLock.Lock()
	originalAdvertised := hostDisplayAdvertised
	hostDisplayAdvertised = true // Avoid an EDID update during teardown.
	hostDisplayAdvertiseLock.Unlock()
	usbStateLock.Lock()
	originalUSBState := usbState
	usbState = usbgadget.USBStateNotAttached // Keyboard clear must not touch HID.
	usbStateLock.Unlock()
	activeSessionsMutex.Lock()
	originalSessions := actionSessions
	actionSessions = 1
	activeSessionsMutex.Unlock()
	audioMu.Lock()
	originalTrack, originalCancel, originalStopped := audioTrack, audioCancel, audioStopped
	audioMu.Unlock()
	t.Cleanup(func() {
		audioMu.Lock()
		audioTrack, audioCancel, audioStopped = originalTrack, originalCancel, originalStopped
		audioMu.Unlock()
		activeSessionsMutex.Lock()
		actionSessions = originalSessions
		activeSessionsMutex.Unlock()
		usbStateLock.Lock()
		usbState = originalUSBState
		usbStateLock.Unlock()
		hostDisplayAdvertiseLock.Lock()
		hostDisplayAdvertised = originalAdvertised
		hostDisplayAdvertiseLock.Unlock()
		nativeInstance, config = originalNative, originalConfig
	})

	// Reproduce the interleaving without scheduler timing: old teardown has
	// already performed its owner-specific stop and decided it was last.
	if remaining := decrActiveSessions(); remaining != 0 {
		t.Fatalf("old session left %d sessions, want 0", remaining)
	}
	// Before that teardown resumes, a replacement connects and owns capture.
	if active := incrActiveSessions(); active != 1 {
		t.Fatalf("replacement has %d sessions, want 1", active)
	}
	canceled := false
	stopped := make(chan struct{})
	close(stopped) // No capture goroutine or ALSA device is needed.
	audioMu.Lock()
	audioTrack = track
	audioCancel = func() { canceled = true }
	audioStopped = stopped
	audioMu.Unlock()

	onLastSessionDisconnected()

	audioMu.Lock()
	if canceled {
		t.Error("stale last-session teardown canceled replacement audio capture")
	}
	if audioTrack != track || audioCancel == nil || audioStopped != stopped {
		t.Error("stale last-session teardown discarded replacement audio ownership")
	}
	canceled = false
	audioMu.Unlock()

	// The replacement's own teardown must still release its capture.
	stopAudioIfOwner(track)

	audioMu.Lock()
	defer audioMu.Unlock()
	if !canceled {
		t.Error("owner teardown did not cancel replacement audio capture")
	}
	if audioTrack != nil || audioCancel != nil || audioStopped != nil {
		t.Error("owner teardown did not clear replacement audio ownership")
	}
}
