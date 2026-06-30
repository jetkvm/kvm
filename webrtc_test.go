//go:build linux && arm

package kvm

import (
	"reflect"
	"strconv"
	"testing"

	"github.com/jetkvm/kvm/internal/native"
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
