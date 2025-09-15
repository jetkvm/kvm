package audio

import (
	"sync"
)

// SimpleBufferPool manages a pool of fixed-size buffers
// Analysis shows 99% of requests are for maxPCMBufferSize, so we simplify to fixed-size
type SimpleBufferPool struct {
	pool sync.Pool
}

// NewSimpleBufferPool creates a new simple buffer pool for fixed-size buffers
func NewSimpleBufferPool(bufferSize int) *SimpleBufferPool {
	return &SimpleBufferPool{
		pool: sync.Pool{
			New: func() interface{} {
				buf := make([]byte, 0, bufferSize)
				return &buf
			},
		},
	}
}

// Get returns a buffer from the pool
func (p *SimpleBufferPool) Get() []byte {
	poolObj := p.pool.Get()
	switch v := poolObj.(type) {
	case *[]byte:
		if v != nil {
			buf := *v
			return buf[:0] // Reset length but keep capacity
		}
	case []byte:
		return v[:0] // Handle direct slice for backward compatibility
	}
	// Fallback for unexpected types or nil
	return make([]byte, 0) // Will be resized by caller if needed
}

// Put returns a buffer to the pool
func (p *SimpleBufferPool) Put(buf []byte) {
	if buf == nil {
		return
	}
	// Clear and reset the buffer
	buf = buf[:0]
	// Use pointer to avoid allocations as recommended by staticcheck
	p.pool.Put(&buf)
}

// Global simple buffer pool - sized for maxPCMBufferSize since that's 99% of usage
var GlobalBufferPool *SimpleBufferPool
