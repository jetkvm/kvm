package audio

import (
	"context"
	"runtime"
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

	// Start the blocking worker thread
	nam.wg.Add(1)
	go nam.outputWorkerThread()

	// Start the non-blocking coordinator
	nam.wg.Add(1)
	go nam.outputCoordinatorThread()

	nam.logger.Info().Msg("non-blocking audio output started")
	return nil
}

// StartAudioInput starts non-blocking audio input (receive and decode)
func (nam *NonBlockingAudioManager) StartAudioInput(receiveChan <-chan []byte) error {
	if !atomic.CompareAndSwapInt32(&nam.inputRunning, 0, 1) {
		return ErrAudioAlreadyRunning
	}

	nam.inputReceiveChan = receiveChan

	// Start the blocking worker thread
	nam.wg.Add(1)
	go nam.inputWorkerThread()

	// Start the non-blocking coordinator
	nam.wg.Add(1)
	go nam.inputCoordinatorThread()

	nam.logger.Info().Msg("non-blocking audio input started")
	return nil
}

// outputWorkerThread handles all blocking audio output operations
func (nam *NonBlockingAudioManager) outputWorkerThread() {
	// Lock to OS thread to isolate blocking CGO operations
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

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

	buf := make([]byte, 1500)

	for {
		select {
		case <-nam.ctx.Done():
			nam.logger.Debug().Msg("output worker thread stopping")
			return

		case workItem := <-nam.outputWorkChan:
			switch workItem.workType {
			case audioWorkReadEncode:
				// Perform blocking audio read/encode operation
				n, err := CGOAudioReadEncode(buf)
				result := audioResult{
					success: err == nil,
					length:  n,
					err:     err,
				}
				if err == nil && n > 0 {
					// Copy data to avoid race conditions
					result.data = make([]byte, n)
					copy(result.data, buf[:n])
				}

				// Send result back (non-blocking)
				select {
				case workItem.resultChan <- result:
				case <-nam.ctx.Done():
					return
				default:
					// Drop result if coordinator is not ready
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
			} else if result.success && result.length == 0 {
				// No data available - this is normal, not an error
				// Just continue without logging or counting as error
			} else {
				atomic.AddInt64(&nam.stats.OutputFramesDropped, 1)
				atomic.AddInt64(&nam.stats.WorkerErrors, 1)
				if result.err != nil {
					nam.logger.Warn().Err(result.err).Msg("audio output worker error")
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
	// Lock to OS thread to isolate blocking CGO operations
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	defer nam.wg.Done()
	defer atomic.StoreInt32(&nam.inputWorkerRunning, 0)

	atomic.StoreInt32(&nam.inputWorkerRunning, 1)
	nam.logger.Debug().Msg("input worker thread started")

	// Initialize audio playback in worker thread
	if err := CGOAudioPlaybackInit(); err != nil {
		nam.logger.Error().Err(err).Msg("failed to initialize audio playback in worker thread")
		return
	}
	defer CGOAudioPlaybackClose()

	for {
		select {
		case <-nam.ctx.Done():
			nam.logger.Debug().Msg("input worker thread stopping")
			return

		case workItem := <-nam.inputWorkChan:
			switch workItem.workType {
			case audioWorkDecodeWrite:
				// Perform blocking audio decode/write operation
				n, err := CGOAudioDecodeWrite(workItem.data)
				result := audioResult{
					success: err == nil,
					length:  n,
					err:     err,
				}

				// Send result back (non-blocking)
				select {
				case workItem.resultChan <- result:
				case <-nam.ctx.Done():
					return
				default:
					// Drop result if coordinator is not ready
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
					// Wait for result with timeout
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
					case <-time.After(50 * time.Millisecond):
						// Timeout waiting for result
						atomic.AddInt64(&nam.stats.InputFramesDropped, 1)
						nam.logger.Warn().Msg("timeout waiting for input worker result")
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

	// Signal worker to close
	select {
	case nam.inputWorkChan <- audioWorkItem{workType: audioWorkClose}:
	case <-time.After(100 * time.Millisecond):
		nam.logger.Warn().Msg("timeout signaling input worker to close")
	}

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

	nam.logger.Info().Msg("non-blocking audio manager stopped")
}

// StopAudioInput stops only the audio input operations
func (nam *NonBlockingAudioManager) StopAudioInput() {
	nam.logger.Info().Msg("stopping audio input")

	// Stop only the input coordinator
	atomic.StoreInt32(&nam.inputRunning, 0)

	// Allow coordinator thread to process the stop signal and update state
	// This prevents race conditions in state queries immediately after stopping
	time.Sleep(50 * time.Millisecond)

	nam.logger.Info().Msg("audio input stopped")
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
