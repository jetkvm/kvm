package audio

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/jetkvm/kvm/internal/logging"
	"github.com/rs/zerolog"
)

// Task represents a function to be executed by a worker in the pool
type Task func()

// GoroutinePool manages a pool of reusable goroutines to reduce the overhead
// of goroutine creation and destruction
type GoroutinePool struct {
	// Atomic fields must be first for proper alignment on 32-bit systems
	taskCount    int64 // Number of tasks processed
	workerCount  int64 // Current number of workers
	maxIdleTime  time.Duration
	maxWorkers   int
	taskQueue    chan Task
	workerSem    chan struct{} // Semaphore to limit concurrent workers
	shutdown     chan struct{}
	shutdownOnce sync.Once
	wg           sync.WaitGroup
	logger       *zerolog.Logger
	name         string
}

// NewGoroutinePool creates a new goroutine pool with the specified parameters
func NewGoroutinePool(name string, maxWorkers int, queueSize int, maxIdleTime time.Duration) *GoroutinePool {
	logger := logging.GetDefaultLogger().With().Str("component", "goroutine-pool").Str("pool", name).Logger()

	pool := &GoroutinePool{
		maxWorkers:  maxWorkers,
		maxIdleTime: maxIdleTime,
		taskQueue:   make(chan Task, queueSize),
		workerSem:   make(chan struct{}, maxWorkers),
		shutdown:    make(chan struct{}),
		logger:      &logger,
		name:        name,
	}

	// Start a supervisor goroutine to monitor pool health
	go pool.supervisor()

	return pool
}

// Submit adds a task to the pool for execution
// Returns true if the task was accepted, false if the queue is full
func (p *GoroutinePool) Submit(task Task) bool {
	select {
	case <-p.shutdown:
		return false // Pool is shutting down
	case p.taskQueue <- task:
		// Task accepted, ensure we have a worker to process it
		p.ensureWorkerAvailable()
		return true
	default:
		// Queue is full
		return false
	}
}

// ensureWorkerAvailable makes sure at least one worker is available to process tasks
func (p *GoroutinePool) ensureWorkerAvailable() {
	// Check if we already have enough workers
	currentWorkers := atomic.LoadInt64(&p.workerCount)

	// Only start new workers if:
	// 1. We have no workers at all, or
	// 2. The queue is growing and we're below max workers
	queueLen := len(p.taskQueue)
	if currentWorkers == 0 || (queueLen > int(currentWorkers) && currentWorkers < int64(p.maxWorkers)) {
		// Try to acquire a semaphore slot without blocking
		select {
		case p.workerSem <- struct{}{}:
			// We got a slot, start a new worker
			p.startWorker()
		default:
			// All worker slots are taken, which means we have enough workers
		}
	}
}

// startWorker launches a new worker goroutine
func (p *GoroutinePool) startWorker() {
	p.wg.Add(1)
	atomic.AddInt64(&p.workerCount, 1)

	go func() {
		defer func() {
			atomic.AddInt64(&p.workerCount, -1)
			<-p.workerSem // Release the semaphore slot
			p.wg.Done()

			// Recover from panics in worker tasks
			if r := recover(); r != nil {
				p.logger.Error().Interface("panic", r).Msg("Worker recovered from panic")
			}
		}()

		idleTimer := time.NewTimer(p.maxIdleTime)
		defer idleTimer.Stop()

		for {
			select {
			case <-p.shutdown:
				return
			case task, ok := <-p.taskQueue:
				if !ok {
					return // Channel closed
				}

				// Reset idle timer
				if !idleTimer.Stop() {
					<-idleTimer.C
				}
				idleTimer.Reset(p.maxIdleTime)

				// Execute the task with panic recovery
				func() {
					defer func() {
						if r := recover(); r != nil {
							p.logger.Error().Interface("panic", r).Msg("Task execution panic recovered")
						}
					}()
					task()
				}()

				atomic.AddInt64(&p.taskCount, 1)
			case <-idleTimer.C:
				// Worker has been idle for too long
				// Keep at least 2 workers alive to handle incoming tasks without creating new goroutines
				if atomic.LoadInt64(&p.workerCount) > 2 {
					return
				}
				// For persistent workers (the minimum 2), use a longer idle timeout
				// This prevents excessive worker creation/destruction cycles
				idleTimer.Reset(p.maxIdleTime * 3) // Triple the idle time for persistent workers
			}
		}
	}()
}

// supervisor monitors the pool and logs statistics periodically
func (p *GoroutinePool) supervisor() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-p.shutdown:
			return
		case <-ticker.C:
			workers := atomic.LoadInt64(&p.workerCount)
			tasks := atomic.LoadInt64(&p.taskCount)
			queueLen := len(p.taskQueue)

			p.logger.Info().
				Int64("workers", workers).
				Int64("tasks_processed", tasks).
				Int("queue_length", queueLen).
				Msg("Pool statistics")
		}
	}
}

// Shutdown gracefully shuts down the pool
// If wait is true, it will wait for all tasks to complete
// If wait is false, it will terminate immediately, potentially leaving tasks unprocessed
func (p *GoroutinePool) Shutdown(wait bool) {
	p.shutdownOnce.Do(func() {
		close(p.shutdown)

		if wait {
			// Wait for all tasks to be processed
			if len(p.taskQueue) > 0 {
				p.logger.Info().Int("remaining_tasks", len(p.taskQueue)).Msg("Waiting for tasks to complete")
			}

			// Close the task queue to signal no more tasks
			close(p.taskQueue)

			// Wait for all workers to finish
			p.wg.Wait()
		}
	})
}

// GetStats returns statistics about the pool
func (p *GoroutinePool) GetStats() map[string]interface{} {
	return map[string]interface{}{
		"name":            p.name,
		"worker_count":    atomic.LoadInt64(&p.workerCount),
		"max_workers":     p.maxWorkers,
		"tasks_processed": atomic.LoadInt64(&p.taskCount),
		"queue_length":    len(p.taskQueue),
		"queue_capacity":  cap(p.taskQueue),
	}
}

// Global pools for different audio processing tasks
var (
	globalAudioProcessorPool     atomic.Pointer[GoroutinePool]
	globalAudioReaderPool        atomic.Pointer[GoroutinePool]
	globalAudioProcessorInitOnce sync.Once
	globalAudioReaderInitOnce    sync.Once
)

// GetAudioProcessorPool returns the global audio processor pool
func GetAudioProcessorPool() *GoroutinePool {
	pool := globalAudioProcessorPool.Load()
	if pool != nil {
		return pool
	}

	globalAudioProcessorInitOnce.Do(func() {
		config := GetConfig()
		newPool := NewGoroutinePool(
			"audio-processor",
			config.MaxAudioProcessorWorkers,
			config.AudioProcessorQueueSize,
			config.WorkerMaxIdleTime,
		)
		globalAudioProcessorPool.Store(newPool)
		pool = newPool
	})

	return globalAudioProcessorPool.Load()
}

// GetAudioReaderPool returns the global audio reader pool
func GetAudioReaderPool() *GoroutinePool {
	pool := globalAudioReaderPool.Load()
	if pool != nil {
		return pool
	}

	globalAudioReaderInitOnce.Do(func() {
		config := GetConfig()
		newPool := NewGoroutinePool(
			"audio-reader",
			config.MaxAudioReaderWorkers,
			config.AudioReaderQueueSize,
			config.WorkerMaxIdleTime,
		)
		globalAudioReaderPool.Store(newPool)
		pool = newPool
	})

	return globalAudioReaderPool.Load()
}

// SubmitAudioProcessorTask submits a task to the audio processor pool
func SubmitAudioProcessorTask(task Task) bool {
	return GetAudioProcessorPool().Submit(task)
}

// SubmitAudioReaderTask submits a task to the audio reader pool
func SubmitAudioReaderTask(task Task) bool {
	return GetAudioReaderPool().Submit(task)
}

// ShutdownAudioPools shuts down all audio goroutine pools
func ShutdownAudioPools(wait bool) {
	logger := logging.GetDefaultLogger().With().Str("component", "audio-pools").Logger()

	processorPool := globalAudioProcessorPool.Load()
	if processorPool != nil {
		logger.Info().Msg("Shutting down audio processor pool")
		processorPool.Shutdown(wait)
	}

	readerPool := globalAudioReaderPool.Load()
	if readerPool != nil {
		logger.Info().Msg("Shutting down audio reader pool")
		readerPool.Shutdown(wait)
	}
}
