package audio

import (
	"sync"
	"sync/atomic"
)

// SizedBufferPool manages a pool of buffers with size tracking
type SizedBufferPool struct {
	// The underlying sync.Pool
	pool sync.Pool

	// Statistics for monitoring
	totalBuffers atomic.Int64
	totalBytes   atomic.Int64
	gets         atomic.Int64
	puts         atomic.Int64
	misses       atomic.Int64

	// Configuration
	maxBufferSize int
	defaultSize   int
}

// NewSizedBufferPool creates a new sized buffer pool
func NewSizedBufferPool(defaultSize, maxBufferSize int) *SizedBufferPool {
	pool := &SizedBufferPool{
		maxBufferSize: maxBufferSize,
		defaultSize:   defaultSize,
	}

	pool.pool = sync.Pool{
		New: func() interface{} {
			// Track pool misses
			pool.misses.Add(1)

			// Create new buffer with default size
			buf := make([]byte, defaultSize)

			// Return pointer-like to avoid allocations
			slice := buf[:0]
			ptrSlice := &slice

			// Track statistics
			pool.totalBuffers.Add(1)
			pool.totalBytes.Add(int64(cap(buf)))

			return ptrSlice
		},
	}

	return pool
}

// Get returns a buffer from the pool with at least the specified capacity
func (p *SizedBufferPool) Get(minCapacity int) []byte {
	// Track gets
	p.gets.Add(1)

	// Get buffer from pool - handle pointer-like storage
	var buf []byte
	poolObj := p.pool.Get()
	switch v := poolObj.(type) {
	case *[]byte:
		// Handle pointer-like storage from Put method
		if v != nil {
			buf = (*v)[:0] // Get the underlying slice
		} else {
			buf = make([]byte, 0, p.defaultSize)
		}
	case []byte:
		// Handle direct slice for backward compatibility
		buf = v
	default:
		// Fallback for unexpected types
		buf = make([]byte, 0, p.defaultSize)
		p.misses.Add(1)
	}

	// Check if buffer has sufficient capacity
	if cap(buf) < minCapacity {
		// Track statistics for the old buffer
		p.totalBytes.Add(-int64(cap(buf)))

		// Allocate new buffer with required capacity
		buf = make([]byte, minCapacity)

		// Track statistics for the new buffer
		p.totalBytes.Add(int64(cap(buf)))
	} else {
		// Resize existing buffer
		buf = buf[:minCapacity]
	}

	return buf
}

// Put returns a buffer to the pool
func (p *SizedBufferPool) Put(buf []byte) {
	// Track statistics
	p.puts.Add(1)

	// Don't pool excessively large buffers to prevent memory bloat
	if cap(buf) > p.maxBufferSize {
		// Track statistics
		p.totalBuffers.Add(-1)
		p.totalBytes.Add(-int64(cap(buf)))
		return
	}

	// Clear buffer contents for security
	for i := range buf {
		buf[i] = 0
	}

	// Return to pool - use pointer-like approach to avoid allocations
	slice := buf[:0]
	p.pool.Put(&slice)
}

// GetStats returns statistics about the buffer pool
func (p *SizedBufferPool) GetStats() (buffers, bytes, gets, puts, misses int64) {
	buffers = p.totalBuffers.Load()
	bytes = p.totalBytes.Load()
	gets = p.gets.Load()
	puts = p.puts.Load()
	misses = p.misses.Load()
	return
}

// BufferPoolStats contains statistics about a buffer pool
type BufferPoolStats struct {
	TotalBuffers      int64
	TotalBytes        int64
	Gets              int64
	Puts              int64
	Misses            int64
	HitRate           float64
	AverageBufferSize float64
}

// GetDetailedStats returns detailed statistics about the buffer pool
func (p *SizedBufferPool) GetDetailedStats() BufferPoolStats {
	buffers := p.totalBuffers.Load()
	bytes := p.totalBytes.Load()
	gets := p.gets.Load()
	puts := p.puts.Load()
	misses := p.misses.Load()

	// Calculate hit rate
	hitRate := 0.0
	if gets > 0 {
		hitRate = float64(gets-misses) / float64(gets) * 100.0
	}

	// Calculate average buffer size
	avgSize := 0.0
	if buffers > 0 {
		avgSize = float64(bytes) / float64(buffers)
	}

	return BufferPoolStats{
		TotalBuffers:      buffers,
		TotalBytes:        bytes,
		Gets:              gets,
		Puts:              puts,
		Misses:            misses,
		HitRate:           hitRate,
		AverageBufferSize: avgSize,
	}
}

// Global audio buffer pools with different size classes
var (
	// Small buffers (up to 4KB)
	smallBufferPool = NewSizedBufferPool(1024, 4*1024)

	// Medium buffers (4KB to 64KB)
	mediumBufferPool = NewSizedBufferPool(8*1024, 64*1024)

	// Large buffers (64KB to 1MB)
	largeBufferPool = NewSizedBufferPool(64*1024, 1024*1024)
)

// GetOptimalBuffer returns a buffer from the most appropriate pool based on size
func GetOptimalBuffer(size int) []byte {
	switch {
	case size <= 4*1024:
		return smallBufferPool.Get(size)
	case size <= 64*1024:
		return mediumBufferPool.Get(size)
	default:
		return largeBufferPool.Get(size)
	}
}

// ReturnOptimalBuffer returns a buffer to the appropriate pool based on size
func ReturnOptimalBuffer(buf []byte) {
	size := cap(buf)
	switch {
	case size <= 4*1024:
		smallBufferPool.Put(buf)
	case size <= 64*1024:
		mediumBufferPool.Put(buf)
	default:
		largeBufferPool.Put(buf)
	}
}

// GetAllPoolStats returns statistics for all buffer pools
func GetAllPoolStats() map[string]BufferPoolStats {
	return map[string]BufferPoolStats{
		"small":  smallBufferPool.GetDetailedStats(),
		"medium": mediumBufferPool.GetDetailedStats(),
		"large":  largeBufferPool.GetDetailedStats(),
	}
}
