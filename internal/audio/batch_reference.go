//go:build cgo

package audio

import (
	"errors"
	"sync"
	"sync/atomic"
	"unsafe"
)

// BatchReferenceManager handles batch reference counting operations
// to reduce atomic operation overhead for high-frequency frame operations
type BatchReferenceManager struct {
	// Batch operations queue
	batchQueue chan batchRefOperation
	workerPool chan struct{} // Worker pool semaphore
	running    int32
	wg         sync.WaitGroup

	// Statistics
	batchedOps   int64
	singleOps    int64
	batchSavings int64 // Number of atomic operations saved
}

type batchRefOperation struct {
	frames    []*ZeroCopyAudioFrame
	operation refOperationType
	resultCh  chan batchRefResult
}

type refOperationType int

const (
	refOpAddRef refOperationType = iota
	refOpRelease
	refOpMixed // For operations with mixed AddRef/Release
)

// Errors
var (
	ErrUnsupportedOperation = errors.New("unsupported batch reference operation")
)

type batchRefResult struct {
	finalReleases []bool // For Release operations, indicates which frames had final release
	err           error
}

// Global batch reference manager
var (
	globalBatchRefManager *BatchReferenceManager
	batchRefOnce          sync.Once
)

// GetBatchReferenceManager returns the global batch reference manager
func GetBatchReferenceManager() *BatchReferenceManager {
	batchRefOnce.Do(func() {
		globalBatchRefManager = NewBatchReferenceManager()
		globalBatchRefManager.Start()
	})
	return globalBatchRefManager
}

// NewBatchReferenceManager creates a new batch reference manager
func NewBatchReferenceManager() *BatchReferenceManager {
	return &BatchReferenceManager{
		batchQueue: make(chan batchRefOperation, 256), // Buffered for high throughput
		workerPool: make(chan struct{}, 4),            // 4 workers for parallel processing
	}
}

// Start starts the batch reference manager workers
func (brm *BatchReferenceManager) Start() {
	if !atomic.CompareAndSwapInt32(&brm.running, 0, 1) {
		return // Already running
	}

	// Start worker goroutines
	for i := 0; i < cap(brm.workerPool); i++ {
		brm.wg.Add(1)
		go brm.worker()
	}
}

// Stop stops the batch reference manager
func (brm *BatchReferenceManager) Stop() {
	if !atomic.CompareAndSwapInt32(&brm.running, 1, 0) {
		return // Already stopped
	}

	close(brm.batchQueue)
	brm.wg.Wait()
}

// worker processes batch reference operations
func (brm *BatchReferenceManager) worker() {
	defer brm.wg.Done()

	for op := range brm.batchQueue {
		brm.processBatchOperation(op)
	}
}

// processBatchOperation processes a batch of reference operations
func (brm *BatchReferenceManager) processBatchOperation(op batchRefOperation) {
	result := batchRefResult{}

	switch op.operation {
	case refOpAddRef:
		// Batch AddRef operations
		for _, frame := range op.frames {
			if frame != nil {
				atomic.AddInt32(&frame.refCount, 1)
			}
		}
		atomic.AddInt64(&brm.batchedOps, int64(len(op.frames)))
		atomic.AddInt64(&brm.batchSavings, int64(len(op.frames)-1)) // Saved ops vs individual calls

	case refOpRelease:
		// Batch Release operations
		result.finalReleases = make([]bool, len(op.frames))
		for i, frame := range op.frames {
			if frame != nil {
				newCount := atomic.AddInt32(&frame.refCount, -1)
				if newCount == 0 {
					result.finalReleases[i] = true
					// Return to pool if pooled
					if frame.pooled {
						globalZeroCopyPool.Put(frame)
					}
				}
			}
		}
		atomic.AddInt64(&brm.batchedOps, int64(len(op.frames)))
		atomic.AddInt64(&brm.batchSavings, int64(len(op.frames)-1))

	case refOpMixed:
		// Handle mixed operations (not implemented in this version)
		result.err = ErrUnsupportedOperation
	}

	// Send result back
	if op.resultCh != nil {
		op.resultCh <- result
		close(op.resultCh)
	}
}

// BatchAddRef performs AddRef on multiple frames in a single batch
func (brm *BatchReferenceManager) BatchAddRef(frames []*ZeroCopyAudioFrame) error {
	if len(frames) == 0 {
		return nil
	}

	// For small batches, use direct operations to avoid overhead
	if len(frames) <= 2 {
		for _, frame := range frames {
			if frame != nil {
				frame.AddRef()
			}
		}
		atomic.AddInt64(&brm.singleOps, int64(len(frames)))
		return nil
	}

	// Use batch processing for larger sets
	if atomic.LoadInt32(&brm.running) == 0 {
		// Fallback to individual operations if batch manager not running
		for _, frame := range frames {
			if frame != nil {
				frame.AddRef()
			}
		}
		atomic.AddInt64(&brm.singleOps, int64(len(frames)))
		return nil
	}

	resultCh := make(chan batchRefResult, 1)
	op := batchRefOperation{
		frames:    frames,
		operation: refOpAddRef,
		resultCh:  resultCh,
	}

	select {
	case brm.batchQueue <- op:
		// Wait for completion
		<-resultCh
		return nil
	default:
		// Queue full, fallback to individual operations
		for _, frame := range frames {
			if frame != nil {
				frame.AddRef()
			}
		}
		atomic.AddInt64(&brm.singleOps, int64(len(frames)))
		return nil
	}
}

// BatchRelease performs Release on multiple frames in a single batch
// Returns a slice indicating which frames had their final reference released
func (brm *BatchReferenceManager) BatchRelease(frames []*ZeroCopyAudioFrame) ([]bool, error) {
	if len(frames) == 0 {
		return nil, nil
	}

	// For small batches, use direct operations
	if len(frames) <= 2 {
		finalReleases := make([]bool, len(frames))
		for i, frame := range frames {
			if frame != nil {
				finalReleases[i] = frame.Release()
			}
		}
		atomic.AddInt64(&brm.singleOps, int64(len(frames)))
		return finalReleases, nil
	}

	// Use batch processing for larger sets
	if atomic.LoadInt32(&brm.running) == 0 {
		// Fallback to individual operations
		finalReleases := make([]bool, len(frames))
		for i, frame := range frames {
			if frame != nil {
				finalReleases[i] = frame.Release()
			}
		}
		atomic.AddInt64(&brm.singleOps, int64(len(frames)))
		return finalReleases, nil
	}

	resultCh := make(chan batchRefResult, 1)
	op := batchRefOperation{
		frames:    frames,
		operation: refOpRelease,
		resultCh:  resultCh,
	}

	select {
	case brm.batchQueue <- op:
		// Wait for completion
		result := <-resultCh
		return result.finalReleases, result.err
	default:
		// Queue full, fallback to individual operations
		finalReleases := make([]bool, len(frames))
		for i, frame := range frames {
			if frame != nil {
				finalReleases[i] = frame.Release()
			}
		}
		atomic.AddInt64(&brm.singleOps, int64(len(frames)))
		return finalReleases, nil
	}
}

// GetStats returns batch reference counting statistics
func (brm *BatchReferenceManager) GetStats() (batchedOps, singleOps, savings int64) {
	return atomic.LoadInt64(&brm.batchedOps),
		atomic.LoadInt64(&brm.singleOps),
		atomic.LoadInt64(&brm.batchSavings)
}

// Convenience functions for global batch reference manager

// BatchAddRefFrames performs batch AddRef on multiple frames
func BatchAddRefFrames(frames []*ZeroCopyAudioFrame) error {
	return GetBatchReferenceManager().BatchAddRef(frames)
}

// BatchReleaseFrames performs batch Release on multiple frames
func BatchReleaseFrames(frames []*ZeroCopyAudioFrame) ([]bool, error) {
	return GetBatchReferenceManager().BatchRelease(frames)
}

// GetBatchReferenceStats returns global batch reference statistics
func GetBatchReferenceStats() (batchedOps, singleOps, savings int64) {
	return GetBatchReferenceManager().GetStats()
}

// ZeroCopyFrameSlice provides utilities for working with slices of zero-copy frames
type ZeroCopyFrameSlice []*ZeroCopyAudioFrame

// AddRefAll performs batch AddRef on all frames in the slice
func (zfs ZeroCopyFrameSlice) AddRefAll() error {
	return BatchAddRefFrames(zfs)
}

// ReleaseAll performs batch Release on all frames in the slice
func (zfs ZeroCopyFrameSlice) ReleaseAll() ([]bool, error) {
	return BatchReleaseFrames(zfs)
}

// FilterNonNil returns a new slice with only non-nil frames
func (zfs ZeroCopyFrameSlice) FilterNonNil() ZeroCopyFrameSlice {
	filtered := make(ZeroCopyFrameSlice, 0, len(zfs))
	for _, frame := range zfs {
		if frame != nil {
			filtered = append(filtered, frame)
		}
	}
	return filtered
}

// Len returns the number of frames in the slice
func (zfs ZeroCopyFrameSlice) Len() int {
	return len(zfs)
}

// Get returns the frame at the specified index
func (zfs ZeroCopyFrameSlice) Get(index int) *ZeroCopyAudioFrame {
	if index < 0 || index >= len(zfs) {
		return nil
	}
	return zfs[index]
}

// UnsafePointers returns unsafe pointers for all frames (for CGO batch operations)
func (zfs ZeroCopyFrameSlice) UnsafePointers() []unsafe.Pointer {
	pointers := make([]unsafe.Pointer, len(zfs))
	for i, frame := range zfs {
		if frame != nil {
			pointers[i] = frame.UnsafePointer()
		}
	}
	return pointers
}
