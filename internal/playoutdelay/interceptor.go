// Package playoutdelay implements the WebRTC playout-delay RTP header
// extension (http://www.webrtc.org/experiments/rtp-hdrext/playout-delay).
//
// Chrome's adaptive jitter buffer is one-way: it grows when packet timing
// gets jittery (e.g. the JetKVM H.264 encoder emitting variable-size frames
// during high-motion content like fullscreen YouTube on the host) and
// stubbornly refuses to shrink back, leaving the "Playback Delay" graph
// stuck at hundreds of milliseconds until the page is reloaded. Receiver-
// side knobs like jitterBufferTarget / playoutDelayHint /
// setMinimumJitterBufferDelay all cap the steady-state floor but cannot
// pull a ratcheted buffer back down.
//
// The playout-delay extension is the sender-side counterpart: each outgoing
// RTP packet carries the desired minimum and maximum playout delay (in
// 10 ms increments). Chrome honours it as an authoritative override of its
// adaptive logic. We send min=max=0 on every video packet, which keeps the
// receiver pinned at the absolute floor.
//
// Reference: https://webrtc.googlesource.com/src/+/HEAD/docs/native-code/rtp-hdrext/playout-delay/README.md
package playoutdelay

import (
	"github.com/pion/interceptor"
	"github.com/pion/rtp"
)

const URI = "http://www.webrtc.org/experiments/rtp-hdrext/playout-delay"

// Factory creates playout-delay interceptors with the given min/max bounds
// in 10 ms units. JetKVM uses min=max=0 — no buffering beyond decoder needs.
type Factory struct {
	MinDelay10ms uint16
	MaxDelay10ms uint16
}

func NewFactory() *Factory {
	return &Factory{MinDelay10ms: 0, MaxDelay10ms: 0}
}

func (f *Factory) NewInterceptor(_ string) (interceptor.Interceptor, error) {
	return &playoutDelayInterceptor{
		minDelay10ms: f.MinDelay10ms,
		maxDelay10ms: f.MaxDelay10ms,
	}, nil
}

type playoutDelayInterceptor struct {
	interceptor.NoOp
	minDelay10ms uint16
	maxDelay10ms uint16
}

func (i *playoutDelayInterceptor) BindLocalStream(
	info *interceptor.StreamInfo,
	writer interceptor.RTPWriter,
) interceptor.RTPWriter {
	var extID uint8
	for _, ext := range info.RTPHeaderExtensions {
		if ext.URI == URI {
			extID = uint8(ext.ID) //nolint:gosec // SDP IDs are 1..14
			break
		}
	}
	if extID == 0 {
		return writer
	}

	payload := encode(i.minDelay10ms, i.maxDelay10ms)
	return interceptor.RTPWriterFunc(func(
		header *rtp.Header,
		rtpPayload []byte,
		attributes interceptor.Attributes,
	) (int, error) {
		if err := header.SetExtension(extID, payload); err != nil {
			return 0, err
		}
		return writer.Write(header, rtpPayload, attributes)
	})
}

// encode packs the 3-byte body: 12 bits MIN, 12 bits MAX, big-endian.
func encode(minDelay10ms, maxDelay10ms uint16) []byte {
	min12 := minDelay10ms & 0x0FFF
	max12 := maxDelay10ms & 0x0FFF
	return []byte{
		byte(min12 >> 4),
		byte(min12<<4) | byte(max12>>8),
		byte(max12),
	}
}
