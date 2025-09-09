//go:build cgo
// +build cgo

package audio

import (
	"sync/atomic"
)

// AudioBufferPool provides a simple buffer pool for audio processing
type AudioBufferPool struct {
	// Atomic counters
	hitCount  int64 // Pool hit counter (atomic)
	missCount int64 // Pool miss counter (atomic)

	// Pool configuration
	bufferSize int
	pool       chan []byte
	maxSize    int
}

// NewAudioBufferPool creates a new simple audio buffer pool
func NewAudioBufferPool(bufferSize int) *AudioBufferPool {
	maxSize := Config.MaxPoolSize
	if maxSize <= 0 {
		maxSize = Config.BufferPoolDefaultSize
	}

	pool := &AudioBufferPool{
		bufferSize: bufferSize,
		pool:       make(chan []byte, maxSize),
		maxSize:    maxSize,
	}

	// Pre-populate the pool
	for i := 0; i < maxSize/2; i++ {
		buf := make([]byte, bufferSize)
		select {
		case pool.pool <- buf:
		default:
			break
		}
	}

	return pool
}

// Get retrieves a buffer from the pool
func (p *AudioBufferPool) Get() []byte {
	select {
	case buf := <-p.pool:
		atomic.AddInt64(&p.hitCount, 1)
		return buf[:0] // Reset length but keep capacity
	default:
		atomic.AddInt64(&p.missCount, 1)
		return make([]byte, 0, p.bufferSize)
	}
}

// Put returns a buffer to the pool
func (p *AudioBufferPool) Put(buf []byte) {
	if buf == nil || cap(buf) != p.bufferSize {
		return // Invalid buffer
	}

	// Reset the buffer
	buf = buf[:0]

	// Try to return to pool
	select {
	case p.pool <- buf:
		// Successfully returned to pool
	default:
		// Pool is full, discard buffer
	}
}

// GetStats returns pool statistics
func (p *AudioBufferPool) GetStats() AudioBufferPoolStats {
	hitCount := atomic.LoadInt64(&p.hitCount)
	missCount := atomic.LoadInt64(&p.missCount)
	totalRequests := hitCount + missCount

	var hitRate float64
	if totalRequests > 0 {
		hitRate = float64(hitCount) / float64(totalRequests) * Config.BufferPoolHitRateBase
	}

	return AudioBufferPoolStats{
		BufferSize:  p.bufferSize,
		MaxPoolSize: p.maxSize,
		CurrentSize: int64(len(p.pool)),
		HitCount:    hitCount,
		MissCount:   missCount,
		HitRate:     hitRate,
	}
}

// AudioBufferPoolStats represents pool statistics
type AudioBufferPoolStats struct {
	BufferSize  int
	MaxPoolSize int
	CurrentSize int64
	HitCount    int64
	MissCount   int64
	HitRate     float64
}

// Global buffer pools
var (
	audioFramePool   = NewAudioBufferPool(Config.AudioFramePoolSize)
	audioControlPool = NewAudioBufferPool(Config.BufferPoolControlSize)
)

// GetAudioFrameBuffer gets a buffer for audio frames
func GetAudioFrameBuffer() []byte {
	return audioFramePool.Get()
}

// PutAudioFrameBuffer returns a buffer to the frame pool
func PutAudioFrameBuffer(buf []byte) {
	audioFramePool.Put(buf)
}

// GetAudioControlBuffer gets a buffer for control messages
func GetAudioControlBuffer() []byte {
	return audioControlPool.Get()
}

// PutAudioControlBuffer returns a buffer to the control pool
func PutAudioControlBuffer(buf []byte) {
	audioControlPool.Put(buf)
}

// GetAudioBufferPoolStats returns statistics for all pools
func GetAudioBufferPoolStats() map[string]AudioBufferPoolStats {
	return map[string]AudioBufferPoolStats{
		"frame_pool":   audioFramePool.GetStats(),
		"control_pool": audioControlPool.GetStats(),
	}
}
