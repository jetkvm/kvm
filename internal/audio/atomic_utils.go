package audio

import (
	"sync/atomic"
	"time"
)

// AtomicCounter provides thread-safe counter operations
type AtomicCounter struct {
	value int64
}

// NewAtomicCounter creates a new atomic counter
func NewAtomicCounter() *AtomicCounter {
	return &AtomicCounter{}
}

// Add atomically adds delta to the counter and returns the new value
func (c *AtomicCounter) Add(delta int64) int64 {
	return atomic.AddInt64(&c.value, delta)
}

// Increment atomically increments the counter by 1
func (c *AtomicCounter) Increment() int64 {
	return atomic.AddInt64(&c.value, 1)
}

// Load atomically loads the counter value
func (c *AtomicCounter) Load() int64 {
	return atomic.LoadInt64(&c.value)
}

// Store atomically stores a new value
func (c *AtomicCounter) Store(value int64) {
	atomic.StoreInt64(&c.value, value)
}

// Reset atomically resets the counter to zero
func (c *AtomicCounter) Reset() {
	atomic.StoreInt64(&c.value, 0)
}

// Swap atomically swaps the value and returns the old value
func (c *AtomicCounter) Swap(new int64) int64 {
	return atomic.SwapInt64(&c.value, new)
}

// FrameMetrics provides common frame tracking metrics
type FrameMetrics struct {
	Total   *AtomicCounter
	Dropped *AtomicCounter
	Bytes   *AtomicCounter
}

// NewFrameMetrics creates a new frame metrics tracker
func NewFrameMetrics() *FrameMetrics {
	return &FrameMetrics{
		Total:   NewAtomicCounter(),
		Dropped: NewAtomicCounter(),
		Bytes:   NewAtomicCounter(),
	}
}

// RecordFrame atomically records a successful frame with its size
func (fm *FrameMetrics) RecordFrame(size int64) {
	fm.Total.Increment()
	fm.Bytes.Add(size)
}

// RecordDrop atomically records a dropped frame
func (fm *FrameMetrics) RecordDrop() {
	fm.Dropped.Increment()
}

// GetStats returns current metrics values
func (fm *FrameMetrics) GetStats() (total, dropped, bytes int64) {
	return fm.Total.Load(), fm.Dropped.Load(), fm.Bytes.Load()
}

// Reset resets all metrics to zero
func (fm *FrameMetrics) Reset() {
	fm.Total.Reset()
	fm.Dropped.Reset()
	fm.Bytes.Reset()
}

// GetDropRate calculates the drop rate as a percentage
func (fm *FrameMetrics) GetDropRate() float64 {
	total := fm.Total.Load()
	if total == 0 {
		return 0.0
	}
	dropped := fm.Dropped.Load()
	return float64(dropped) / float64(total) * 100.0
}

// LatencyTracker provides atomic latency tracking
type LatencyTracker struct {
	current *AtomicCounter
	min     *AtomicCounter
	max     *AtomicCounter
	average *AtomicCounter
	samples *AtomicCounter
}

// NewLatencyTracker creates a new latency tracker
func NewLatencyTracker() *LatencyTracker {
	lt := &LatencyTracker{
		current: NewAtomicCounter(),
		min:     NewAtomicCounter(),
		max:     NewAtomicCounter(),
		average: NewAtomicCounter(),
		samples: NewAtomicCounter(),
	}
	// Initialize min to max value so first measurement sets it properly
	lt.min.Store(int64(^uint64(0) >> 1)) // Max int64
	return lt
}

// RecordLatency atomically records a new latency measurement
func (lt *LatencyTracker) RecordLatency(latency time.Duration) {
	latencyNanos := latency.Nanoseconds()
	lt.current.Store(latencyNanos)
	lt.samples.Increment()

	// Update min
	for {
		oldMin := lt.min.Load()
		if latencyNanos >= oldMin {
			break
		}
		if atomic.CompareAndSwapInt64(&lt.min.value, oldMin, latencyNanos) {
			break
		}
	}

	// Update max
	for {
		oldMax := lt.max.Load()
		if latencyNanos <= oldMax {
			break
		}
		if atomic.CompareAndSwapInt64(&lt.max.value, oldMax, latencyNanos) {
			break
		}
	}

	// Update average using exponential moving average
	oldAvg := lt.average.Load()
	newAvg := (oldAvg*7 + latencyNanos) / 8 // 87.5% weight to old average
	lt.average.Store(newAvg)
}

// GetLatencyStats returns current latency statistics
func (lt *LatencyTracker) GetLatencyStats() (current, min, max, average time.Duration, samples int64) {
	return time.Duration(lt.current.Load()),
		time.Duration(lt.min.Load()),
		time.Duration(lt.max.Load()),
		time.Duration(lt.average.Load()),
		lt.samples.Load()
}

// PoolMetrics provides common pool performance metrics
type PoolMetrics struct {
	Hits   *AtomicCounter
	Misses *AtomicCounter
}

// NewPoolMetrics creates a new pool metrics tracker
func NewPoolMetrics() *PoolMetrics {
	return &PoolMetrics{
		Hits:   NewAtomicCounter(),
		Misses: NewAtomicCounter(),
	}
}

// RecordHit atomically records a pool hit
func (pm *PoolMetrics) RecordHit() {
	pm.Hits.Increment()
}

// RecordMiss atomically records a pool miss
func (pm *PoolMetrics) RecordMiss() {
	pm.Misses.Increment()
}

// GetHitRate calculates the hit rate as a percentage
func (pm *PoolMetrics) GetHitRate() float64 {
	hits := pm.Hits.Load()
	misses := pm.Misses.Load()
	total := hits + misses
	if total == 0 {
		return 0.0
	}
	return float64(hits) / float64(total) * 100.0
}

// GetStats returns hit and miss counts
func (pm *PoolMetrics) GetStats() (hits, misses int64, hitRate float64) {
	hits = pm.Hits.Load()
	misses = pm.Misses.Load()
	hitRate = pm.GetHitRate()
	return
}
