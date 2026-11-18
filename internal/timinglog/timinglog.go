// Package timinglog keeps the times of two events on one clock: a keyboard
// report was written to USB, and the last byte of an OS Services video frame
// was there. A tool that measures the time from a key to the frame that shows
// it reads both times here, so the network to the KVM is not part of its
// result.
//
// The log has memory only between Start and Stop, and keeps the newest
// entries.
package timinglog

import (
	"sync"
	"time"
)

const (
	keyCapacity   = 256
	frameCapacity = 1024
)

// Log is the timing log as a tool reads it. Each entry is a time in
// microseconds and a value, the oldest entry first.
type Log struct {
	Running bool `json:"running"`
	// KeyCount and FrameCount count every entry since Start. A count above
	// the number of entries tells that the oldest are gone.
	KeyCount   uint32 `json:"keyCount"`
	FrameCount uint32 `json:"frameCount"`
	// Keys: when the report was written, and the code of its first key, 0
	// when no key is down.
	Keys [][2]int64 `json:"keys"`
	// Frames: when the frame was complete, and the RTP timestamp of its
	// packets.
	Frames [][2]int64 `json:"frames"`
}

type entry struct {
	us    int64
	value uint32
}

// ring holds the newest entries. count is every entry since Start; the
// newest is at count - 1, modulo the capacity. Both capacities divide 2^32,
// so a count that wraps stays in step.
type ring struct {
	entries []entry
	count   uint32
}

func (r *ring) add(at time.Time, value uint32) {
	if r.entries == nil {
		return
	}
	r.entries[r.count%uint32(len(r.entries))] = entry{us: at.Sub(epoch).Microseconds(), value: value}
	r.count++
}

func (r *ring) read() [][2]int64 {
	capacity := uint32(len(r.entries))
	held := min(r.count, capacity)
	out := make([][2]int64, 0, held)
	for i := range held {
		e := r.entries[(r.count-held+i)%capacity]
		out = append(out, [2]int64{e.us, int64(e.value)})
	}
	return out
}

var (
	// epoch is the zero of the log's clock.
	epoch = time.Now()

	mu     sync.Mutex
	keys   ring
	frames ring
)

// Start starts an empty log.
func Start() {
	mu.Lock()
	defer mu.Unlock()
	keys = ring{entries: make([]entry, keyCapacity)}
	frames = ring{entries: make([]entry, frameCapacity)}
}

// Stop stops the log and frees its memory.
func Stop() {
	mu.Lock()
	defer mu.Unlock()
	keys = ring{}
	frames = ring{}
}

// Key logs a keyboard report: the code of its first key, and when it was
// written to USB.
func Key(code byte, written time.Time) {
	mu.Lock()
	defer mu.Unlock()
	keys.add(written, uint32(code))
}

// Frame logs an OS Services video frame: the RTP timestamp of its packets,
// and when its last byte was there.
func Frame(rtpTimestamp uint32, complete time.Time) {
	mu.Lock()
	defer mu.Unlock()
	frames.add(complete, rtpTimestamp)
}

// Read returns the log.
func Read() Log {
	mu.Lock()
	defer mu.Unlock()
	return Log{
		Running:    keys.entries != nil,
		KeyCount:   keys.count,
		FrameCount: frames.count,
		Keys:       keys.read(),
		Frames:     frames.read(),
	}
}

// Answer does the action of a request and returns the log after it: "start"
// starts an empty log, "stop" stops the log, and each other action only
// reads.
func Answer(action string) Log {
	switch action {
	case "start":
		Start()
	case "stop":
		Stop()
	}
	return Read()
}
