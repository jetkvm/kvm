//go:build linux && arm

package kvm

import (
	"math"
	"testing"
	"time"

	"github.com/jetkvm/kvm/internal/native"
	"github.com/pion/rtcp"
	"github.com/pion/webrtc/v4"
)

func TestReceiverVideoBitrate(t *testing.T) {
	for _, tt := range []struct {
		name    string
		bitrate float32
		sources []uint32
		want    uint32
	}{
		{"matching video", 1500000, []uint32{8, 42}, 1500000},
		{"other stream", 1500000, []uint32{8}, 0},
		{"no sources", 1500000, nil, 0},
		{"zero estimate", 0, []uint32{42}, 1},
		{"negative", -1, []uint32{42}, 0},
		{"nan", float32(math.NaN()), []uint32{42}, 0},
		{"infinite", float32(math.Inf(1)), []uint32{42}, 0},
		{"overflow", 1e15, []uint32{42}, math.MaxUint32},
	} {
		t.Run(tt.name, func(t *testing.T) {
			packet := &rtcp.ReceiverEstimatedMaximumBitrate{Bitrate: tt.bitrate, SSRCs: tt.sources}
			if got := receiverVideoBitrate(packet, 42); got != tt.want {
				t.Fatalf("got %d, want %d", got, tt.want)
			}
		})
	}
}

type rembNative struct {
	native.EmptyNativeInterface
	limit   uint32
	updates chan uint32
}

func (n *rembNative) VideoSetREMB(limit uint32) error {
	n.limit = limit
	if n.updates != nil {
		n.updates <- limit
	}
	return nil
}

func TestVideoREMBCleansUpReceivers(t *testing.T) {
	original := nativeInstance
	n := &rembNative{}
	nativeInstance = n
	t.Cleanup(func() { nativeInstance = original; clear(videoREMB.estimates) })
	a, b := &webrtc.RTPSender{}, &webrtc.RTPSender{}
	updateVideoREMB(a, 4000000)
	updateVideoREMB(b, 1000000)
	if n.limit != 1000000 {
		t.Fatalf("shared encoder limit = %d", n.limit)
	}
	updateVideoREMB(b, 2000000)
	if n.limit != 2000000 {
		t.Fatalf("recovered limit = %d", n.limit)
	}
	updateVideoREMB(b, 0)
	if n.limit != 4000000 {
		t.Fatalf("remaining receiver limit = %d", n.limit)
	}
	updateVideoREMB(a, 0)
	if n.limit != 0 {
		t.Fatalf("stale limit after disconnect = %d", n.limit)
	}
}

// Exercise the actual RTCP reader over a negotiated WebRTC connection, including
// disconnect cleanup, rather than only testing the packet helper in isolation.
func TestVideoREMBFeedback(t *testing.T) {
	original := nativeInstance
	updates := make(chan uint32, 8)
	nativeInstance = &rembNative{updates: updates}
	defer func() { nativeInstance = original }()
	senderPC, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	defer senderPC.Close()
	receiverPC, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	defer receiverPC.Close()
	track, err := webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264}, "video", "test")
	if err != nil {
		t.Fatal(err)
	}
	sender, err := senderPC.AddTrack(track)
	if err != nil {
		t.Fatal(err)
	}
	connected := make(chan struct{})
	receiverPC.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		if state == webrtc.PeerConnectionStateConnected {
			close(connected)
		}
	})
	offer, err := senderPC.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	gathered := webrtc.GatheringCompletePromise(senderPC)
	if err = senderPC.SetLocalDescription(offer); err != nil {
		t.Fatal(err)
	}
	<-gathered
	if err = receiverPC.SetRemoteDescription(*senderPC.LocalDescription()); err != nil {
		t.Fatal(err)
	}
	answer, err := receiverPC.CreateAnswer(nil)
	if err != nil {
		t.Fatal(err)
	}
	gathered = webrtc.GatheringCompletePromise(receiverPC)
	if err = receiverPC.SetLocalDescription(answer); err != nil {
		t.Fatal(err)
	}
	<-gathered
	if err = senderPC.SetRemoteDescription(*receiverPC.LocalDescription()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-connected:
	case <-time.After(10 * time.Second):
		t.Fatal("peers did not connect")
	}
	done := make(chan struct{})
	go func() { defer close(done); readVideoRTCP(sender) }()
	defer func() { _ = senderPC.Close(); <-done }()
	ssrc := uint32(sender.GetParameters().Encodings[0].SSRC)
	for _, bitrate := range []uint32{1000000, 5000000} {
		err = receiverPC.WriteRTCP([]rtcp.Packet{&rtcp.ReceiverEstimatedMaximumBitrate{Bitrate: float32(bitrate), SSRCs: []uint32{ssrc}}})
		if err != nil {
			t.Fatal(err)
		}
		select {
		case got := <-updates:
			if got != bitrate {
				t.Fatalf("native limit = %d, want %d", got, bitrate)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("REMB did not reach native encoder")
		}
	}
	if err = senderPC.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-updates:
		if got != 0 {
			t.Fatalf("disconnect left limit %d", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("disconnect did not clear REMB")
	}
}
