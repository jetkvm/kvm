package audio

import (
	"context"
	"sync"

	"github.com/jetkvm/kvm/internal/logging"
	"github.com/pion/webrtc/v4/pkg/media"
	"github.com/rs/zerolog"
)

// AudioRelay handles forwarding audio frames from the audio server subprocess
// to WebRTC without any CGO audio processing. This runs in the main process.
type AudioRelay struct {
	client  *AudioClient
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	logger  *zerolog.Logger
	running bool
	mutex   sync.RWMutex

	// WebRTC integration
	audioTrack AudioTrackWriter
	config     AudioConfig
	muted      bool

	// Statistics
	framesRelayed int64
	framesDropped int64
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
		ctx:    ctx,
		cancel: cancel,
		logger: &logger,
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
	client, err := NewAudioClient()
	if err != nil {
		return err
	}
	r.client = client
	r.audioTrack = audioTrack
	r.config = config

	// Start relay goroutine
	r.wg.Add(1)
	go r.relayLoop()

	r.running = true
	r.logger.Info().Msg("Audio relay started")
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
		r.client.Close()
		r.client = nil
	}

	r.running = false
	r.logger.Info().Msg("Audio relay stopped")
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
	r.mutex.RLock()
	defer r.mutex.RUnlock()
	return r.framesRelayed, r.framesDropped
}

// UpdateTrack updates the WebRTC audio track for the relay
func (r *AudioRelay) UpdateTrack(audioTrack AudioTrackWriter) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.audioTrack = audioTrack
}

// relayLoop is the main relay loop that forwards frames from subprocess to WebRTC
func (r *AudioRelay) relayLoop() {
	defer r.wg.Done()
	r.logger.Debug().Msg("Audio relay loop started")

	for {
		select {
		case <-r.ctx.Done():
			r.logger.Debug().Msg("Audio relay loop stopping")
			return
		default:
			// Receive frame from audio server subprocess
			frame, err := r.client.ReceiveFrame()
			if err != nil {
				r.logger.Error().Err(err).Msg("Failed to receive audio frame")
				r.incrementDropped()
				continue
			}

			// Forward frame to WebRTC
			if err := r.forwardToWebRTC(frame); err != nil {
				r.logger.Warn().Err(err).Msg("Failed to forward frame to WebRTC")
				r.incrementDropped()
			} else {
				r.incrementRelayed()
			}
		}
	}
}

// forwardToWebRTC forwards a frame to the WebRTC audio track
func (r *AudioRelay) forwardToWebRTC(frame []byte) error {
	r.mutex.RLock()
	audioTrack := r.audioTrack
	config := r.config
	muted := r.muted
	r.mutex.RUnlock()

	if audioTrack == nil {
		return nil // No audio track available
	}

	// Prepare sample data
	var sampleData []byte
	if muted {
		// Send silence when muted
		sampleData = make([]byte, len(frame))
	} else {
		sampleData = frame
	}

	// Write sample to WebRTC track
	return audioTrack.WriteSample(media.Sample{
		Data:     sampleData,
		Duration: config.FrameSize,
	})
}

// incrementRelayed atomically increments the relayed frames counter
func (r *AudioRelay) incrementRelayed() {
	r.mutex.Lock()
	r.framesRelayed++
	r.mutex.Unlock()
}

// incrementDropped atomically increments the dropped frames counter
func (r *AudioRelay) incrementDropped() {
	r.mutex.Lock()
	r.framesDropped++
	r.mutex.Unlock()
}
