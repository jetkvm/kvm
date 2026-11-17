package audio

import (
	"context"
	"time"
)

// LiveQueue decouples network reception from USB backpressure. It has one
// producer and one consumer and keeps at most three recent audio packets.
type LiveQueue struct{ packets chan livePacket }
type livePacket struct {
	data     []byte
	received time.Time
}

func NewLiveQueue() *LiveQueue { return &LiveQueue{packets: make(chan livePacket, 3)} }
func (q *LiveQueue) Offer(data []byte) {
	p := livePacket{append([]byte(nil), data...), time.Now()}
	select {
	case q.packets <- p:
		return
	default:
	}
	select {
	case <-q.packets:
	default:
	}
	q.packets <- p
}

// Close must be called only by the producer, after its final Offer.
func (q *LiveQueue) Close() { close(q.packets) }
func (q *LiveQueue) Next(ctx context.Context) ([]byte, bool) {
	for {
		if ctx.Err() != nil {
			return nil, false
		}
		select {
		case <-ctx.Done():
			return nil, false
		case p, ok := <-q.packets:
			if !ok {
				return nil, false
			}
			if time.Since(p.received) <= 120*time.Millisecond {
				return p.data, true
			}
		}
	}
}
