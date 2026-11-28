package timinglog

import (
	"strings"
	"sync/atomic"

	"github.com/pion/interceptor"
	"github.com/pion/rtp"
)

// VideoTap reads the RTP timestamp of the video packets that one peer
// connection sends. The track makes the timestamps itself, so the sender of
// a frame reads the timestamp of its packets here.
//
// A VideoTap is the interceptor factory and the interceptor of one peer
// connection. It is the last in the registry: a packet that an earlier
// interceptor sends again does not pass it.
type VideoTap struct {
	interceptor.NoOp
	timestamp atomic.Uint32
	packets   atomic.Uint64
}

func (t *VideoTap) NewInterceptor(string) (interceptor.Interceptor, error) {
	return t, nil
}

func (t *VideoTap) BindLocalStream(info *interceptor.StreamInfo, writer interceptor.RTPWriter) interceptor.RTPWriter {
	if !strings.HasPrefix(strings.ToLower(info.MimeType), "video/") {
		return writer
	}
	return interceptor.RTPWriterFunc(func(header *rtp.Header, payload []byte, attributes interceptor.Attributes) (int, error) {
		t.timestamp.Store(header.Timestamp)
		t.packets.Add(1)
		return writer.Write(header, payload, attributes)
	})
}

// Last returns the RTP timestamp of the newest video packet, and the number
// of video packets until now. A sender that sees the number change across
// the write of a frame has the timestamp of that frame.
func (t *VideoTap) Last() (timestamp uint32, packets uint64) {
	if t == nil {
		return 0, 0
	}
	return t.timestamp.Load(), t.packets.Load()
}
