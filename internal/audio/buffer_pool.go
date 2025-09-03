//go:build cgo
// +build cgo

package audio

import (
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"
)

// AudioLatencyInfo holds simplified latency information for cleanup decisions
type AudioLatencyInfo struct {
	LatencyMs float64
	Timestamp time.Time
}

// Global latency tracking
var (
	currentAudioLatency        = AudioLatencyInfo{}
	currentAudioLatencyLock    sync.RWMutex
	audioMonitoringInitialized int32 // Atomic flag to track initialization
)

// InitializeAudioMonitoring starts the background goroutines for latency tracking and cache cleanup
// This is safe to call multiple times as it will only initialize once
func InitializeAudioMonitoring() {
	// Use atomic CAS to ensure we only initialize once
	if atomic.CompareAndSwapInt32(&audioMonitoringInitialized, 0, 1) {
		// Start the latency recorder
		startLatencyRecorder()

		// Start the cleanup goroutine
		startCleanupGoroutine()
	}
}

// latencyChannel is used for non-blocking latency recording
var latencyChannel = make(chan float64, 10)

// startLatencyRecorder starts the latency recorder goroutine
// This should be called during package initialization
func startLatencyRecorder() {
	go latencyRecorderLoop()
}

// latencyRecorderLoop processes latency recordings in the background
func latencyRecorderLoop() {
	for latencyMs := range latencyChannel {
		currentAudioLatencyLock.Lock()
		currentAudioLatency = AudioLatencyInfo{
			LatencyMs: latencyMs,
			Timestamp: time.Now(),
		}
		currentAudioLatencyLock.Unlock()
	}
}

// RecordAudioLatency records the current audio processing latency
// This is called from the audio input manager when latency is measured
// It is non-blocking to ensure zero overhead in the critical audio path
func RecordAudioLatency(latencyMs float64) {
	// Non-blocking send - if channel is full, we drop the update
	select {
	case latencyChannel <- latencyMs:
		// Successfully sent
	default:
		// Channel full, drop this update to avoid blocking the audio path
	}
}

// GetAudioLatencyMetrics returns the current audio latency information
// Returns nil if no latency data is available or if it's too old
func GetAudioLatencyMetrics() *AudioLatencyInfo {
	currentAudioLatencyLock.RLock()
	defer currentAudioLatencyLock.RUnlock()

	// Check if we have valid latency data
	if currentAudioLatency.Timestamp.IsZero() {
		return nil
	}

	// Check if the data is too old (more than 5 seconds)
	if time.Since(currentAudioLatency.Timestamp) > 5*time.Second {
		return nil
	}

	return &AudioLatencyInfo{
		LatencyMs: currentAudioLatency.LatencyMs,
		Timestamp: currentAudioLatency.Timestamp,
	}
}

// Lock-free buffer cache for per-goroutine optimization
type lockFreeBufferCache struct {
	buffers [4]*[]byte // Small fixed-size array for lock-free access
}

// TTL tracking for goroutine cache entries
type cacheEntry struct {
	cache      *lockFreeBufferCache
	lastAccess int64 // Unix timestamp of last access
	gid        int64 // Goroutine ID for better tracking
}

// Per-goroutine buffer cache using goroutine-local storage
var goroutineBufferCache = make(map[int64]*lockFreeBufferCache)
var goroutineCacheMutex sync.RWMutex
var lastCleanupTime int64        // Unix timestamp of last cleanup
const maxCacheSize = 500         // Maximum number of goroutine caches (reduced from 1000)
const cleanupInterval int64 = 30 // Cleanup interval in seconds (30 seconds, reduced from 60)
const bufferTTL int64 = 60       // Time-to-live for cached buffers in seconds (1 minute, reduced from 2)

// getGoroutineID extracts goroutine ID from runtime stack for cache key
func getGoroutineID() int64 {
	b := make([]byte, 64)
	b = b[:runtime.Stack(b, false)]
	// Parse "goroutine 123 [running]:" format
	for i := 10; i < len(b); i++ {
		if b[i] == ' ' {
			id := int64(0)
			for j := 10; j < i; j++ {
				if b[j] >= '0' && b[j] <= '9' {
					id = id*10 + int64(b[j]-'0')
				}
			}
			return id
		}
	}
	return 0
}

// Map of goroutine ID to cache entry with TTL tracking
var goroutineCacheWithTTL = make(map[int64]*cacheEntry)

// cleanupChannel is used for asynchronous cleanup requests
var cleanupChannel = make(chan struct{}, 1)

// startCleanupGoroutine starts the cleanup goroutine
// This should be called during package initialization
func startCleanupGoroutine() {
	go cleanupLoop()
}

// cleanupLoop processes cleanup requests in the background
func cleanupLoop() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-cleanupChannel:
			// Received explicit cleanup request
			performCleanup(true)
		case <-ticker.C:
			// Regular cleanup check
			performCleanup(false)
		}
	}
}

// requestCleanup signals the cleanup goroutine to perform a cleanup
// This is non-blocking and can be called from the critical path
func requestCleanup() {
	select {
	case cleanupChannel <- struct{}{}:
		// Successfully requested cleanup
	default:
		// Channel full, cleanup already pending
	}
}

// performCleanup does the actual cache cleanup work
// This runs in a dedicated goroutine, not in the critical path
func performCleanup(forced bool) {
	now := time.Now().Unix()
	lastCleanup := atomic.LoadInt64(&lastCleanupTime)

	// Check if we're in a high-latency situation
	isHighLatency := false
	latencyMetrics := GetAudioLatencyMetrics()
	if latencyMetrics != nil && latencyMetrics.LatencyMs > 10.0 {
		// Under high latency, be more aggressive with cleanup
		isHighLatency = true
	}

	// Only cleanup if enough time has passed (less time if high latency) or if forced
	interval := cleanupInterval
	if isHighLatency {
		interval = cleanupInterval / 2 // More frequent cleanup under high latency
	}

	if !forced && now-lastCleanup < interval {
		return
	}

	// Try to acquire cleanup lock atomically
	if !atomic.CompareAndSwapInt64(&lastCleanupTime, lastCleanup, now) {
		return // Another goroutine is already cleaning up
	}

	// Perform the actual cleanup
	doCleanupGoroutineCache()
}

// cleanupGoroutineCache triggers an asynchronous cleanup of the goroutine cache
// This is safe to call from the critical path as it's non-blocking
func cleanupGoroutineCache() {
	// Request asynchronous cleanup
	requestCleanup()
}

// The actual cleanup implementation that runs in the background goroutine
func doCleanupGoroutineCache() {
	// Get current time for TTL calculations
	now := time.Now().Unix()

	// Check if we're in a high-latency situation
	isHighLatency := false
	latencyMetrics := GetAudioLatencyMetrics()
	if latencyMetrics != nil && latencyMetrics.LatencyMs > 10.0 {
		// Under high latency, be more aggressive with cleanup
		isHighLatency = true
	}

	goroutineCacheMutex.Lock()
	defer goroutineCacheMutex.Unlock()

	// Convert old cache format to new TTL-based format if needed
	if len(goroutineCacheWithTTL) == 0 && len(goroutineBufferCache) > 0 {
		for gid, cache := range goroutineBufferCache {
			goroutineCacheWithTTL[gid] = &cacheEntry{
				cache:      cache,
				lastAccess: now,
			}
		}
		// Clear old cache to free memory
		goroutineBufferCache = make(map[int64]*lockFreeBufferCache)
	}

	// Remove stale entries based on TTL (more aggressive under high latency)
	expiredCount := 0
	ttl := bufferTTL
	if isHighLatency {
		// Under high latency, use a much shorter TTL
		ttl = bufferTTL / 4
	}

	for gid, entry := range goroutineCacheWithTTL {
		// Both now and entry.lastAccess are int64, so this comparison is safe
		if now-entry.lastAccess > ttl {
			delete(goroutineCacheWithTTL, gid)
			expiredCount++
		}
	}

	// If cache is still too large after TTL cleanup, remove oldest entries
	// Under high latency, use a more aggressive target size
	targetSize := maxCacheSize
	targetReduction := maxCacheSize / 2

	if isHighLatency {
		// Under high latency, target a much smaller cache size
		targetSize = maxCacheSize / 4
		targetReduction = maxCacheSize / 8
	}

	if len(goroutineCacheWithTTL) > targetSize {
		// Find oldest entries
		type ageEntry struct {
			gid        int64
			lastAccess int64
		}
		oldestEntries := make([]ageEntry, 0, len(goroutineCacheWithTTL))
		for gid, entry := range goroutineCacheWithTTL {
			oldestEntries = append(oldestEntries, ageEntry{gid, entry.lastAccess})
		}

		// Sort by lastAccess (oldest first)
		sort.Slice(oldestEntries, func(i, j int) bool {
			return oldestEntries[i].lastAccess < oldestEntries[j].lastAccess
		})

		// Remove oldest entries to get down to target reduction size
		toRemove := len(goroutineCacheWithTTL) - targetReduction
		for i := 0; i < toRemove && i < len(oldestEntries); i++ {
			delete(goroutineCacheWithTTL, oldestEntries[i].gid)
		}
	}
}

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
		// Use default value on validation error
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
	// Skip cleanup trigger in hotpath - cleanup runs in background
	// cleanupGoroutineCache() - moved to background goroutine

	// Fast path: Try lock-free per-goroutine cache first
	gid := getGoroutineID()
	goroutineCacheMutex.RLock()
	cacheEntry, exists := goroutineCacheWithTTL[gid]
	goroutineCacheMutex.RUnlock()

	if exists && cacheEntry != nil && cacheEntry.cache != nil {
		// Try to get buffer from lock-free cache
		cache := cacheEntry.cache
		for i := 0; i < len(cache.buffers); i++ {
			bufPtr := (*unsafe.Pointer)(unsafe.Pointer(&cache.buffers[i]))
			buf := (*[]byte)(atomic.LoadPointer(bufPtr))
			if buf != nil && atomic.CompareAndSwapPointer(bufPtr, unsafe.Pointer(buf), nil) {
				atomic.AddInt64(&p.hitCount, 1)
				*buf = (*buf)[:0]
				return *buf
			}
		}
		// Update access time only after cache miss to reduce overhead
		cacheEntry.lastAccess = time.Now().Unix()
	}

	// Fallback: Try pre-allocated pool with mutex
	p.mutex.Lock()
	if len(p.preallocated) > 0 {
		lastIdx := len(p.preallocated) - 1
		buf := p.preallocated[lastIdx]
		p.preallocated = p.preallocated[:lastIdx]
		p.mutex.Unlock()
		atomic.AddInt64(&p.hitCount, 1)
		*buf = (*buf)[:0]
		return *buf
	}
	p.mutex.Unlock()

	// Try sync.Pool next
	if poolBuf := p.pool.Get(); poolBuf != nil {
		buf := poolBuf.(*[]byte)
		atomic.AddInt64(&p.hitCount, 1)
		atomic.AddInt64(&p.currentSize, -1)
		// Fast capacity check - most buffers should be correct size
		if cap(*buf) >= p.bufferSize {
			*buf = (*buf)[:0]
			return *buf
		}
		// Buffer too small, fall through to allocation
	}

	// Pool miss - allocate new buffer with exact capacity
	atomic.AddInt64(&p.missCount, 1)
	return make([]byte, 0, p.bufferSize)
}

func (p *AudioBufferPool) Put(buf []byte) {
	// Fast validation - reject buffers that are too small or too large
	bufCap := cap(buf)
	if bufCap < p.bufferSize || bufCap > p.bufferSize*2 {
		return // Buffer size mismatch, don't pool it to prevent memory bloat
	}

	// Reset buffer for reuse - clear any sensitive data
	resetBuf := buf[:0]

	// Fast path: Try to put in lock-free per-goroutine cache
	gid := getGoroutineID()
	goroutineCacheMutex.RLock()
	entryWithTTL, exists := goroutineCacheWithTTL[gid]
	goroutineCacheMutex.RUnlock()

	var cache *lockFreeBufferCache
	if exists && entryWithTTL != nil {
		cache = entryWithTTL.cache
		// Update access time only when we successfully use the cache
	} else {
		// Create new cache for this goroutine
		cache = &lockFreeBufferCache{}
		now := time.Now().Unix()
		goroutineCacheMutex.Lock()
		goroutineCacheWithTTL[gid] = &cacheEntry{
			cache:      cache,
			lastAccess: now,
			gid:        gid,
		}
		goroutineCacheMutex.Unlock()
	}

	if cache != nil {
		// Try to store in lock-free cache
		for i := 0; i < len(cache.buffers); i++ {
			bufPtr := (*unsafe.Pointer)(unsafe.Pointer(&cache.buffers[i]))
			if atomic.CompareAndSwapPointer(bufPtr, nil, unsafe.Pointer(&buf)) {
				// Update access time only on successful cache
				if exists && entryWithTTL != nil {
					entryWithTTL.lastAccess = time.Now().Unix()
				}
				return // Successfully cached
			}
		}
	}

	// Fallback: Try to return to pre-allocated pool for fastest reuse
	p.mutex.Lock()
	if len(p.preallocated) < p.preallocSize {
		p.preallocated = append(p.preallocated, &resetBuf)
		p.mutex.Unlock()
		return
	}
	p.mutex.Unlock()

	// Check sync.Pool size limit to prevent excessive memory usage
	if atomic.LoadInt64(&p.currentSize) >= int64(p.maxPoolSize) {
		return // Pool is full, let GC handle this buffer
	}

	// Return to sync.Pool and update counter atomically
	p.pool.Put(&resetBuf)
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
	TotalBytes        int64   // Total memory usage in bytes
	AverageBufferSize float64 // Average size of buffers in the pool
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
