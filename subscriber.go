package kvm

import (
	"sync"
	"sync/atomic"
)

// Subscriber is a generic pub-sub broadcaster for frame/data channels.
// It provides thread-safe subscription management with fast-path count checking.
type Subscriber[T any] struct {
	mu    sync.RWMutex
	subs  []chan T
	count atomic.Int32
}

// Subscribe creates a new channel and registers it for receiving broadcasts.
// The channel is buffered with the specified size.
func (s *Subscriber[T]) Subscribe(bufferSize int) <-chan T {
	ch := make(chan T, bufferSize)
	s.mu.Lock()
	s.subs = append(s.subs, ch)
	s.count.Add(1)
	s.mu.Unlock()
	return ch
}

// Unsubscribe removes and closes the given channel from the subscriber list.
func (s *Subscriber[T]) Unsubscribe(ch <-chan T) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, sub := range s.subs {
		if sub == ch {
			s.subs = append(s.subs[:i], s.subs[i+1:]...)
			s.count.Add(-1)
			close(sub)
			return
		}
	}
}

// Broadcast sends data to all subscribers without blocking.
// If a subscriber's channel is full, the data is dropped for that subscriber.
func (s *Subscriber[T]) Broadcast(data T) {
	if s.count.Load() == 0 {
		return // Fast-path: no subscribers
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, ch := range s.subs {
		select {
		case ch <- data:
		default:
		}
	}
}

// HasSubscribers returns true if there are any active subscribers.
func (s *Subscriber[T]) HasSubscribers() bool {
	return s.count.Load() > 0
}

// Count returns the current number of subscribers.
func (s *Subscriber[T]) Count() int32 {
	return s.count.Load()
}
