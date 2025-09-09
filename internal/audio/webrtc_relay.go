package audio

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jetkvm/kvm/internal/logging"
	"github.com/pion/webrtc/v4/pkg/media"
	"github.com/rs/zerolog"
)

// AudioRelay handles forwarding audio frames from the audio server subprocess
// to WebRTC without any CGO audio processing. This runs in the main process.
type AudioRelay struct {
	// Atomic fields MUST be first for ARM32 alignment (int64 fields need 8-byte alignment)
	framesRelayed int64
	framesDropped int64

	client     *AudioOutputClient
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	logger     *zerolog.Logger
	running    bool
	mutex      sync.RWMutex
	bufferPool *AudioBufferPool // Buffer pool for memory optimization

	// WebRTC integration
	audioTrack AudioTrackWriter
	config     AudioConfig
	muted      bool
}

// AudioTrackWriter interface for WebRTC audio track
type AudioTrackWriter interface {
	WriteSample(sample media.Sample) error
}

// NewAudioRelay creates a new audio relay for the main process
func NewAudioRelay() *AudioRelay {
	ctx, cancel := context.WithCancel(context.Background())
	logger := logging.GetDefaultLogger().With().Str("component", "audio-relay").Logger()

	return &AudioRelay{
		ctx:        ctx,
		cancel:     cancel,
		logger:     &logger,
		bufferPool: NewAudioBufferPool(GetMaxAudioFrameSize()),
	}
}

// Start begins the audio relay process
func (r *AudioRelay) Start(audioTrack AudioTrackWriter, config AudioConfig) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	if r.running {
		return nil // Already running
	}

	// Create audio client to connect to subprocess
	client := NewAudioOutputClient()
	r.client = client
	r.audioTrack = audioTrack
	r.config = config

	// Connect to the audio output server
	if err := client.Connect(); err != nil {
		return fmt.Errorf("failed to connect to audio output server: %w", err)
	}

	// Start relay goroutine
	r.wg.Add(1)
	go r.relayLoop()

	r.running = true
	r.logger.Info().Msg("Audio relay connected to output server")
	return nil
}

// Stop stops the audio relay
func (r *AudioRelay) Stop() {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	if !r.running {
		return
	}

	r.cancel()
	r.wg.Wait()

	if r.client != nil {
		r.client.Disconnect()
		r.client = nil
	}

	r.running = false
	r.logger.Info().Msgf("Audio relay stopped after relaying %d frames", r.framesRelayed)
}

// SetMuted sets the mute state
func (r *AudioRelay) SetMuted(muted bool) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.muted = muted
}

// IsMuted returns the current mute state (checks both relay and global mute)
func (r *AudioRelay) IsMuted() bool {
	r.mutex.RLock()
	defer r.mutex.RUnlock()
	return r.muted || IsAudioMuted()
}

// GetStats returns relay statistics
func (r *AudioRelay) GetStats() (framesRelayed, framesDropped int64) {
	return atomic.LoadInt64(&r.framesRelayed), atomic.LoadInt64(&r.framesDropped)
}

// UpdateTrack updates the WebRTC audio track for the relay
func (r *AudioRelay) UpdateTrack(audioTrack AudioTrackWriter) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.audioTrack = audioTrack
}

func (r *AudioRelay) relayLoop() {
	defer r.wg.Done()

	var maxConsecutiveErrors = Config.MaxConsecutiveErrors
	consecutiveErrors := 0
	backoffDelay := time.Millisecond * 10
	maxBackoff := time.Second * 5

	for {
		select {
		case <-r.ctx.Done():
			return
		default:
			frame, err := r.client.ReceiveFrame()
			if err != nil {
				consecutiveErrors++
				r.incrementDropped()

				// Exponential backoff for stability
				if consecutiveErrors >= maxConsecutiveErrors {
					// Attempt reconnection
					if r.attemptReconnection() {
						consecutiveErrors = 0
						backoffDelay = time.Millisecond * 10
						continue
					}
					return
				}

				time.Sleep(backoffDelay)
				if backoffDelay < maxBackoff {
					backoffDelay *= 2
				}
				continue
			}

			consecutiveErrors = 0
			backoffDelay = time.Millisecond * 10
			if err := r.forwardToWebRTC(frame); err != nil {
				r.incrementDropped()
			} else {
				r.incrementRelayed()
			}
		}
	}
}

// forwardToWebRTC forwards a frame to the WebRTC audio track
func (r *AudioRelay) forwardToWebRTC(frame []byte) error {
	// Use ultra-fast validation for critical audio path
	if err := ValidateAudioFrame(frame); err != nil {
		r.incrementDropped()
		r.logger.Debug().Err(err).Msg("invalid frame data in relay")
		return err
	}

	r.mutex.RLock()
	defer r.mutex.RUnlock()

	audioTrack := r.audioTrack
	config := r.config
	muted := r.muted

	// Comprehensive nil check for audioTrack to prevent panic
	if audioTrack == nil {
		return nil // No audio track available
	}

	// Check if interface contains nil pointer using reflection
	if reflect.ValueOf(audioTrack).IsNil() {
		return nil // Audio track interface contains nil pointer
	}

	// Prepare sample data
	var sampleData []byte
	if muted {
		// Send silence when muted - use buffer pool to avoid allocation
		sampleData = r.bufferPool.Get()
		sampleData = sampleData[:len(frame)] // Resize to frame length
		// Clear the buffer to create silence
		for i := range sampleData {
			sampleData[i] = 0
		}
		defer r.bufferPool.Put(sampleData) // Return to pool after use
	} else {
		sampleData = frame
	}

	// Write sample to WebRTC track while holding the read lock
	return audioTrack.WriteSample(media.Sample{
		Data:     sampleData,
		Duration: config.FrameSize,
	})
}

// incrementRelayed atomically increments the relayed frames counter
func (r *AudioRelay) incrementRelayed() {
	atomic.AddInt64(&r.framesRelayed, 1)
}

// incrementDropped atomically increments the dropped frames counter
func (r *AudioRelay) incrementDropped() {
	atomic.AddInt64(&r.framesDropped, 1)
}

// attemptReconnection tries to reconnect the audio client for stability
func (r *AudioRelay) attemptReconnection() bool {
	if r.client == nil {
		return false
	}

	// Disconnect and reconnect
	r.client.Disconnect()
	time.Sleep(time.Millisecond * 100)

	err := r.client.Connect()
	return err == nil
}
