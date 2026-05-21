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

// URI is the standard WebRTC playout-delay extension identifier. Registering
// this URI with the MediaEngine causes pion to negotiate it via SDP; the
// receiver (Chrome) honours it without any browser-side configuration.
const URI = "http://www.webrtc.org/experiments/rtp-hdrext/playout-delay"

// Factory creates playout-delay interceptors. Register it on a
// pion interceptor.Registry alongside the default interceptors.
type Factory struct {
	// MinDelay10ms is the minimum playout delay in 10 ms units.
	// 0 means "no minimum buffering".
	MinDelay10ms uint16
	// MaxDelay10ms is the maximum playout delay in 10 ms units.
	// 0 means "no buffering allowed beyond the decoder requirement".
	MaxDelay10ms uint16
}

// NewFactory builds a Factory that pins both bounds at zero, which is what
// every JetKVM call site wants — interactive latency, no jitter masking.
func NewFactory() *Factory {
	return &Factory{MinDelay10ms: 0, MaxDelay10ms: 0}
}

// NewInterceptor satisfies interceptor.Factory.
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

// BindLocalStream wires the playout-delay extension onto every outgoing RTP
// packet for this stream. The extension ID is whatever pion negotiated for
// the URI in SDP — we look it up once and reuse it per packet.
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
		// Extension wasn't negotiated for this stream (e.g. the browser
		// didn't include it in the SDP answer). Nothing to do.
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

// encode packs the 3-byte playout-delay extension body:
//
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//	|       MIN delay       |       MAX delay       |
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//
// 12 bits each, big-endian.
func encode(minDelay10ms, maxDelay10ms uint16) []byte {
	min12 := minDelay10ms & 0x0FFF
	max12 := maxDelay10ms & 0x0FFF
	return []byte{
		byte(min12 >> 4),
		byte(min12<<4) | byte(max12>>8),
		byte(max12),
	}
}
