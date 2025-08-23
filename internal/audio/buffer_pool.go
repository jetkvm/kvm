package audio

import (
	"sync"
)

type AudioBufferPool struct {
	pool       sync.Pool
	bufferSize int
}

func NewAudioBufferPool(bufferSize int) *AudioBufferPool {
	return &AudioBufferPool{
		bufferSize: bufferSize,
		pool: sync.Pool{
			New: func() interface{} {
				return make([]byte, 0, bufferSize)
			},
		},
	}
}

func (p *AudioBufferPool) Get() []byte {
	if buf := p.pool.Get(); buf != nil {
		return *buf.(*[]byte)
	}
	return make([]byte, 0, p.bufferSize)
}

func (p *AudioBufferPool) Put(buf []byte) {
	if cap(buf) >= p.bufferSize {
		resetBuf := buf[:0]
		p.pool.Put(&resetBuf)
	}
}

var (
	audioFramePool   = NewAudioBufferPool(1500)
	audioControlPool = NewAudioBufferPool(64)
)

func GetAudioFrameBuffer() []byte {
	return audioFramePool.Get()
}

func PutAudioFrameBuffer(buf []byte) {
	audioFramePool.Put(buf)
}

func GetAudioControlBuffer() []byte {
	return audioControlPool.Get()
}

func PutAudioControlBuffer(buf []byte) {
	audioControlPool.Put(buf)
}
