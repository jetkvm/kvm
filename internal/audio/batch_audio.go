//go:build cgo

package audio

import (
	"context"
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
	initialized  int32
	running      int32
	threadPinned int32

	// Buffers (pre-allocated to avoid allocation overhead)
	readBufPool *sync.Pool
}

type BatchAudioStats struct {
	// int64 fields MUST be first for ARM32 alignment
	BatchedReads    int64
	SingleReads     int64
	BatchedFrames   int64
	SingleFrames    int64
	CGOCallsReduced int64
	OSThreadPinTime time.Duration // time.Duration is int64 internally
	LastBatchTime   time.Time
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

// NewBatchAudioProcessor creates a new batch audio processor
func NewBatchAudioProcessor(batchSize int, batchDuration time.Duration) *BatchAudioProcessor {
	ctx, cancel := context.WithCancel(context.Background())
	logger := logging.GetDefaultLogger().With().Str("component", "batch-audio").Logger()

	processor := &BatchAudioProcessor{
		ctx:           ctx,
		cancel:        cancel,
		logger:        &logger,
		batchSize:     batchSize,
		batchDuration: batchDuration,
		readQueue:     make(chan batchReadRequest, batchSize*2),
		readBufPool: &sync.Pool{
			New: func() interface{} {
				return make([]byte, 1500) // Max audio frame size
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
	time.Sleep(bap.batchDuration + 10*time.Millisecond)

	bap.logger.Info().Msg("batch audio processor stopped")
}

// BatchReadEncode performs batched audio read and encode operations
func (bap *BatchAudioProcessor) BatchReadEncode(buffer []byte) (int, error) {
	if atomic.LoadInt32(&bap.running) == 0 {
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

	select {
	case bap.readQueue <- request:
		// Successfully queued
	case <-time.After(5 * time.Millisecond):
		// Queue is full or blocked, fallback to single operation
		atomic.AddInt64(&bap.stats.SingleReads, 1)
		atomic.AddInt64(&bap.stats.SingleFrames, 1)
		return CGOAudioReadEncode(buffer)
	}

	// Wait for result
	select {
	case result := <-resultChan:
		return result.length, result.err
	case <-time.After(50 * time.Millisecond):
		// Timeout, fallback to single operation
		atomic.AddInt64(&bap.stats.SingleReads, 1)
		atomic.AddInt64(&bap.stats.SingleFrames, 1)
		return CGOAudioReadEncode(buffer)
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

// processBatchRead processes a batch of read requests efficiently
func (bap *BatchAudioProcessor) processBatchRead(batch []batchReadRequest) {
	if len(batch) == 0 {
		return
	}

	// Pin to OS thread for the entire batch to minimize thread switching overhead
	start := time.Now()
	if atomic.CompareAndSwapInt32(&bap.threadPinned, 0, 1) {
		runtime.LockOSThread()
		defer func() {
			runtime.UnlockOSThread()
			atomic.StoreInt32(&bap.threadPinned, 0)
			bap.stats.OSThreadPinTime += time.Since(start)
		}()
	}

	batchSize := len(batch)
	atomic.AddInt64(&bap.stats.BatchedReads, 1)
	atomic.AddInt64(&bap.stats.BatchedFrames, int64(batchSize))
	if batchSize > 1 {
		atomic.AddInt64(&bap.stats.CGOCallsReduced, int64(batchSize-1))
	}

	// Process each request in the batch
	for _, req := range batch {
		length, err := CGOAudioReadEncode(req.buffer)
		result := batchReadResult{
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

	bap.stats.LastBatchTime = time.Now()
}

// GetStats returns current batch processor statistics
func (bap *BatchAudioProcessor) GetStats() BatchAudioStats {
	return BatchAudioStats{
		BatchedReads:    atomic.LoadInt64(&bap.stats.BatchedReads),
		SingleReads:     atomic.LoadInt64(&bap.stats.SingleReads),
		BatchedFrames:   atomic.LoadInt64(&bap.stats.BatchedFrames),
		SingleFrames:    atomic.LoadInt64(&bap.stats.SingleFrames),
		CGOCallsReduced: atomic.LoadInt64(&bap.stats.CGOCallsReduced),
		OSThreadPinTime: bap.stats.OSThreadPinTime,
		LastBatchTime:   bap.stats.LastBatchTime,
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
		processor := NewBatchAudioProcessor(4, 5*time.Millisecond) // 4 frames per batch, 5ms timeout
		atomic.StorePointer(&globalBatchProcessor, unsafe.Pointer(processor))
		return processor
	}

	// Another goroutine initialized it, try again
	ptr = atomic.LoadPointer(&globalBatchProcessor)
	if ptr != nil {
		return (*BatchAudioProcessor)(ptr)
	}

	// Fallback: create a new processor (should rarely happen)
	return NewBatchAudioProcessor(4, 5*time.Millisecond)
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
	if processor != nil && processor.IsRunning() {
		return processor.BatchReadEncode(buffer)
	}
	return CGOAudioReadEncode(buffer)
}
