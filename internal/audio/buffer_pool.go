package audio

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/jetkvm/kvm/internal/logging"
)

type AudioBufferPool struct {
	// Atomic fields MUST be first for ARM32 alignment (int64 fields need 8-byte alignment)
	currentSize int64 // Current pool size (atomic)
	hitCount    int64 // Pool hit counter (atomic)
	missCount   int64 // Pool miss counter (atomic)

	// Other fields
	pool        sync.Pool
	bufferSize  int
	maxPoolSize int
	mutex       sync.RWMutex
	// Memory optimization fields
	preallocated []*[]byte // Pre-allocated buffers for immediate use
	preallocSize int       // Number of pre-allocated buffers
}

func NewAudioBufferPool(bufferSize int) *AudioBufferPool {
	// Validate buffer size parameter
	if err := ValidateBufferSize(bufferSize); err != nil {
		// Log validation error and use default value
		logger := logging.GetDefaultLogger().With().Str("component", "AudioBufferPool").Logger()
		logger.Warn().Err(err).Int("bufferSize", bufferSize).Msg("invalid buffer size, using default")
		bufferSize = GetConfig().AudioFramePoolSize
	}

	// Optimize preallocation based on buffer size to reduce memory footprint
	var preallocSize int
	if bufferSize <= GetConfig().AudioFramePoolSize {
		// For frame buffers, use configured percentage
		preallocSize = GetConfig().PreallocPercentage
	} else {
		// For larger buffers, reduce preallocation to save memory
		preallocSize = GetConfig().PreallocPercentage / 2
	}

	// Pre-allocate with exact capacity to avoid slice growth
	preallocated := make([]*[]byte, 0, preallocSize)

	// Pre-allocate buffers with optimized capacity
	for i := 0; i < preallocSize; i++ {
		// Use exact buffer size to prevent over-allocation
		buf := make([]byte, 0, bufferSize)
		preallocated = append(preallocated, &buf)
	}

	return &AudioBufferPool{
		bufferSize:   bufferSize,
		maxPoolSize:  GetConfig().MaxPoolSize,
		preallocated: preallocated,
		preallocSize: preallocSize,
		pool: sync.Pool{
			New: func() interface{} {
				// Allocate exact size to minimize memory waste
				buf := make([]byte, 0, bufferSize)
				return &buf
			},
		},
	}
}

func (p *AudioBufferPool) Get() []byte {
	start := time.Now()
	wasHit := false
	defer func() {
		latency := time.Since(start)
		// Record metrics for frame pool (assuming this is the main usage)
		if p.bufferSize >= GetConfig().AudioFramePoolSize {
			GetGranularMetricsCollector().RecordFramePoolGet(latency, wasHit)
		} else {
			GetGranularMetricsCollector().RecordControlPoolGet(latency, wasHit)
		}
	}()

	// First try to get from pre-allocated pool for fastest access
	p.mutex.Lock()
	if len(p.preallocated) > 0 {
		lastIdx := len(p.preallocated) - 1
		buf := p.preallocated[lastIdx]
		p.preallocated = p.preallocated[:lastIdx]
		p.mutex.Unlock()

		// Update hit counter
		atomic.AddInt64(&p.hitCount, 1)
		wasHit = true
		// Ensure buffer is properly reset
		*buf = (*buf)[:0]
		return *buf
	}
	p.mutex.Unlock()

	// Try sync.Pool next
	if poolBuf := p.pool.Get(); poolBuf != nil {
		buf := poolBuf.(*[]byte)
		// Update hit counter
		atomic.AddInt64(&p.hitCount, 1)
		// Ensure buffer is properly reset and check capacity
		if cap(*buf) >= p.bufferSize {
			wasHit = true
			*buf = (*buf)[:0]
			return *buf
		} else {
			// Buffer too small, allocate new one
			atomic.AddInt64(&p.missCount, 1)
			return make([]byte, 0, p.bufferSize)
		}
	}

	// Pool miss - allocate new buffer with exact capacity
	atomic.AddInt64(&p.missCount, 1)
	return make([]byte, 0, p.bufferSize)
}

func (p *AudioBufferPool) Put(buf []byte) {
	start := time.Now()
	defer func() {
		latency := time.Since(start)
		// Record metrics for frame pool (assuming this is the main usage)
		if p.bufferSize >= GetConfig().AudioFramePoolSize {
			GetGranularMetricsCollector().RecordFramePoolPut(latency, cap(buf))
		} else {
			GetGranularMetricsCollector().RecordControlPoolPut(latency, cap(buf))
		}
	}()

	// Validate buffer capacity - reject buffers that are too small or too large
	bufCap := cap(buf)
	if bufCap < p.bufferSize || bufCap > p.bufferSize*2 {
		return // Buffer size mismatch, don't pool it to prevent memory bloat
	}

	// Reset buffer for reuse - clear any sensitive data
	resetBuf := buf[:0]

	// First try to return to pre-allocated pool for fastest reuse
	p.mutex.Lock()
	if len(p.preallocated) < p.preallocSize {
		p.preallocated = append(p.preallocated, &resetBuf)
		p.mutex.Unlock()
		return
	}
	p.mutex.Unlock()

	// Check sync.Pool size limit to prevent excessive memory usage
	currentSize := atomic.LoadInt64(&p.currentSize)
	if currentSize >= int64(p.maxPoolSize) {
		return // Pool is full, let GC handle this buffer
	}

	// Return to sync.Pool
	p.pool.Put(&resetBuf)

	// Update pool size counter atomically
	atomic.AddInt64(&p.currentSize, 1)
}

var (
	audioFramePool   = NewAudioBufferPool(GetConfig().AudioFramePoolSize)
	audioControlPool = NewAudioBufferPool(GetConfig().OutputHeaderSize)
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

// GetPoolStats returns detailed statistics about this buffer pool
func (p *AudioBufferPool) GetPoolStats() AudioBufferPoolDetailedStats {
	p.mutex.RLock()
	preallocatedCount := len(p.preallocated)
	currentSize := p.currentSize
	p.mutex.RUnlock()

	hitCount := atomic.LoadInt64(&p.hitCount)
	missCount := atomic.LoadInt64(&p.missCount)
	totalRequests := hitCount + missCount

	var hitRate float64
	if totalRequests > 0 {
		hitRate = float64(hitCount) / float64(totalRequests) * GetConfig().PercentageMultiplier
	}

	return AudioBufferPoolDetailedStats{
		BufferSize:        p.bufferSize,
		MaxPoolSize:       p.maxPoolSize,
		CurrentPoolSize:   currentSize,
		PreallocatedCount: int64(preallocatedCount),
		PreallocatedMax:   int64(p.preallocSize),
		HitCount:          hitCount,
		MissCount:         missCount,
		HitRate:           hitRate,
	}
}

// AudioBufferPoolDetailedStats provides detailed pool statistics
type AudioBufferPoolDetailedStats struct {
	BufferSize        int
	MaxPoolSize       int
	CurrentPoolSize   int64
	PreallocatedCount int64
	PreallocatedMax   int64
	HitCount          int64
	MissCount         int64
	HitRate           float64 // Percentage
}

// GetAudioBufferPoolStats returns statistics about the audio buffer pools
type AudioBufferPoolStats struct {
	FramePoolSize   int64
	FramePoolMax    int
	ControlPoolSize int64
	ControlPoolMax  int
	// Enhanced statistics
	FramePoolHitRate   float64
	ControlPoolHitRate float64
	FramePoolDetails   AudioBufferPoolDetailedStats
	ControlPoolDetails AudioBufferPoolDetailedStats
}

func GetAudioBufferPoolStats() AudioBufferPoolStats {
	audioFramePool.mutex.RLock()
	frameSize := audioFramePool.currentSize
	frameMax := audioFramePool.maxPoolSize
	audioFramePool.mutex.RUnlock()

	audioControlPool.mutex.RLock()
	controlSize := audioControlPool.currentSize
	controlMax := audioControlPool.maxPoolSize
	audioControlPool.mutex.RUnlock()

	// Get detailed statistics
	frameDetails := audioFramePool.GetPoolStats()
	controlDetails := audioControlPool.GetPoolStats()

	return AudioBufferPoolStats{
		FramePoolSize:      frameSize,
		FramePoolMax:       frameMax,
		ControlPoolSize:    controlSize,
		ControlPoolMax:     controlMax,
		FramePoolHitRate:   frameDetails.HitRate,
		ControlPoolHitRate: controlDetails.HitRate,
		FramePoolDetails:   frameDetails,
		ControlPoolDetails: controlDetails,
	}
}
