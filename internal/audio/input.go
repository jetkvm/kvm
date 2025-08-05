package audio

import (
	"sync/atomic"
	"time"

	"github.com/jetkvm/kvm/internal/logging"
	"github.com/rs/zerolog"
)

// AudioInputMetrics holds metrics for microphone input
// Note: int64 fields must be 64-bit aligned for atomic operations on ARM
type AudioInputMetrics struct {
	FramesSent      int64 // Must be first for alignment
	FramesDropped   int64
	BytesProcessed  int64
	ConnectionDrops int64
	AverageLatency  time.Duration // time.Duration is int64
	LastFrameTime   time.Time
}

// AudioInputManager manages microphone input stream from WebRTC to USB gadget
type AudioInputManager struct {
	// metrics MUST be first for ARM32 alignment (contains int64 fields)
	metrics AudioInputMetrics

	inputBuffer chan []byte
	logger      zerolog.Logger
	running     int32
}

// NewAudioInputManager creates a new audio input manager
func NewAudioInputManager() *AudioInputManager {
	return &AudioInputManager{
		inputBuffer: make(chan []byte, 100), // Buffer up to 100 frames
		logger:      logging.GetDefaultLogger().With().Str("component", "audio-input").Logger(),
	}
}

// Start begins processing microphone input
func (aim *AudioInputManager) Start() error {
	if !atomic.CompareAndSwapInt32(&aim.running, 0, 1) {
		return nil // Already running
	}

	aim.logger.Info().Msg("Starting audio input manager")

	// Start the non-blocking audio input stream
	err := StartNonBlockingAudioInput(aim.inputBuffer)
	if err != nil {
		atomic.StoreInt32(&aim.running, 0)
		return err
	}

	return nil
}

// Stop stops processing microphone input
func (aim *AudioInputManager) Stop() {
	if !atomic.CompareAndSwapInt32(&aim.running, 1, 0) {
		return // Already stopped
	}

	aim.logger.Info().Msg("Stopping audio input manager")

	// Stop the non-blocking audio input stream
	StopNonBlockingAudioInput()

	// Drain the input buffer
	go func() {
		for {
			select {
			case <-aim.inputBuffer:
				// Drain
			case <-time.After(100 * time.Millisecond):
				return
			}
		}
	}()

	aim.logger.Info().Msg("Audio input manager stopped")
}

// WriteOpusFrame writes an Opus frame to the input buffer
func (aim *AudioInputManager) WriteOpusFrame(frame []byte) error {
	if atomic.LoadInt32(&aim.running) == 0 {
		return nil // Not running, ignore
	}

	select {
	case aim.inputBuffer <- frame:
		atomic.AddInt64(&aim.metrics.FramesSent, 1)
		atomic.AddInt64(&aim.metrics.BytesProcessed, int64(len(frame)))
		aim.metrics.LastFrameTime = time.Now()
		return nil
	default:
		// Buffer full, drop frame
		atomic.AddInt64(&aim.metrics.FramesDropped, 1)
		aim.logger.Warn().Msg("Audio input buffer full, dropping frame")
		return nil
	}
}

// GetMetrics returns current microphone input metrics
func (aim *AudioInputManager) GetMetrics() AudioInputMetrics {
	return AudioInputMetrics{
		FramesSent:      atomic.LoadInt64(&aim.metrics.FramesSent),
		FramesDropped:   atomic.LoadInt64(&aim.metrics.FramesDropped),
		BytesProcessed:  atomic.LoadInt64(&aim.metrics.BytesProcessed),
		LastFrameTime:   aim.metrics.LastFrameTime,
		ConnectionDrops: atomic.LoadInt64(&aim.metrics.ConnectionDrops),
		AverageLatency:  aim.metrics.AverageLatency,
	}
}

// IsRunning returns whether the audio input manager is running
func (aim *AudioInputManager) IsRunning() bool {
	return atomic.LoadInt32(&aim.running) == 1
}
