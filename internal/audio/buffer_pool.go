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
		logger.Error().Err(err).Int("bufferSize", bufferSize).Msg("Invalid buffer size provided, using default")
		bufferSize = GetConfig().AudioFramePoolSize
	}

	// Pre-allocate 20% of max pool size for immediate availability
	preallocSize := GetConfig().PreallocPercentage
	preallocated := make([]*[]byte, 0, preallocSize)

	// Pre-allocate buffers to reduce initial allocation overhead
	for i := 0; i < preallocSize; i++ {
		buf := make([]byte, 0, bufferSize)
		preallocated = append(preallocated, &buf)
	}

	return &AudioBufferPool{
		bufferSize:   bufferSize,
		maxPoolSize:  GetConfig().MaxPoolSize, // Limit pool size to prevent excessive memory usage
		preallocated: preallocated,
		preallocSize: preallocSize,
		pool: sync.Pool{
			New: func() interface{} {
				buf := make([]byte, 0, bufferSize)
				return &buf
			},
		},
	}
}

func (p *AudioBufferPool) Get() []byte {
	start := time.Now()
	defer func() {
		latency := time.Since(start)
		// Record metrics for frame pool (assuming this is the main usage)
		if p.bufferSize >= GetConfig().AudioFramePoolSize {
			GetGranularMetricsCollector().RecordFramePoolGet(latency, atomic.LoadInt64(&p.hitCount) > 0)
		} else {
			GetGranularMetricsCollector().RecordControlPoolGet(latency, atomic.LoadInt64(&p.hitCount) > 0)
		}
	}()

	// First try pre-allocated buffers for fastest access
	p.mutex.Lock()
	if len(p.preallocated) > 0 {
		buf := p.preallocated[len(p.preallocated)-1]
		p.preallocated = p.preallocated[:len(p.preallocated)-1]
		p.mutex.Unlock()
		atomic.AddInt64(&p.hitCount, 1)
		return (*buf)[:0] // Reset length but keep capacity
	}
	p.mutex.Unlock()

	// Try sync.Pool next
	if buf := p.pool.Get(); buf != nil {
		bufPtr := buf.(*[]byte)
		// Update pool size counter when retrieving from pool
		p.mutex.Lock()
		if p.currentSize > 0 {
			p.currentSize--
		}
		p.mutex.Unlock()
		atomic.AddInt64(&p.hitCount, 1)
		return (*bufPtr)[:0] // Reset length but keep capacity
	}

	// Last resort: allocate new buffer
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

	if cap(buf) < p.bufferSize {
		return // Buffer too small, don't pool it
	}

	// Reset buffer for reuse
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
	p.mutex.RLock()
	currentSize := p.currentSize
	p.mutex.RUnlock()

	if currentSize >= int64(p.maxPoolSize) {
		return // Pool is full, let GC handle this buffer
	}

	// Return to sync.Pool
	p.pool.Put(&resetBuf)

	// Update pool size counter
	p.mutex.Lock()
	p.currentSize++
	p.mutex.Unlock()
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
