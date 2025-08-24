//go:build linux

package audio

import (
	"runtime"
	"syscall"
	"unsafe"

	"github.com/jetkvm/kvm/internal/logging"
	"github.com/rs/zerolog"
)

// SchedParam represents scheduling parameters for Linux
type SchedParam struct {
	Priority int32
}

// Priority levels for audio processing
const (
	// SCHED_FIFO priorities (1-99, higher = more priority)
	AudioHighPriority   = 80 // High priority for critical audio processing
	AudioMediumPriority = 60 // Medium priority for regular audio processing
	AudioLowPriority    = 40 // Low priority for background audio tasks

	// SCHED_NORMAL is the default (priority 0)
	NormalPriority = 0
)

// Scheduling policies
const (
	SCHED_NORMAL = 0
	SCHED_FIFO   = 1
	SCHED_RR     = 2
)

// PriorityScheduler manages thread priorities for audio processing
type PriorityScheduler struct {
	logger  zerolog.Logger
	enabled bool
}

// NewPriorityScheduler creates a new priority scheduler
func NewPriorityScheduler() *PriorityScheduler {
	return &PriorityScheduler{
		logger:  logging.GetDefaultLogger().With().Str("component", "priority-scheduler").Logger(),
		enabled: true,
	}
}

// SetThreadPriority sets the priority of the current thread
func (ps *PriorityScheduler) SetThreadPriority(priority int, policy int) error {
	if !ps.enabled {
		return nil
	}

	// Lock to OS thread to ensure we're setting priority for the right thread
	runtime.LockOSThread()

	// Get current thread ID
	tid := syscall.Gettid()

	// Set scheduling parameters
	param := &SchedParam{
		Priority: int32(priority),
	}

	// Use syscall to set scheduler
	_, _, errno := syscall.Syscall(syscall.SYS_SCHED_SETSCHEDULER,
		uintptr(tid),
		uintptr(policy),
		uintptr(unsafe.Pointer(param)))

	if errno != 0 {
		// If we can't set real-time priority, try nice value instead
		if policy != SCHED_NORMAL {
			ps.logger.Warn().Int("errno", int(errno)).Msg("Failed to set real-time priority, falling back to nice")
			return ps.setNicePriority(priority)
		}
		return errno
	}

	ps.logger.Debug().Int("tid", tid).Int("priority", priority).Int("policy", policy).Msg("Thread priority set")
	return nil
}

// setNicePriority sets nice value as fallback when real-time scheduling is not available
func (ps *PriorityScheduler) setNicePriority(rtPriority int) error {
	// Convert real-time priority to nice value (inverse relationship)
	// RT priority 80 -> nice -10, RT priority 40 -> nice 0
	niceValue := (40 - rtPriority) / 4
	if niceValue < -20 {
		niceValue = -20
	}
	if niceValue > 19 {
		niceValue = 19
	}

	err := syscall.Setpriority(syscall.PRIO_PROCESS, 0, niceValue)
	if err != nil {
		ps.logger.Warn().Err(err).Int("nice", niceValue).Msg("Failed to set nice priority")
		return err
	}

	ps.logger.Debug().Int("nice", niceValue).Msg("Nice priority set as fallback")
	return nil
}

// SetAudioProcessingPriority sets high priority for audio processing threads
func (ps *PriorityScheduler) SetAudioProcessingPriority() error {
	return ps.SetThreadPriority(AudioHighPriority, SCHED_FIFO)
}

// SetAudioIOPriority sets medium priority for audio I/O threads
func (ps *PriorityScheduler) SetAudioIOPriority() error {
	return ps.SetThreadPriority(AudioMediumPriority, SCHED_FIFO)
}

// SetAudioBackgroundPriority sets low priority for background audio tasks
func (ps *PriorityScheduler) SetAudioBackgroundPriority() error {
	return ps.SetThreadPriority(AudioLowPriority, SCHED_FIFO)
}

// ResetPriority resets thread to normal scheduling
func (ps *PriorityScheduler) ResetPriority() error {
	return ps.SetThreadPriority(NormalPriority, SCHED_NORMAL)
}

// Disable disables priority scheduling (useful for testing or fallback)
func (ps *PriorityScheduler) Disable() {
	ps.enabled = false
	ps.logger.Info().Msg("Priority scheduling disabled")
}

// Enable enables priority scheduling
func (ps *PriorityScheduler) Enable() {
	ps.enabled = true
	ps.logger.Info().Msg("Priority scheduling enabled")
}

// Global priority scheduler instance
var globalPriorityScheduler *PriorityScheduler

// GetPriorityScheduler returns the global priority scheduler instance
func GetPriorityScheduler() *PriorityScheduler {
	if globalPriorityScheduler == nil {
		globalPriorityScheduler = NewPriorityScheduler()
	}
	return globalPriorityScheduler
}

// SetAudioThreadPriority is a convenience function to set audio processing priority
func SetAudioThreadPriority() error {
	return GetPriorityScheduler().SetAudioProcessingPriority()
}

// SetAudioIOThreadPriority is a convenience function to set audio I/O priority
func SetAudioIOThreadPriority() error {
	return GetPriorityScheduler().SetAudioIOPriority()
}

// ResetThreadPriority is a convenience function to reset thread priority
func ResetThreadPriority() error {
	return GetPriorityScheduler().ResetPriority()
}
