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

// getPriorityConstants returns priority levels from centralized config
func getPriorityConstants() (audioHigh, audioMedium, audioLow, normal int) {
	config := GetConfig()
	return config.AudioHighPriority, config.AudioMediumPriority, config.AudioLowPriority, config.NormalPriority
}

// getSchedulingPolicies returns scheduling policies from centralized config
func getSchedulingPolicies() (schedNormal, schedFIFO, schedRR int) {
	config := GetConfig()
	return config.SchedNormal, config.SchedFIFO, config.SchedRR
}

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
		schedNormal, _, _ := getSchedulingPolicies()
		if policy != schedNormal {
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
	if niceValue < GetConfig().MinNiceValue {
		niceValue = GetConfig().MinNiceValue
	}
	if niceValue > GetConfig().MaxNiceValue {
		niceValue = GetConfig().MaxNiceValue
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
	audioHigh, _, _, _ := getPriorityConstants()
	_, schedFIFO, _ := getSchedulingPolicies()
	return ps.SetThreadPriority(audioHigh, schedFIFO)
}

// SetAudioIOPriority sets medium priority for audio I/O threads
func (ps *PriorityScheduler) SetAudioIOPriority() error {
	_, audioMedium, _, _ := getPriorityConstants()
	_, schedFIFO, _ := getSchedulingPolicies()
	return ps.SetThreadPriority(audioMedium, schedFIFO)
}

// SetAudioBackgroundPriority sets low priority for background audio tasks
func (ps *PriorityScheduler) SetAudioBackgroundPriority() error {
	_, _, audioLow, _ := getPriorityConstants()
	_, schedFIFO, _ := getSchedulingPolicies()
	return ps.SetThreadPriority(audioLow, schedFIFO)
}

// ResetPriority resets thread to normal scheduling
func (ps *PriorityScheduler) ResetPriority() error {
	_, _, _, normal := getPriorityConstants()
	schedNormal, _, _ := getSchedulingPolicies()
	return ps.SetThreadPriority(normal, schedNormal)
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
