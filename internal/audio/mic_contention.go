package audio

import (
	"sync/atomic"
	"time"
	"unsafe"
)

// MicrophoneContentionManager provides optimized microphone operation locking
// with reduced contention using atomic operations and conditional locking
type MicrophoneContentionManager struct {
	// Atomic fields (must be 64-bit aligned on 32-bit systems)
	lastOpNano    int64 // Unix nanoseconds of last operation
	cooldownNanos int64 // Cooldown duration in nanoseconds
	operationID   int64 // Incremental operation ID for tracking

	// Lock-free state flags (using atomic.Pointer for lock-free updates)
	lockPtr unsafe.Pointer // *sync.Mutex - conditionally allocated
}

// NewMicrophoneContentionManager creates a new microphone contention manager
func NewMicrophoneContentionManager(cooldown time.Duration) *MicrophoneContentionManager {
	return &MicrophoneContentionManager{
		cooldownNanos: int64(cooldown),
	}
}

// OperationResult represents the result of attempting a microphone operation
type OperationResult struct {
	Allowed           bool
	RemainingCooldown time.Duration
	OperationID       int64
}

// TryOperation attempts to perform a microphone operation with optimized contention handling
func (mcm *MicrophoneContentionManager) TryOperation() OperationResult {
	now := time.Now().UnixNano()
	cooldown := atomic.LoadInt64(&mcm.cooldownNanos)

	// Fast path: check if we're clearly outside cooldown period using atomic read
	lastOp := atomic.LoadInt64(&mcm.lastOpNano)
	elapsed := now - lastOp

	if elapsed >= cooldown {
		// Attempt atomic update without locking
		if atomic.CompareAndSwapInt64(&mcm.lastOpNano, lastOp, now) {
			opID := atomic.AddInt64(&mcm.operationID, 1)
			return OperationResult{
				Allowed:           true,
				RemainingCooldown: 0,
				OperationID:       opID,
			}
		}
	}

	// Slow path: potential contention, check remaining cooldown
	currentLastOp := atomic.LoadInt64(&mcm.lastOpNano)
	currentElapsed := now - currentLastOp

	if currentElapsed >= cooldown {
		// Race condition: another operation might have updated lastOpNano
		// Try once more with CAS
		if atomic.CompareAndSwapInt64(&mcm.lastOpNano, currentLastOp, now) {
			opID := atomic.AddInt64(&mcm.operationID, 1)
			return OperationResult{
				Allowed:           true,
				RemainingCooldown: 0,
				OperationID:       opID,
			}
		}
		// If CAS failed, fall through to cooldown calculation
		currentLastOp = atomic.LoadInt64(&mcm.lastOpNano)
		currentElapsed = now - currentLastOp
	}

	remaining := time.Duration(cooldown - currentElapsed)
	if remaining < 0 {
		remaining = 0
	}

	return OperationResult{
		Allowed:           false,
		RemainingCooldown: remaining,
		OperationID:       atomic.LoadInt64(&mcm.operationID),
	}
}

// SetCooldown updates the cooldown duration atomically
func (mcm *MicrophoneContentionManager) SetCooldown(cooldown time.Duration) {
	atomic.StoreInt64(&mcm.cooldownNanos, int64(cooldown))
}

// GetCooldown returns the current cooldown duration
func (mcm *MicrophoneContentionManager) GetCooldown() time.Duration {
	return time.Duration(atomic.LoadInt64(&mcm.cooldownNanos))
}

// GetLastOperationTime returns the time of the last operation
func (mcm *MicrophoneContentionManager) GetLastOperationTime() time.Time {
	nanos := atomic.LoadInt64(&mcm.lastOpNano)
	if nanos == 0 {
		return time.Time{}
	}
	return time.Unix(0, nanos)
}

// GetOperationCount returns the total number of successful operations
func (mcm *MicrophoneContentionManager) GetOperationCount() int64 {
	return atomic.LoadInt64(&mcm.operationID)
}

// Reset resets the contention manager state
func (mcm *MicrophoneContentionManager) Reset() {
	atomic.StoreInt64(&mcm.lastOpNano, 0)
	atomic.StoreInt64(&mcm.operationID, 0)
}

// Global instance for microphone contention management
var (
	globalMicContentionManager unsafe.Pointer // *MicrophoneContentionManager
	micContentionInitialized   int32
)

// GetMicrophoneContentionManager returns the global microphone contention manager
func GetMicrophoneContentionManager() *MicrophoneContentionManager {
	ptr := atomic.LoadPointer(&globalMicContentionManager)
	if ptr != nil {
		return (*MicrophoneContentionManager)(ptr)
	}

	// Initialize on first use
	if atomic.CompareAndSwapInt32(&micContentionInitialized, 0, 1) {
		manager := NewMicrophoneContentionManager(200 * time.Millisecond)
		atomic.StorePointer(&globalMicContentionManager, unsafe.Pointer(manager))
		return manager
	}

	// Another goroutine initialized it, try again
	ptr = atomic.LoadPointer(&globalMicContentionManager)
	if ptr != nil {
		return (*MicrophoneContentionManager)(ptr)
	}

	// Fallback: create a new manager (should rarely happen)
	return NewMicrophoneContentionManager(200 * time.Millisecond)
}

// TryMicrophoneOperation provides a convenient global function for microphone operations
func TryMicrophoneOperation() OperationResult {
	manager := GetMicrophoneContentionManager()
	return manager.TryOperation()
}

// SetMicrophoneCooldown updates the global microphone cooldown
func SetMicrophoneCooldown(cooldown time.Duration) {
	manager := GetMicrophoneContentionManager()
	manager.SetCooldown(cooldown)
}
