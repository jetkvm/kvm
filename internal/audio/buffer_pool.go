package audio

import (
	"sync"
)

// AudioBufferPool manages reusable audio buffers to reduce allocations
type AudioBufferPool struct {
	pool sync.Pool
}

// NewAudioBufferPool creates a new buffer pool for audio frames
func NewAudioBufferPool(bufferSize int) *AudioBufferPool {
	return &AudioBufferPool{
		pool: sync.Pool{
			New: func() interface{} {
				// Pre-allocate buffer with specified size
				return make([]byte, bufferSize)
			},
		},
	}
}

// Get retrieves a buffer from the pool
func (p *AudioBufferPool) Get() []byte {
	if buf := p.pool.Get(); buf != nil {
		return *buf.(*[]byte)
	}
	return make([]byte, 0, 1500) // fallback if pool is empty
}

// Put returns a buffer to the pool
func (p *AudioBufferPool) Put(buf []byte) {
	// Reset length but keep capacity for reuse
	if cap(buf) >= 1500 { // Only pool buffers of reasonable size
		resetBuf := buf[:0]
		p.pool.Put(&resetBuf)
	}
}

// Global buffer pools for different audio operations
var (
	// Pool for 1500-byte audio frame buffers (Opus max frame size)
	audioFramePool = NewAudioBufferPool(1500)

	// Pool for smaller control buffers
	audioControlPool = NewAudioBufferPool(64)
)

// GetAudioFrameBuffer gets a reusable buffer for audio frames
func GetAudioFrameBuffer() []byte {
	return audioFramePool.Get()
}

// PutAudioFrameBuffer returns a buffer to the frame pool
func PutAudioFrameBuffer(buf []byte) {
	audioFramePool.Put(buf)
}

// GetAudioControlBuffer gets a reusable buffer for control data
func GetAudioControlBuffer() []byte {
	return audioControlPool.Get()
}

// PutAudioControlBuffer returns a buffer to the control pool
func PutAudioControlBuffer(buf []byte) {
	audioControlPool.Put(buf)
}
