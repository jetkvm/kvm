//go:build cgo

package audio

import (
	"sync"
	"sync/atomic"
	"time"
)

// BatchZeroCopyProcessor handles batch operations on zero-copy audio frames
// with optimized reference counting and memory management
type BatchZeroCopyProcessor struct {
	// Configuration
	maxBatchSize      int
	batchTimeout      time.Duration
	processingDelay   time.Duration
	adaptiveThreshold float64

	// Processing queues
	readEncodeQueue  chan *batchZeroCopyRequest
	decodeWriteQueue chan *batchZeroCopyRequest

	// Worker management
	workerPool chan struct{}
	running    int32
	wg         sync.WaitGroup

	// Statistics
	batchedFrames    int64
	singleFrames     int64
	batchSavings     int64
	processingTimeUs int64
	adaptiveHits     int64
	adaptiveMisses   int64
}

type batchZeroCopyRequest struct {
	frames    []*ZeroCopyAudioFrame
	operation batchZeroCopyOperation
	resultCh  chan batchZeroCopyResult
	timestamp time.Time
}

type batchZeroCopyOperation int

const (
	batchOpReadEncode batchZeroCopyOperation = iota
	batchOpDecodeWrite
	batchOpMixed
)

type batchZeroCopyResult struct {
	encodedData    [][]byte // For read-encode operations
	processedCount int      // Number of successfully processed frames
	err            error
}

// Global batch zero-copy processor
var (
	globalBatchZeroCopyProcessor *BatchZeroCopyProcessor
	batchZeroCopyOnce            sync.Once
)

// GetBatchZeroCopyProcessor returns the global batch zero-copy processor
func GetBatchZeroCopyProcessor() *BatchZeroCopyProcessor {
	batchZeroCopyOnce.Do(func() {
		globalBatchZeroCopyProcessor = NewBatchZeroCopyProcessor()
		globalBatchZeroCopyProcessor.Start()
	})
	return globalBatchZeroCopyProcessor
}

// NewBatchZeroCopyProcessor creates a new batch zero-copy processor
func NewBatchZeroCopyProcessor() *BatchZeroCopyProcessor {
	cache := GetCachedConfig()
	return &BatchZeroCopyProcessor{
		maxBatchSize:      cache.BatchProcessorFramesPerBatch,
		batchTimeout:      cache.BatchProcessorTimeout,
		processingDelay:   cache.BatchProcessingDelay,
		adaptiveThreshold: cache.BatchProcessorAdaptiveThreshold,
		readEncodeQueue:   make(chan *batchZeroCopyRequest, cache.BatchProcessorMaxQueueSize),
		decodeWriteQueue:  make(chan *batchZeroCopyRequest, cache.BatchProcessorMaxQueueSize),
		workerPool:        make(chan struct{}, 4), // 4 workers for parallel processing
	}
}

// Start starts the batch zero-copy processor workers
func (bzcp *BatchZeroCopyProcessor) Start() {
	if !atomic.CompareAndSwapInt32(&bzcp.running, 0, 1) {
		return // Already running
	}

	// Start worker goroutines for read-encode operations
	for i := 0; i < cap(bzcp.workerPool)/2; i++ {
		bzcp.wg.Add(1)
		go bzcp.readEncodeWorker()
	}

	// Start worker goroutines for decode-write operations
	for i := 0; i < cap(bzcp.workerPool)/2; i++ {
		bzcp.wg.Add(1)
		go bzcp.decodeWriteWorker()
	}
}

// Stop stops the batch zero-copy processor
func (bzcp *BatchZeroCopyProcessor) Stop() {
	if !atomic.CompareAndSwapInt32(&bzcp.running, 1, 0) {
		return // Already stopped
	}

	close(bzcp.readEncodeQueue)
	close(bzcp.decodeWriteQueue)
	bzcp.wg.Wait()
}

// readEncodeWorker processes batch read-encode operations
func (bzcp *BatchZeroCopyProcessor) readEncodeWorker() {
	defer bzcp.wg.Done()

	for req := range bzcp.readEncodeQueue {
		bzcp.processBatchReadEncode(req)
	}
}

// decodeWriteWorker processes batch decode-write operations
func (bzcp *BatchZeroCopyProcessor) decodeWriteWorker() {
	defer bzcp.wg.Done()

	for req := range bzcp.decodeWriteQueue {
		bzcp.processBatchDecodeWrite(req)
	}
}

// processBatchReadEncode processes a batch of read-encode operations
func (bzcp *BatchZeroCopyProcessor) processBatchReadEncode(req *batchZeroCopyRequest) {
	startTime := time.Now()
	result := batchZeroCopyResult{}

	// Batch AddRef all frames first
	err := BatchAddRefFrames(req.frames)
	if err != nil {
		result.err = err
		if req.resultCh != nil {
			req.resultCh <- result
			close(req.resultCh)
		}
		return
	}

	// Process frames using existing batch read-encode logic
	encodedData, err := BatchReadEncode(len(req.frames))
	if err != nil {
		// Batch release frames on error
		if _, releaseErr := BatchReleaseFrames(req.frames); releaseErr != nil {
			// Log release error but preserve original error
			_ = releaseErr
		}
		result.err = err
	} else {
		result.encodedData = encodedData
		result.processedCount = len(encodedData)
		// Batch release frames after successful processing
		if _, releaseErr := BatchReleaseFrames(req.frames); releaseErr != nil {
			// Log release error but don't fail the operation
			_ = releaseErr
		}
	}

	// Update statistics
	atomic.AddInt64(&bzcp.batchedFrames, int64(len(req.frames)))
	atomic.AddInt64(&bzcp.batchSavings, int64(len(req.frames)-1))
	atomic.AddInt64(&bzcp.processingTimeUs, time.Since(startTime).Microseconds())

	// Send result back
	if req.resultCh != nil {
		req.resultCh <- result
		close(req.resultCh)
	}
}

// processBatchDecodeWrite processes a batch of decode-write operations
func (bzcp *BatchZeroCopyProcessor) processBatchDecodeWrite(req *batchZeroCopyRequest) {
	startTime := time.Now()
	result := batchZeroCopyResult{}

	// Batch AddRef all frames first
	err := BatchAddRefFrames(req.frames)
	if err != nil {
		result.err = err
		if req.resultCh != nil {
			req.resultCh <- result
			close(req.resultCh)
		}
		return
	}

	// Extract data from zero-copy frames for batch processing
	frameData := make([][]byte, len(req.frames))
	for i, frame := range req.frames {
		if frame != nil {
			// Get data from zero-copy frame
			frameData[i] = frame.Data()[:frame.Length()]
		}
	}

	// Process frames using existing batch decode-write logic
	err = BatchDecodeWrite(frameData)
	if err != nil {
		result.err = err
	} else {
		result.processedCount = len(req.frames)
	}

	// Batch release frames
	if _, releaseErr := BatchReleaseFrames(req.frames); releaseErr != nil {
		// Log release error but don't override processing error
		_ = releaseErr
	}

	// Update statistics
	atomic.AddInt64(&bzcp.batchedFrames, int64(len(req.frames)))
	atomic.AddInt64(&bzcp.batchSavings, int64(len(req.frames)-1))
	atomic.AddInt64(&bzcp.processingTimeUs, time.Since(startTime).Microseconds())

	// Send result back
	if req.resultCh != nil {
		req.resultCh <- result
		close(req.resultCh)
	}
}

// BatchReadEncodeZeroCopy performs batch read-encode on zero-copy frames
func (bzcp *BatchZeroCopyProcessor) BatchReadEncodeZeroCopy(frames []*ZeroCopyAudioFrame) ([][]byte, error) {
	if len(frames) == 0 {
		return nil, nil
	}

	// For small batches, use direct operations to avoid overhead
	if len(frames) <= 2 {
		atomic.AddInt64(&bzcp.singleFrames, int64(len(frames)))
		return bzcp.processSingleReadEncode(frames)
	}

	// Use adaptive threshold to determine batch vs single processing
	batchedFrames := atomic.LoadInt64(&bzcp.batchedFrames)
	singleFrames := atomic.LoadInt64(&bzcp.singleFrames)
	totalFrames := batchedFrames + singleFrames

	if totalFrames > 100 { // Only apply adaptive logic after some samples
		batchRatio := float64(batchedFrames) / float64(totalFrames)
		if batchRatio < bzcp.adaptiveThreshold {
			// Batch processing not effective, use single processing
			atomic.AddInt64(&bzcp.adaptiveMisses, 1)
			atomic.AddInt64(&bzcp.singleFrames, int64(len(frames)))
			return bzcp.processSingleReadEncode(frames)
		}
		atomic.AddInt64(&bzcp.adaptiveHits, 1)
	}

	// Use batch processing
	if atomic.LoadInt32(&bzcp.running) == 0 {
		// Fallback to single processing if batch processor not running
		atomic.AddInt64(&bzcp.singleFrames, int64(len(frames)))
		return bzcp.processSingleReadEncode(frames)
	}

	resultCh := make(chan batchZeroCopyResult, 1)
	req := &batchZeroCopyRequest{
		frames:    frames,
		operation: batchOpReadEncode,
		resultCh:  resultCh,
		timestamp: time.Now(),
	}

	select {
	case bzcp.readEncodeQueue <- req:
		// Wait for completion
		result := <-resultCh
		return result.encodedData, result.err
	default:
		// Queue full, fallback to single processing
		atomic.AddInt64(&bzcp.singleFrames, int64(len(frames)))
		return bzcp.processSingleReadEncode(frames)
	}
}

// BatchDecodeWriteZeroCopy performs batch decode-write on zero-copy frames
func (bzcp *BatchZeroCopyProcessor) BatchDecodeWriteZeroCopy(frames []*ZeroCopyAudioFrame) error {
	if len(frames) == 0 {
		return nil
	}

	// For small batches, use direct operations
	if len(frames) <= 2 {
		atomic.AddInt64(&bzcp.singleFrames, int64(len(frames)))
		return bzcp.processSingleDecodeWrite(frames)
	}

	// Use adaptive threshold
	batchedFrames := atomic.LoadInt64(&bzcp.batchedFrames)
	singleFrames := atomic.LoadInt64(&bzcp.singleFrames)
	totalFrames := batchedFrames + singleFrames

	if totalFrames > 100 {
		batchRatio := float64(batchedFrames) / float64(totalFrames)
		if batchRatio < bzcp.adaptiveThreshold {
			atomic.AddInt64(&bzcp.adaptiveMisses, 1)
			atomic.AddInt64(&bzcp.singleFrames, int64(len(frames)))
			return bzcp.processSingleDecodeWrite(frames)
		}
		atomic.AddInt64(&bzcp.adaptiveHits, 1)
	}

	// Use batch processing
	if atomic.LoadInt32(&bzcp.running) == 0 {
		atomic.AddInt64(&bzcp.singleFrames, int64(len(frames)))
		return bzcp.processSingleDecodeWrite(frames)
	}

	resultCh := make(chan batchZeroCopyResult, 1)
	req := &batchZeroCopyRequest{
		frames:    frames,
		operation: batchOpDecodeWrite,
		resultCh:  resultCh,
		timestamp: time.Now(),
	}

	select {
	case bzcp.decodeWriteQueue <- req:
		// Wait for completion
		result := <-resultCh
		return result.err
	default:
		// Queue full, fallback to single processing
		atomic.AddInt64(&bzcp.singleFrames, int64(len(frames)))
		return bzcp.processSingleDecodeWrite(frames)
	}
}

// processSingleReadEncode processes frames individually for read-encode
func (bzcp *BatchZeroCopyProcessor) processSingleReadEncode(frames []*ZeroCopyAudioFrame) ([][]byte, error) {
	// Extract data and use existing batch processing
	frameData := make([][]byte, 0, len(frames))
	for _, frame := range frames {
		if frame != nil {
			frame.AddRef()
			frameData = append(frameData, frame.Data()[:frame.Length()])
		}
	}

	// Use existing batch read-encode
	result, err := BatchReadEncode(len(frameData))

	// Release frames
	for _, frame := range frames {
		if frame != nil {
			frame.Release()
		}
	}

	return result, err
}

// processSingleDecodeWrite processes frames individually for decode-write
func (bzcp *BatchZeroCopyProcessor) processSingleDecodeWrite(frames []*ZeroCopyAudioFrame) error {
	// Extract data and use existing batch processing
	frameData := make([][]byte, 0, len(frames))
	for _, frame := range frames {
		if frame != nil {
			frame.AddRef()
			frameData = append(frameData, frame.Data()[:frame.Length()])
		}
	}

	// Use existing batch decode-write
	err := BatchDecodeWrite(frameData)

	// Release frames
	for _, frame := range frames {
		if frame != nil {
			frame.Release()
		}
	}

	return err
}

// GetBatchZeroCopyStats returns batch zero-copy processing statistics
func (bzcp *BatchZeroCopyProcessor) GetBatchZeroCopyStats() (batchedFrames, singleFrames, savings, processingTimeUs, adaptiveHits, adaptiveMisses int64) {
	return atomic.LoadInt64(&bzcp.batchedFrames),
		atomic.LoadInt64(&bzcp.singleFrames),
		atomic.LoadInt64(&bzcp.batchSavings),
		atomic.LoadInt64(&bzcp.processingTimeUs),
		atomic.LoadInt64(&bzcp.adaptiveHits),
		atomic.LoadInt64(&bzcp.adaptiveMisses)
}

// Convenience functions for global batch zero-copy processor

// BatchReadEncodeZeroCopyFrames performs batch read-encode on zero-copy frames
func BatchReadEncodeZeroCopyFrames(frames []*ZeroCopyAudioFrame) ([][]byte, error) {
	return GetBatchZeroCopyProcessor().BatchReadEncodeZeroCopy(frames)
}

// BatchDecodeWriteZeroCopyFrames performs batch decode-write on zero-copy frames
func BatchDecodeWriteZeroCopyFrames(frames []*ZeroCopyAudioFrame) error {
	return GetBatchZeroCopyProcessor().BatchDecodeWriteZeroCopy(frames)
}

// GetGlobalBatchZeroCopyStats returns global batch zero-copy processing statistics
func GetGlobalBatchZeroCopyStats() (batchedFrames, singleFrames, savings, processingTimeUs, adaptiveHits, adaptiveMisses int64) {
	return GetBatchZeroCopyProcessor().GetBatchZeroCopyStats()
}
