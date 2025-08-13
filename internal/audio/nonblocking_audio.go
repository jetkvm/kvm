package audio

import (
	"context"
	"errors"
	// "runtime" // removed: no longer directly pinning OS thread here; batching handles it
	"sync"
	"sync/atomic"
	"time"

	"github.com/jetkvm/kvm/internal/logging"
	"github.com/rs/zerolog"
)

// NonBlockingAudioManager manages audio operations in separate worker threads
// to prevent blocking of mouse/keyboard operations
type NonBlockingAudioManager struct {
	// Statistics - MUST be first for ARM32 alignment (int64 fields need 8-byte alignment)
	stats NonBlockingAudioStats

	// Control
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	logger *zerolog.Logger

	// Audio output (capture from device, send to WebRTC)
	outputSendFunc   func([]byte)
	outputWorkChan   chan audioWorkItem
	outputResultChan chan audioResult

	// Audio input (receive from WebRTC, playback to device)
	inputReceiveChan <-chan []byte
	inputWorkChan    chan audioWorkItem
	inputResultChan  chan audioResult

	// Worker threads and flags - int32 fields grouped together
	outputRunning       int32
	inputRunning        int32
	outputWorkerRunning int32
	inputWorkerRunning  int32
}

type audioWorkItem struct {
	workType   audioWorkType
	data       []byte
	resultChan chan audioResult
}

type audioWorkType int

const (
	audioWorkInit audioWorkType = iota
	audioWorkReadEncode
	audioWorkDecodeWrite
	audioWorkClose
)

type audioResult struct {
	success bool
	data    []byte
	length  int
	err     error
}

type NonBlockingAudioStats struct {
	// int64 fields MUST be first for ARM32 alignment
	OutputFramesProcessed int64
	OutputFramesDropped   int64
	InputFramesProcessed  int64
	InputFramesDropped    int64
	WorkerErrors          int64
	// time.Time is int64 internally, so it's also aligned
	LastProcessTime time.Time
}

// NewNonBlockingAudioManager creates a new non-blocking audio manager
func NewNonBlockingAudioManager() *NonBlockingAudioManager {
	ctx, cancel := context.WithCancel(context.Background())
	logger := logging.GetDefaultLogger().With().Str("component", "nonblocking-audio").Logger()

	return &NonBlockingAudioManager{
		ctx:              ctx,
		cancel:           cancel,
		logger:           &logger,
		outputWorkChan:   make(chan audioWorkItem, 10), // Buffer for work items
		outputResultChan: make(chan audioResult, 10),   // Buffer for results
		inputWorkChan:    make(chan audioWorkItem, 10),
		inputResultChan:  make(chan audioResult, 10),
	}
}

// StartAudioOutput starts non-blocking audio output (capture and encode)
func (nam *NonBlockingAudioManager) StartAudioOutput(sendFunc func([]byte)) error {
	if !atomic.CompareAndSwapInt32(&nam.outputRunning, 0, 1) {
		return ErrAudioAlreadyRunning
	}

	nam.outputSendFunc = sendFunc

	// Enable batch audio processing for performance
	EnableBatchAudioProcessing()

	// Start the blocking worker thread
	nam.wg.Add(1)
	go nam.outputWorkerThread()

	// Start the non-blocking coordinator
	nam.wg.Add(1)
	go nam.outputCoordinatorThread()

	nam.logger.Info().Msg("non-blocking audio output started with batch processing")
	return nil
}

// StartAudioInput starts non-blocking audio input (receive and decode)
func (nam *NonBlockingAudioManager) StartAudioInput(receiveChan <-chan []byte) error {
	if !atomic.CompareAndSwapInt32(&nam.inputRunning, 0, 1) {
		return ErrAudioAlreadyRunning
	}

	nam.inputReceiveChan = receiveChan

	// Enable batch audio processing for performance
	EnableBatchAudioProcessing()

	// Start the blocking worker thread
	nam.wg.Add(1)
	go nam.inputWorkerThread()

	// Start the non-blocking coordinator
	nam.wg.Add(1)
	go nam.inputCoordinatorThread()

	nam.logger.Info().Msg("non-blocking audio input started with batch processing")
	return nil
}

// outputWorkerThread handles all blocking audio output operations
func (nam *NonBlockingAudioManager) outputWorkerThread() {
	defer nam.wg.Done()
	defer atomic.StoreInt32(&nam.outputWorkerRunning, 0)

	atomic.StoreInt32(&nam.outputWorkerRunning, 1)
	nam.logger.Debug().Msg("output worker thread started")

	// Initialize audio in worker thread
	if err := CGOAudioInit(); err != nil {
		nam.logger.Error().Err(err).Msg("failed to initialize audio in worker thread")
		return
	}
	defer CGOAudioClose()

	// Use buffer pool to avoid allocations
	buf := GetAudioFrameBuffer()
	defer PutAudioFrameBuffer(buf)

	for {
		select {
		case <-nam.ctx.Done():
			nam.logger.Debug().Msg("output worker thread stopping")
			return

		case workItem := <-nam.outputWorkChan:
			switch workItem.workType {
			case audioWorkReadEncode:
				n, err := BatchCGOAudioReadEncode(buf)
					
					result := audioResult{
					success: err == nil,
					length:  n,
					err:     err,
				}
				if err == nil && n > 0 {
					// Get buffer from pool and copy data
					resultBuf := GetAudioFrameBuffer()
					copy(resultBuf[:n], buf[:n])
					result.data = resultBuf[:n]
				}

				// Send result back (non-blocking)
				select {
				case workItem.resultChan <- result:
				case <-nam.ctx.Done():
					return
				default:
					// Drop result if coordinator is not ready
					if result.data != nil {
						PutAudioFrameBuffer(result.data)
					}
					atomic.AddInt64(&nam.stats.OutputFramesDropped, 1)
				}

			case audioWorkClose:
				nam.logger.Debug().Msg("output worker received close signal")
				return
			}
		}
	}
}

// outputCoordinatorThread coordinates audio output without blocking
func (nam *NonBlockingAudioManager) outputCoordinatorThread() {
	defer nam.wg.Done()
	defer atomic.StoreInt32(&nam.outputRunning, 0)

	nam.logger.Debug().Msg("output coordinator thread started")

	ticker := time.NewTicker(20 * time.Millisecond) // Match frame timing
	defer ticker.Stop()

	pendingWork := false
	resultChan := make(chan audioResult, 1)

	for atomic.LoadInt32(&nam.outputRunning) == 1 {
		select {
		case <-nam.ctx.Done():
			nam.logger.Debug().Msg("output coordinator stopping")
			return

		case <-ticker.C:
			// Only submit work if worker is ready and no pending work
			if !pendingWork && atomic.LoadInt32(&nam.outputWorkerRunning) == 1 {
				if IsAudioMuted() {
					continue // Skip when muted
				}

				workItem := audioWorkItem{
					workType:   audioWorkReadEncode,
					resultChan: resultChan,
				}

				// Submit work (non-blocking)
				select {
				case nam.outputWorkChan <- workItem:
					pendingWork = true
				default:
					// Worker is busy, drop this frame
					atomic.AddInt64(&nam.stats.OutputFramesDropped, 1)
				}
			}

		case result := <-resultChan:
			pendingWork = false
			nam.stats.LastProcessTime = time.Now()

			if result.success && result.data != nil && result.length > 0 {
				// Send to WebRTC (non-blocking)
				if nam.outputSendFunc != nil {
					nam.outputSendFunc(result.data)
					atomic.AddInt64(&nam.stats.OutputFramesProcessed, 1)
					RecordFrameReceived(result.length)
				}
				// Return buffer to pool after use
				PutAudioFrameBuffer(result.data)
			} else if result.success && result.length == 0 {
				// No data available - this is normal, not an error
				// Just continue without logging or counting as error
			} else {
				atomic.AddInt64(&nam.stats.OutputFramesDropped, 1)
				atomic.AddInt64(&nam.stats.WorkerErrors, 1)
				if result.err != nil {
					nam.logger.Warn().Err(result.err).Msg("audio output worker error")
				}
				// Clean up buffer if present
				if result.data != nil {
					PutAudioFrameBuffer(result.data)
				}
				RecordFrameDropped()
			}
		}
	}

	// Signal worker to close
	select {
	case nam.outputWorkChan <- audioWorkItem{workType: audioWorkClose}:
	case <-time.After(100 * time.Millisecond):
		nam.logger.Warn().Msg("timeout signaling output worker to close")
	}

	nam.logger.Info().Msg("output coordinator thread stopped")
}

// inputWorkerThread handles all blocking audio input operations
func (nam *NonBlockingAudioManager) inputWorkerThread() {
	defer nam.wg.Done()
	// Cleanup CGO resources properly to avoid double-close scenarios
	// The outputWorkerThread's CGOAudioClose() will handle all cleanup
	atomic.StoreInt32(&nam.inputWorkerRunning, 0)

	atomic.StoreInt32(&nam.inputWorkerRunning, 1)
	nam.logger.Debug().Msg("input worker thread started")

	// Initialize audio playback in worker thread
	if err := CGOAudioPlaybackInit(); err != nil {
		nam.logger.Error().Err(err).Msg("failed to initialize audio playback in worker thread")
		return
	}
	
	// Ensure CGO cleanup happens even if we exit unexpectedly
	cgoInitialized := true
	defer func() {
		if cgoInitialized {
			nam.logger.Debug().Msg("cleaning up CGO audio playback")
			// Add extra safety: ensure no more CGO calls can happen
			atomic.StoreInt32(&nam.inputWorkerRunning, 0)
			// Note: Don't call CGOAudioPlaybackClose() here to avoid double-close
			// The outputWorkerThread's CGOAudioClose() will handle all cleanup
		}
	}()

	for {
		// If coordinator has stopped, exit worker loop
		if atomic.LoadInt32(&nam.inputRunning) == 0 {
			return
		}
		select {
		case <-nam.ctx.Done():
			nam.logger.Debug().Msg("input worker thread stopping due to context cancellation")
			return

		case workItem := <-nam.inputWorkChan:
			switch workItem.workType {
			case audioWorkDecodeWrite:
				// Check if we're still supposed to be running before processing
				if atomic.LoadInt32(&nam.inputWorkerRunning) == 0 || atomic.LoadInt32(&nam.inputRunning) == 0 {
					nam.logger.Debug().Msg("input worker stopping, ignoring decode work")
					// Do not send to resultChan; coordinator may have exited
					return
				}
				
				// Validate input data before CGO call
				if workItem.data == nil || len(workItem.data) == 0 {
					result := audioResult{
						success: false,
						err:     errors.New("invalid audio data"),
					}
					
					// Check if coordinator is still running before sending result
					if atomic.LoadInt32(&nam.inputRunning) == 1 {
						select {
						case workItem.resultChan <- result:
						case <-nam.ctx.Done():
							return
						case <-time.After(10 * time.Millisecond):
							// Timeout - coordinator may have stopped, drop result
							atomic.AddInt64(&nam.stats.InputFramesDropped, 1)
						}
					} else {
						// Coordinator has stopped, drop result
						atomic.AddInt64(&nam.stats.InputFramesDropped, 1)
					}
					continue
				}

				// Perform blocking CGO operation with panic recovery
				var result audioResult
				func() {
					defer func() {
						if r := recover(); r != nil {
							nam.logger.Error().Interface("panic", r).Msg("CGO decode write panic recovered")
							result = audioResult{
								success: false,
								err:     errors.New("CGO decode write panic"),
							}
						}
					}()
					
					// Double-check we're still running before CGO call
					if atomic.LoadInt32(&nam.inputWorkerRunning) == 0 {
						result = audioResult{success: false, err: errors.New("worker shutting down")}
						return
					}
					
					n, err := BatchCGOAudioDecodeWrite(workItem.data)
					
					result = audioResult{
						success: err == nil,
						length:  n,
						err:     err,
					}
				}()

				// Send result back (non-blocking) - check if coordinator is still running
				if atomic.LoadInt32(&nam.inputRunning) == 1 {
					select {
					case workItem.resultChan <- result:
					case <-nam.ctx.Done():
						return
					case <-time.After(10 * time.Millisecond):
						// Timeout - coordinator may have stopped, drop result
						atomic.AddInt64(&nam.stats.InputFramesDropped, 1)
					}
				} else {
					// Coordinator has stopped, drop result
					atomic.AddInt64(&nam.stats.InputFramesDropped, 1)
				}

			case audioWorkClose:
				nam.logger.Debug().Msg("input worker received close signal")
				return
			}
		}
	}
}

// inputCoordinatorThread coordinates audio input without blocking
func (nam *NonBlockingAudioManager) inputCoordinatorThread() {
	defer nam.wg.Done()
	defer atomic.StoreInt32(&nam.inputRunning, 0)

	nam.logger.Debug().Msg("input coordinator thread started")

	resultChan := make(chan audioResult, 1)
	// Do not close resultChan to avoid races with worker sends during shutdown

	for atomic.LoadInt32(&nam.inputRunning) == 1 {
		select {
		case <-nam.ctx.Done():
			nam.logger.Debug().Msg("input coordinator stopping")
			return

		case frame := <-nam.inputReceiveChan:
			if len(frame) == 0 {
				continue
			}

			// Submit work to worker (non-blocking)
			if atomic.LoadInt32(&nam.inputWorkerRunning) == 1 {
				workItem := audioWorkItem{
					workType:   audioWorkDecodeWrite,
					data:       frame,
					resultChan: resultChan,
				}

				select {
				case nam.inputWorkChan <- workItem:
					// Wait for result with timeout and context cancellation
					select {
					case result := <-resultChan:
						if result.success {
							atomic.AddInt64(&nam.stats.InputFramesProcessed, 1)
						} else {
							atomic.AddInt64(&nam.stats.InputFramesDropped, 1)
							atomic.AddInt64(&nam.stats.WorkerErrors, 1)
							if result.err != nil {
								nam.logger.Warn().Err(result.err).Msg("audio input worker error")
							}
						}
					case <-nam.ctx.Done():
						nam.logger.Debug().Msg("input coordinator stopping during result wait")
						return
					case <-time.After(50 * time.Millisecond):
						// Timeout waiting for result
						atomic.AddInt64(&nam.stats.InputFramesDropped, 1)
						nam.logger.Warn().Msg("timeout waiting for input worker result")
						// Drain any pending result to prevent worker blocking
						select {
						case <-resultChan:
						default:
						}
					}
				default:
					// Worker is busy, drop this frame
					atomic.AddInt64(&nam.stats.InputFramesDropped, 1)
				}
			}

		case <-time.After(250 * time.Millisecond):
			// Periodic timeout to prevent blocking
			continue
		}
	}

	// Avoid sending close signals or touching channels here; inputRunning=0 will stop worker via checks
	nam.logger.Info().Msg("input coordinator thread stopped")
}

// Stop stops all audio operations
func (nam *NonBlockingAudioManager) Stop() {
	nam.logger.Info().Msg("stopping non-blocking audio manager")

	// Signal all threads to stop
	nam.cancel()

	// Stop coordinators
	atomic.StoreInt32(&nam.outputRunning, 0)
	atomic.StoreInt32(&nam.inputRunning, 0)

	// Wait for all goroutines to finish
	nam.wg.Wait()

	// Disable batch processing to free resources
	DisableBatchAudioProcessing()

	nam.logger.Info().Msg("non-blocking audio manager stopped")
}

// StopAudioInput stops only the audio input operations
func (nam *NonBlockingAudioManager) StopAudioInput() {
	nam.logger.Info().Msg("stopping audio input")

	// Stop only the input coordinator
	atomic.StoreInt32(&nam.inputRunning, 0)

	// Drain the receive channel to prevent blocking senders
	go func() {
		for {
			select {
			case <-nam.inputReceiveChan:
				// Drain any remaining frames
			case <-time.After(100 * time.Millisecond):
				return
			}
		}
	}()

	// Wait for the worker to actually stop to prevent race conditions
	timeout := time.After(2 * time.Second)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			nam.logger.Warn().Msg("timeout waiting for input worker to stop")
			return
		case <-ticker.C:
			if atomic.LoadInt32(&nam.inputWorkerRunning) == 0 {
				nam.logger.Info().Msg("audio input stopped successfully")
				// Close ALSA playback resources now that input worker has stopped
				CGOAudioPlaybackClose()
				return
			}
		}
	}
}

// GetStats returns current statistics
func (nam *NonBlockingAudioManager) GetStats() NonBlockingAudioStats {
	return NonBlockingAudioStats{
		OutputFramesProcessed: atomic.LoadInt64(&nam.stats.OutputFramesProcessed),
		OutputFramesDropped:   atomic.LoadInt64(&nam.stats.OutputFramesDropped),
		InputFramesProcessed:  atomic.LoadInt64(&nam.stats.InputFramesProcessed),
		InputFramesDropped:    atomic.LoadInt64(&nam.stats.InputFramesDropped),
		WorkerErrors:          atomic.LoadInt64(&nam.stats.WorkerErrors),
		LastProcessTime:       nam.stats.LastProcessTime,
	}
}

// IsRunning returns true if any audio operations are running
func (nam *NonBlockingAudioManager) IsRunning() bool {
	return atomic.LoadInt32(&nam.outputRunning) == 1 || atomic.LoadInt32(&nam.inputRunning) == 1
}

// IsInputRunning returns true if audio input is running
func (nam *NonBlockingAudioManager) IsInputRunning() bool {
	return atomic.LoadInt32(&nam.inputRunning) == 1
}

// IsOutputRunning returns true if audio output is running
func (nam *NonBlockingAudioManager) IsOutputRunning() bool {
	return atomic.LoadInt32(&nam.outputRunning) == 1
}
