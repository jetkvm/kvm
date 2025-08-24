package audio

import (
	"sync/atomic"
	"time"
	"unsafe"
)

// MicrophoneContentionManager manages microphone access with cooldown periods
type MicrophoneContentionManager struct {
	// Atomic fields MUST be first for ARM32 alignment (int64 fields need 8-byte alignment)
	lastOpNano    int64
	cooldownNanos int64
	operationID   int64
	
	lockPtr       unsafe.Pointer
}

func NewMicrophoneContentionManager(cooldown time.Duration) *MicrophoneContentionManager {
	return &MicrophoneContentionManager{
		cooldownNanos: int64(cooldown),
	}
}

type OperationResult struct {
	Allowed           bool
	RemainingCooldown time.Duration
	OperationID       int64
}

func (mcm *MicrophoneContentionManager) TryOperation() OperationResult {
	now := time.Now().UnixNano()
	cooldown := atomic.LoadInt64(&mcm.cooldownNanos)
	lastOp := atomic.LoadInt64(&mcm.lastOpNano)
	elapsed := now - lastOp

	if elapsed >= cooldown {
		if atomic.CompareAndSwapInt64(&mcm.lastOpNano, lastOp, now) {
			opID := atomic.AddInt64(&mcm.operationID, 1)
			return OperationResult{
				Allowed:           true,
				RemainingCooldown: 0,
				OperationID:       opID,
			}
		}
		// Retry once if CAS failed
		lastOp = atomic.LoadInt64(&mcm.lastOpNano)
		elapsed = now - lastOp
		if elapsed >= cooldown && atomic.CompareAndSwapInt64(&mcm.lastOpNano, lastOp, now) {
			opID := atomic.AddInt64(&mcm.operationID, 1)
			return OperationResult{
				Allowed:           true,
				RemainingCooldown: 0,
				OperationID:       opID,
			}
		}
	}

	remaining := time.Duration(cooldown - elapsed)
	if remaining < 0 {
		remaining = 0
	}

	return OperationResult{
		Allowed:           false,
		RemainingCooldown: remaining,
		OperationID:       atomic.LoadInt64(&mcm.operationID),
	}
}

func (mcm *MicrophoneContentionManager) SetCooldown(cooldown time.Duration) {
	atomic.StoreInt64(&mcm.cooldownNanos, int64(cooldown))
}

func (mcm *MicrophoneContentionManager) GetCooldown() time.Duration {
	return time.Duration(atomic.LoadInt64(&mcm.cooldownNanos))
}

func (mcm *MicrophoneContentionManager) GetLastOperationTime() time.Time {
	nanos := atomic.LoadInt64(&mcm.lastOpNano)
	if nanos == 0 {
		return time.Time{}
	}
	return time.Unix(0, nanos)
}

func (mcm *MicrophoneContentionManager) GetOperationCount() int64 {
	return atomic.LoadInt64(&mcm.operationID)
}

func (mcm *MicrophoneContentionManager) Reset() {
	atomic.StoreInt64(&mcm.lastOpNano, 0)
	atomic.StoreInt64(&mcm.operationID, 0)
}

var (
	globalMicContentionManager unsafe.Pointer
	micContentionInitialized   int32
)

func GetMicrophoneContentionManager() *MicrophoneContentionManager {
	ptr := atomic.LoadPointer(&globalMicContentionManager)
	if ptr != nil {
		return (*MicrophoneContentionManager)(ptr)
	}

	if atomic.CompareAndSwapInt32(&micContentionInitialized, 0, 1) {
		manager := NewMicrophoneContentionManager(200 * time.Millisecond)
		atomic.StorePointer(&globalMicContentionManager, unsafe.Pointer(manager))
		return manager
	}

	ptr = atomic.LoadPointer(&globalMicContentionManager)
	if ptr != nil {
		return (*MicrophoneContentionManager)(ptr)
	}

	return NewMicrophoneContentionManager(200 * time.Millisecond)
}

func TryMicrophoneOperation() OperationResult {
	return GetMicrophoneContentionManager().TryOperation()
}

func SetMicrophoneCooldown(cooldown time.Duration) {
	GetMicrophoneContentionManager().SetCooldown(cooldown)
}
