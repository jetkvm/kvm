package audio

import (
	"context"
	"testing"
	"time"
)

func TestLiveQueueDropsBacklogWhileConsumerStalled(t *testing.T) {
	q := NewLiveQueue()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 100; i++ {
			q.Offer([]byte{byte(i)})
		}
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("network reader blocked on stalled USB")
	}
	q.Close()
	for want := 97; want < 100; want++ {
		got, ok := q.Next(context.Background())
		if !ok || got[0] != byte(want) {
			t.Fatalf("expected recent packet %d, got %v", want, got)
		}
	}
	if _, ok := q.Next(context.Background()); ok {
		t.Fatal("queue did not close")
	}
}
func TestLiveQueueDiscardsExpiredSpeech(t *testing.T) {
	q := NewLiveQueue()
	q.packets <- livePacket{[]byte{1}, time.Now().Add(-time.Second)}
	q.Offer([]byte{2})
	q.Close()
	got, ok := q.Next(context.Background())
	if !ok || got[0] != 2 {
		t.Fatalf("stale speech played: %v", got)
	}
}
func TestLiveQueueOwnsPayloadAndCancels(t *testing.T) {
	q := NewLiveQueue()
	p := []byte{1}
	q.Offer(p)
	p[0] = 2
	got, _ := q.Next(context.Background())
	if got[0] != 1 {
		t.Fatal("payload reused by reader")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, ok := q.Next(ctx); ok {
		t.Fatal("did not cancel")
	}
}

func TestLiveQueueCanceledConsumerDoesNotReplayBufferedSpeech(t *testing.T) {
	q := NewLiveQueue()
	q.Offer([]byte{1})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, ok := q.Next(ctx); ok {
		t.Fatal("canceled consumer replayed buffered speech")
	}
}
