package kvm

import (
	"github.com/pion/interceptor"
	"github.com/pion/rtp"
)

const playoutDelayExtensionURI = "http://www.webrtc.org/experiments/rtp-hdrext/playout-delay"

// playoutDelayInterceptorFactory creates PlayoutDelayInterceptor instances.
type playoutDelayInterceptorFactory struct{}

func (f *playoutDelayInterceptorFactory) NewInterceptor(_ string) (interceptor.Interceptor, error) {
	return &playoutDelayInterceptor{}, nil
}

// playoutDelayInterceptor adds the playout-delay RTP header extension to
// outgoing video packets, telling the browser to minimize its jitter buffer.
//
// Extension format: 3 bytes = (min_delay_10ms << 12) | max_delay_10ms
// Setting both to 0 requests minimum playout delay.
type playoutDelayInterceptor struct {
	interceptor.NoOp
}

func (p *playoutDelayInterceptor) BindLocalStream(
	info *interceptor.StreamInfo,
	writer interceptor.RTPWriter,
) interceptor.RTPWriter {
	var hdrExtID uint8
	for _, e := range info.RTPHeaderExtensions {
		if e.URI == playoutDelayExtensionURI {
			hdrExtID = uint8(e.ID) //nolint:gosec
			break
		}
	}
	if hdrExtID == 0 {
		return writer
	}

	// min=0, max=0: request minimum playout delay (3 bytes, all zeros)
	ext := []byte{0x00, 0x00, 0x00}

	return interceptor.RTPWriterFunc(
		func(header *rtp.Header, payload []byte, attributes interceptor.Attributes) (int, error) {
			if err := header.SetExtension(hdrExtID, ext); err != nil {
				return 0, err
			}
			return writer.Write(header, payload, attributes)
		},
	)
}
