//go:build cgo

package audio

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/jetkvm/kvm/internal/logging"
	"github.com/rs/zerolog"
)

// BatchAudioProcessor manages batched CGO operations to reduce syscall overhead
type BatchAudioProcessor struct {
	// Statistics - MUST be first for ARM32 alignment (int64 fields need 8-byte alignment)
	stats BatchAudioStats

	// Control
	ctx           context.Context
	cancel        context.CancelFunc
	logger        *zerolog.Logger
	batchSize     int
	batchDuration time.Duration

	// Batch queues and state (atomic for lock-free access)
	readQueue    chan batchReadRequest
	writeQueue   chan batchWriteRequest
	initialized  int32
	running      int32
	threadPinned int32
	writePinned  int32

	// Buffers (pre-allocated to avoid allocation overhead)
	readBufPool  *sync.Pool
	writeBufPool *sync.Pool
}

type BatchAudioStats struct {
	// int64 fields MUST be first for ARM32 alignment
	BatchedReads    int64
	SingleReads     int64
	BatchedWrites   int64
	SingleWrites    int64
	BatchedFrames   int64
	SingleFrames    int64
	WriteFrames     int64
	CGOCallsReduced int64
	OSThreadPinTime time.Duration // time.Duration is int64 internally
	WriteThreadTime time.Duration // time.Duration is int64 internally
	LastBatchTime   time.Time
	LastWriteTime   time.Time
}

type batchReadRequest struct {
	buffer     []byte
	resultChan chan batchReadResult
	timestamp  time.Time
}

type batchReadResult struct {
	length int
	err    error
}

type batchWriteRequest struct {
	buffer     []byte // Buffer for backward compatibility
	opusData   []byte // Opus encoded data for decode-write operations
	pcmBuffer  []byte // PCM buffer for decode-write operations
	resultChan chan batchWriteResult
	timestamp  time.Time
}

type batchWriteResult struct {
	length int
	err    error
}

// NewBatchAudioProcessor creates a new batch audio processor
func NewBatchAudioProcessor(batchSize int, batchDuration time.Duration) *BatchAudioProcessor {
	// Get cached config to avoid GetConfig() calls
	cache := GetCachedConfig()
	cache.Update()

	// Validate input parameters with minimal overhead
	if batchSize <= 0 || batchSize > 1000 {
		batchSize = cache.BatchProcessorFramesPerBatch
	}
	if batchDuration <= 0 {
		batchDuration = cache.BatchProcessingDelay
	}

	ctx, cancel := context.WithCancel(context.Background())
	// Pre-allocate logger to avoid repeated allocations
	logger := logging.GetDefaultLogger().With().Str("component", "batch-audio").Logger()

	// Pre-calculate frame size to avoid repeated GetConfig() calls
	frameSize := cache.GetMinReadEncodeBuffer()
	if frameSize == 0 {
		frameSize = 1500 // Safe fallback
	}

	processor := &BatchAudioProcessor{
		ctx:           ctx,
		cancel:        cancel,
		logger:        &logger,
		batchSize:     batchSize,
		batchDuration: batchDuration,
		readQueue:     make(chan batchReadRequest, batchSize*2),
		writeQueue:    make(chan batchWriteRequest, batchSize*2),
		readBufPool: &sync.Pool{
			New: func() interface{} {
				// Use pre-calculated frame size to avoid GetConfig() calls
				return make([]byte, 0, frameSize)
			},
		},
		writeBufPool: &sync.Pool{
			New: func() interface{} {
				// Use pre-calculated frame size to avoid GetConfig() calls
				return make([]byte, 0, frameSize)
			},
		},
	}

	return processor
}

// Start initializes and starts the batch processor
func (bap *BatchAudioProcessor) Start() error {
	if !atomic.CompareAndSwapInt32(&bap.running, 0, 1) {
		return nil // Already running
	}

	// Initialize CGO resources once per processor lifecycle
	if !atomic.CompareAndSwapInt32(&bap.initialized, 0, 1) {
		return nil // Already initialized
	}

	// Start batch processing goroutines
	go bap.batchReadProcessor()
	go bap.batchWriteProcessor()

	bap.logger.Info().Int("batch_size", bap.batchSize).
		Dur("batch_duration", bap.batchDuration).
		Msg("batch audio processor started")

	return nil
}

// Stop cleanly shuts down the batch processor
func (bap *BatchAudioProcessor) Stop() {
	if !atomic.CompareAndSwapInt32(&bap.running, 1, 0) {
		return // Already stopped
	}

	bap.cancel()

	// Wait for processing to complete
	time.Sleep(bap.batchDuration + GetConfig().BatchProcessingDelay)

	bap.logger.Info().Msg("batch audio processor stopped")
}

// BatchReadEncode performs batched audio read and encode operations
func (bap *BatchAudioProcessor) BatchReadEncode(buffer []byte) (int, error) {
	// Get cached config to avoid GetConfig() calls in hot path
	cache := GetCachedConfig()
	cache.Update()

	// Validate buffer before processing
	if err := ValidateBufferSize(len(buffer)); err != nil {
		bap.logger.Debug().Err(err).Msg("invalid buffer for batch processing")
		return 0, err
	}

	if !bap.IsRunning() {
		// Fallback to single operation if batch processor is not running
		atomic.AddInt64(&bap.stats.SingleReads, 1)
		atomic.AddInt64(&bap.stats.SingleFrames, 1)
		return CGOAudioReadEncode(buffer)
	}

	resultChan := make(chan batchReadResult, 1)
	request := batchReadRequest{
		buffer:     buffer,
		resultChan: resultChan,
		timestamp:  time.Now(),
	}

	// Try to queue the request with non-blocking send
	select {
	case bap.readQueue <- request:
		// Successfully queued
	default:
		// Queue is full, fallback to single operation
		atomic.AddInt64(&bap.stats.SingleReads, 1)
		atomic.AddInt64(&bap.stats.SingleFrames, 1)
		return CGOAudioReadEncode(buffer)
	}

	// Wait for result with timeout
	select {
	case result := <-resultChan:
		return result.length, result.err
	case <-time.After(cache.BatchProcessingTimeout):
		// Timeout, fallback to single operation
		atomic.AddInt64(&bap.stats.SingleReads, 1)
		atomic.AddInt64(&bap.stats.SingleFrames, 1)
		return CGOAudioReadEncode(buffer)
	}
}

// BatchDecodeWrite performs batched audio decode and write operations
// This is the legacy version that uses a single buffer
func (bap *BatchAudioProcessor) BatchDecodeWrite(buffer []byte) (int, error) {
	// Get cached config to avoid GetConfig() calls in hot path
	cache := GetCachedConfig()
	cache.Update()

	// Validate buffer before processing
	if err := ValidateBufferSize(len(buffer)); err != nil {
		bap.logger.Debug().Err(err).Msg("invalid buffer for batch processing")
		return 0, err
	}

	if !bap.IsRunning() {
		// Fallback to single operation if batch processor is not running
		atomic.AddInt64(&bap.stats.SingleWrites, 1)
		atomic.AddInt64(&bap.stats.WriteFrames, 1)
		return CGOAudioDecodeWriteLegacy(buffer)
	}

	resultChan := make(chan batchWriteResult, 1)
	request := batchWriteRequest{
		buffer:     buffer,
		resultChan: resultChan,
		timestamp:  time.Now(),
	}

	// Try to queue the request with non-blocking send
	select {
	case bap.writeQueue <- request:
		// Successfully queued
	default:
		// Queue is full, fall back to single operation
		atomic.AddInt64(&bap.stats.SingleWrites, 1)
		atomic.AddInt64(&bap.stats.WriteFrames, 1)
		return CGOAudioDecodeWriteLegacy(buffer)
	}

	// Wait for result with timeout
	select {
	case result := <-resultChan:
		return result.length, result.err
	case <-time.After(cache.BatchProcessingTimeout):
		atomic.AddInt64(&bap.stats.SingleWrites, 1)
		atomic.AddInt64(&bap.stats.WriteFrames, 1)
		return CGOAudioDecodeWriteLegacy(buffer)
	}
}

// BatchDecodeWriteWithBuffers performs batched audio decode and write operations with separate opus and PCM buffers
func (bap *BatchAudioProcessor) BatchDecodeWriteWithBuffers(opusData []byte, pcmBuffer []byte) (int, error) {
	// Get cached config to avoid GetConfig() calls in hot path
	cache := GetCachedConfig()
	cache.Update()

	// Validate buffers before processing
	if len(opusData) == 0 {
		return 0, fmt.Errorf("empty opus data buffer")
	}
	if len(pcmBuffer) == 0 {
		return 0, fmt.Errorf("empty PCM buffer")
	}

	if !bap.IsRunning() {
		// Fallback to single operation if batch processor is not running
		atomic.AddInt64(&bap.stats.SingleWrites, 1)
		atomic.AddInt64(&bap.stats.WriteFrames, 1)
		// Use the optimized function with separate buffers
		return CGOAudioDecodeWrite(opusData, pcmBuffer)
	}

	resultChan := make(chan batchWriteResult, 1)
	request := batchWriteRequest{
		opusData:   opusData,
		pcmBuffer:  pcmBuffer,
		resultChan: resultChan,
		timestamp:  time.Now(),
	}

	// Try to queue the request with non-blocking send
	select {
	case bap.writeQueue <- request:
		// Successfully queued
	default:
		// Queue is full, fall back to single operation
		atomic.AddInt64(&bap.stats.SingleWrites, 1)
		atomic.AddInt64(&bap.stats.WriteFrames, 1)
		// Use the optimized function with separate buffers
		return CGOAudioDecodeWrite(opusData, pcmBuffer)
	}

	// Wait for result with timeout
	select {
	case result := <-resultChan:
		return result.length, result.err
	case <-time.After(cache.BatchProcessingTimeout):
		atomic.AddInt64(&bap.stats.SingleWrites, 1)
		atomic.AddInt64(&bap.stats.WriteFrames, 1)
		// Use the optimized function with separate buffers
		return CGOAudioDecodeWrite(opusData, pcmBuffer)
	}
}

// batchReadProcessor processes batched read operations
func (bap *BatchAudioProcessor) batchReadProcessor() {
	defer bap.logger.Debug().Msg("batch read processor stopped")

	ticker := time.NewTicker(bap.batchDuration)
	defer ticker.Stop()

	var batch []batchReadRequest
	batch = make([]batchReadRequest, 0, bap.batchSize)

	for atomic.LoadInt32(&bap.running) == 1 {
		select {
		case <-bap.ctx.Done():
			return

		case req := <-bap.readQueue:
			batch = append(batch, req)
			if len(batch) >= bap.batchSize {
				bap.processBatchRead(batch)
				batch = batch[:0] // Clear slice but keep capacity
			}

		case <-ticker.C:
			if len(batch) > 0 {
				bap.processBatchRead(batch)
				batch = batch[:0] // Clear slice but keep capacity
			}
		}
	}

	// Process any remaining requests
	if len(batch) > 0 {
		bap.processBatchRead(batch)
	}
}

// batchWriteProcessor processes batched write operations
func (bap *BatchAudioProcessor) batchWriteProcessor() {
	defer bap.logger.Debug().Msg("batch write processor stopped")

	ticker := time.NewTicker(bap.batchDuration)
	defer ticker.Stop()

	var batch []batchWriteRequest
	batch = make([]batchWriteRequest, 0, bap.batchSize)

	for atomic.LoadInt32(&bap.running) == 1 {
		select {
		case <-bap.ctx.Done():
			return

		case req := <-bap.writeQueue:
			batch = append(batch, req)
			if len(batch) >= bap.batchSize {
				bap.processBatchWrite(batch)
				batch = batch[:0] // Clear slice but keep capacity
			}

		case <-ticker.C:
			if len(batch) > 0 {
				bap.processBatchWrite(batch)
				batch = batch[:0] // Clear slice but keep capacity
			}
		}
	}

	// Process any remaining requests
	if len(batch) > 0 {
		bap.processBatchWrite(batch)
	}
}

// processBatchRead processes a batch of read requests efficiently
func (bap *BatchAudioProcessor) processBatchRead(batch []batchReadRequest) {
	batchSize := len(batch)
	if batchSize == 0 {
		return
	}

	// Get cached config once - avoid repeated calls
	cache := GetCachedConfig()
	minBatchSize := cache.MinBatchSizeForThreadPinning

	// Only pin to OS thread for large batches to reduce thread contention
	var start time.Time
	threadWasPinned := false
	if batchSize >= minBatchSize && atomic.CompareAndSwapInt32(&bap.threadPinned, 0, 1) {
		start = time.Now()
		threadWasPinned = true
		runtime.LockOSThread()
		// Skip priority setting for better performance - audio threads already have good priority
	}

	// Update stats efficiently
	atomic.AddInt64(&bap.stats.BatchedReads, 1)
	atomic.AddInt64(&bap.stats.BatchedFrames, int64(batchSize))
	if batchSize > 1 {
		atomic.AddInt64(&bap.stats.CGOCallsReduced, int64(batchSize-1))
	}

	// Process each request in the batch with minimal overhead
	for i := range batch {
		req := &batch[i]
		length, err := CGOAudioReadEncode(req.buffer)

		// Send result back (non-blocking) - reuse result struct
		select {
		case req.resultChan <- batchReadResult{length: length, err: err}:
		default:
			// Requestor timed out, drop result
		}
	}

	// Release thread lock if we pinned it
	if threadWasPinned {
		runtime.UnlockOSThread()
		atomic.StoreInt32(&bap.threadPinned, 0)
		bap.stats.OSThreadPinTime += time.Since(start)
	}

	bap.stats.LastBatchTime = time.Now()
}

// processBatchWrite processes a batch of write requests efficiently
func (bap *BatchAudioProcessor) processBatchWrite(batch []batchWriteRequest) {
	if len(batch) == 0 {
		return
	}

	// Get cached config to avoid GetConfig() calls in hot path
	cache := GetCachedConfig()

	// Only pin to OS thread for large batches to reduce thread contention
	start := time.Now()
	shouldPinThread := len(batch) >= cache.MinBatchSizeForThreadPinning

	// Track if we pinned the thread in this call
	threadWasPinned := false

	if shouldPinThread && atomic.CompareAndSwapInt32(&bap.writePinned, 0, 1) {
		threadWasPinned = true
		runtime.LockOSThread()

		// Set high priority for batch audio processing - skip logging in hotpath
		_ = SetAudioThreadPriority()
	}

	batchSize := len(batch)
	atomic.AddInt64(&bap.stats.BatchedWrites, 1)
	atomic.AddInt64(&bap.stats.WriteFrames, int64(batchSize))
	if batchSize > 1 {
		atomic.AddInt64(&bap.stats.CGOCallsReduced, int64(batchSize-1))
	}

	// Add deferred function to release thread lock if we pinned it
	if threadWasPinned {
		defer func() {
			// Skip logging in hotpath for performance
			_ = ResetThreadPriority()
			runtime.UnlockOSThread()
			atomic.StoreInt32(&bap.writePinned, 0)
			bap.stats.WriteThreadTime += time.Since(start)
		}()
	}

	// Process each request in the batch
	for _, req := range batch {
		var length int
		var err error

		// Handle both legacy and new decode-write operations
		if req.opusData != nil && req.pcmBuffer != nil {
			// New style with separate opus data and PCM buffer
			length, err = CGOAudioDecodeWrite(req.opusData, req.pcmBuffer)
		} else {
			// Legacy style with single buffer
			length, err = CGOAudioDecodeWriteLegacy(req.buffer)
		}

		result := batchWriteResult{
			length: length,
			err:    err,
		}

		// Send result back (non-blocking)
		select {
		case req.resultChan <- result:
		default:
			// Requestor timed out, drop result
		}
	}

	bap.stats.LastWriteTime = time.Now()
}

// GetStats returns current batch processor statistics
func (bap *BatchAudioProcessor) GetStats() BatchAudioStats {
	return BatchAudioStats{
		BatchedReads:    atomic.LoadInt64(&bap.stats.BatchedReads),
		SingleReads:     atomic.LoadInt64(&bap.stats.SingleReads),
		BatchedWrites:   atomic.LoadInt64(&bap.stats.BatchedWrites),
		SingleWrites:    atomic.LoadInt64(&bap.stats.SingleWrites),
		BatchedFrames:   atomic.LoadInt64(&bap.stats.BatchedFrames),
		SingleFrames:    atomic.LoadInt64(&bap.stats.SingleFrames),
		WriteFrames:     atomic.LoadInt64(&bap.stats.WriteFrames),
		CGOCallsReduced: atomic.LoadInt64(&bap.stats.CGOCallsReduced),
		OSThreadPinTime: bap.stats.OSThreadPinTime,
		WriteThreadTime: bap.stats.WriteThreadTime,
		LastBatchTime:   bap.stats.LastBatchTime,
		LastWriteTime:   bap.stats.LastWriteTime,
	}
}

// IsRunning returns whether the batch processor is running
func (bap *BatchAudioProcessor) IsRunning() bool {
	return atomic.LoadInt32(&bap.running) == 1
}

// Global batch processor instance
var (
	globalBatchProcessor      unsafe.Pointer // *BatchAudioProcessor
	batchProcessorInitialized int32
)

// GetBatchAudioProcessor returns the global batch processor instance
func GetBatchAudioProcessor() *BatchAudioProcessor {
	ptr := atomic.LoadPointer(&globalBatchProcessor)
	if ptr != nil {
		return (*BatchAudioProcessor)(ptr)
	}

	// Initialize on first use
	if atomic.CompareAndSwapInt32(&batchProcessorInitialized, 0, 1) {
		// Get cached config to avoid GetConfig() calls
		cache := GetCachedConfig()
		cache.Update()

		processor := NewBatchAudioProcessor(cache.BatchProcessorFramesPerBatch, cache.BatchProcessorTimeout)
		atomic.StorePointer(&globalBatchProcessor, unsafe.Pointer(processor))
		return processor
	}

	// Another goroutine initialized it, try again
	ptr = atomic.LoadPointer(&globalBatchProcessor)
	if ptr != nil {
		return (*BatchAudioProcessor)(ptr)
	}

	// Fallback: create a new processor (should rarely happen)
	config := GetConfig()
	return NewBatchAudioProcessor(config.BatchProcessorFramesPerBatch, config.BatchProcessorTimeout)
}

// EnableBatchAudioProcessing enables the global batch processor
func EnableBatchAudioProcessing() error {
	processor := GetBatchAudioProcessor()
	return processor.Start()
}

// DisableBatchAudioProcessing disables the global batch processor
func DisableBatchAudioProcessing() {
	ptr := atomic.LoadPointer(&globalBatchProcessor)
	if ptr != nil {
		processor := (*BatchAudioProcessor)(ptr)
		processor.Stop()
	}
}

// BatchCGOAudioReadEncode is a batched version of CGOAudioReadEncode
func BatchCGOAudioReadEncode(buffer []byte) (int, error) {
	processor := GetBatchAudioProcessor()
	if processor == nil || !processor.IsRunning() {
		// Fall back to non-batched version if processor is not running
		return CGOAudioReadEncode(buffer)
	}

	return processor.BatchReadEncode(buffer)
}

// BatchCGOAudioDecodeWrite is a batched version of CGOAudioDecodeWrite
func BatchCGOAudioDecodeWrite(buffer []byte) (int, error) {
	processor := GetBatchAudioProcessor()
	if processor == nil || !processor.IsRunning() {
		// Fall back to non-batched version if processor is not running
		return CGOAudioDecodeWriteLegacy(buffer)
	}

	return processor.BatchDecodeWrite(buffer)
}

// BatchCGOAudioDecodeWriteWithBuffers is a batched version of CGOAudioDecodeWrite that uses separate opus and PCM buffers
func BatchCGOAudioDecodeWriteWithBuffers(opusData []byte, pcmBuffer []byte) (int, error) {
	processor := GetBatchAudioProcessor()
	if processor == nil || !processor.IsRunning() {
		// Fall back to non-batched version if processor is not running
		return CGOAudioDecodeWrite(opusData, pcmBuffer)
	}

	return processor.BatchDecodeWriteWithBuffers(opusData, pcmBuffer)
}
