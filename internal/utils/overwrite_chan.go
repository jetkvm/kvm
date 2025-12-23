package utils

// OverwriteChan is a channel that overwrites the oldest value(s) when full
// Use when you want to keep only the latest n values and have non-blocking sends.
type OverwriteChan[T any] struct {
	ch chan T
}

func NewOverwriteChan[T any](size int) *OverwriteChan[T] {
	// Ensure it has a capped capacity of at least 1
	if size < 1 {
		size = 1
	}

	return &OverwriteChan[T]{
		ch: make(chan T, size),
	}
}

// Send adds the value, overwriting (discarding oldest entries) if the channel is full.
// Non-blocking.
func (oc *OverwriteChan[T]) Send(v T) {
	for {
		// Send the value (non-blockingly).
		select {
		case oc.ch <- v:
			// Sent successfully (chan had space).
			return
		default:
			// It was full, discard oldest (non-blockingly), then loop back to try again.
			select {
			case <-oc.ch:
				// drained oldest value, there will be space now
			default:
				// someone else drained it, there is space now
			}
		}
	}
}

// Closes the underlying channel.
func (oc *OverwriteChan[T]) Close() {
	close(oc.ch)
}

// Chan returns the underlying receive-only channel for consumers.
func (oc *OverwriteChan[T]) Chan() <-chan T {
	return oc.ch
}
