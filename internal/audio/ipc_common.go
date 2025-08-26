package audio

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// Common IPC message interface
type IPCMessage interface {
	GetMagic() uint32
	GetType() uint8
	GetLength() uint32
	GetTimestamp() int64
	GetData() []byte
}

// Common optimized message structure
type OptimizedMessage struct {
	header [17]byte // Pre-allocated header buffer
	data   []byte   // Reusable data buffer
}

// Generic message pool for both input and output
type GenericMessagePool struct {
	// 64-bit fields must be first for proper alignment on ARM
	hitCount  int64 // Pool hit counter (atomic)
	missCount int64 // Pool miss counter (atomic)

	pool         chan *OptimizedMessage
	preallocated []*OptimizedMessage // Pre-allocated messages
	preallocSize int
	maxPoolSize  int
	mutex        sync.RWMutex
}

// NewGenericMessagePool creates a new generic message pool
func NewGenericMessagePool(size int) *GenericMessagePool {
	pool := &GenericMessagePool{
		pool:         make(chan *OptimizedMessage, size),
		preallocSize: size / 4, // 25% pre-allocated for immediate use
		maxPoolSize:  size,
	}

	// Pre-allocate some messages for immediate use
	pool.preallocated = make([]*OptimizedMessage, pool.preallocSize)
	for i := 0; i < pool.preallocSize; i++ {
		pool.preallocated[i] = &OptimizedMessage{
			data: make([]byte, 0, GetConfig().MaxFrameSize),
		}
	}

	// Fill the channel pool
	for i := 0; i < size-pool.preallocSize; i++ {
		select {
		case pool.pool <- &OptimizedMessage{
			data: make([]byte, 0, GetConfig().MaxFrameSize),
		}:
		default:
			break
		}
	}

	return pool
}

// Get retrieves an optimized message from the pool
func (mp *GenericMessagePool) Get() *OptimizedMessage {
	// Try pre-allocated first (fastest path)
	mp.mutex.Lock()
	if len(mp.preallocated) > 0 {
		msg := mp.preallocated[len(mp.preallocated)-1]
		mp.preallocated = mp.preallocated[:len(mp.preallocated)-1]
		mp.mutex.Unlock()
		atomic.AddInt64(&mp.hitCount, 1)
		return msg
	}
	mp.mutex.Unlock()

	// Try channel pool
	select {
	case msg := <-mp.pool:
		atomic.AddInt64(&mp.hitCount, 1)
		return msg
	default:
		// Pool empty, create new message
		atomic.AddInt64(&mp.missCount, 1)
		return &OptimizedMessage{
			data: make([]byte, 0, GetConfig().MaxFrameSize),
		}
	}
}

// Put returns an optimized message to the pool
func (mp *GenericMessagePool) Put(msg *OptimizedMessage) {
	if msg == nil {
		return
	}

	// Reset the message for reuse
	msg.data = msg.data[:0]

	// Try to return to pre-allocated slice first
	mp.mutex.Lock()
	if len(mp.preallocated) < mp.preallocSize {
		mp.preallocated = append(mp.preallocated, msg)
		mp.mutex.Unlock()
		return
	}
	mp.mutex.Unlock()

	// Try to return to channel pool
	select {
	case mp.pool <- msg:
		// Successfully returned to pool
	default:
		// Pool full, let GC handle it
	}
}

// GetStats returns pool statistics
func (mp *GenericMessagePool) GetStats() (hitCount, missCount int64, hitRate float64) {
	hits := atomic.LoadInt64(&mp.hitCount)
	misses := atomic.LoadInt64(&mp.missCount)
	total := hits + misses
	if total > 0 {
		hitRate = float64(hits) / float64(total) * 100
	}
	return hits, misses, hitRate
}

// Common write message function
func WriteIPCMessage(conn net.Conn, msg IPCMessage, pool *GenericMessagePool, droppedFramesCounter *int64) error {
	if conn == nil {
		return fmt.Errorf("connection is nil")
	}

	// Get optimized message from pool for header preparation
	optMsg := pool.Get()
	defer pool.Put(optMsg)

	// Prepare header in pre-allocated buffer
	binary.LittleEndian.PutUint32(optMsg.header[0:4], msg.GetMagic())
	optMsg.header[4] = msg.GetType()
	binary.LittleEndian.PutUint32(optMsg.header[5:9], msg.GetLength())
	binary.LittleEndian.PutUint64(optMsg.header[9:17], uint64(msg.GetTimestamp()))

	// Use non-blocking write with timeout
	ctx, cancel := context.WithTimeout(context.Background(), GetConfig().WriteTimeout)
	defer cancel()

	// Create a channel to signal write completion
	done := make(chan error, 1)
	go func() {
		// Write header using pre-allocated buffer
		_, err := conn.Write(optMsg.header[:])
		if err != nil {
			done <- err
			return
		}

		// Write data if present
		if msg.GetLength() > 0 && msg.GetData() != nil {
			_, err = conn.Write(msg.GetData())
			if err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()

	// Wait for completion or timeout
	select {
	case err := <-done:
		if err != nil {
			if droppedFramesCounter != nil {
				atomic.AddInt64(droppedFramesCounter, 1)
			}
			return err
		}
		return nil
	case <-ctx.Done():
		// Timeout occurred - drop frame to prevent blocking
		if droppedFramesCounter != nil {
			atomic.AddInt64(droppedFramesCounter, 1)
		}
		return fmt.Errorf("write timeout - frame dropped")
	}
}

// Common connection acceptance with retry logic
func AcceptConnectionWithRetry(listener net.Listener, maxRetries int, retryDelay time.Duration) (net.Conn, error) {
	var lastErr error
	for i := 0; i < maxRetries; i++ {
		conn, err := listener.Accept()
		if err == nil {
			return conn, nil
		}
		lastErr = err
		if i < maxRetries-1 {
			time.Sleep(retryDelay)
		}
	}
	return nil, fmt.Errorf("failed to accept connection after %d retries: %w", maxRetries, lastErr)
}

// Common frame statistics structure
type FrameStats struct {
	Total   int64
	Dropped int64
}

// GetFrameStats safely retrieves frame statistics
func GetFrameStats(totalCounter, droppedCounter *int64) FrameStats {
	return FrameStats{
		Total:   atomic.LoadInt64(totalCounter),
		Dropped: atomic.LoadInt64(droppedCounter),
	}
}

// CalculateDropRate calculates the drop rate percentage
func CalculateDropRate(stats FrameStats) float64 {
	if stats.Total == 0 {
		return 0.0
	}
	return float64(stats.Dropped) / float64(stats.Total) * 100.0
}

// ResetFrameStats resets frame counters
func ResetFrameStats(totalCounter, droppedCounter *int64) {
	atomic.StoreInt64(totalCounter, 0)
	atomic.StoreInt64(droppedCounter, 0)
}
