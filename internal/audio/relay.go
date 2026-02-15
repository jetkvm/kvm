package audio

import (
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/jetkvm/kvm/internal/logging"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
	"github.com/rs/zerolog"
)

// PCMCallback is called with raw PCM audio data (16-bit stereo 48kHz).
// This is used for RDP audio output which requires raw PCM.
type PCMCallback func(pcm []byte)

// PCMEnabledCheck returns true if PCM callback should be invoked.
// Used to skip GetLastPCM CGO call when no RDP audio subscribers.
type PCMEnabledCheck func() bool

type OutputRelay struct {
	source     *AudioSource
	audioTrack atomic.Pointer[webrtc.TrackLocalStaticSample]
	logger     *zerolog.Logger
	running    atomic.Bool
	sample     media.Sample
	stopped    chan struct{}

	// Callback for raw PCM data (for RDP audio).
	// Must be set before Start(); read without synchronization by relayLoop.
	pcmCallback     PCMCallback
	pcmEnabledCheck PCMEnabledCheck // Check before calling GetLastPCM

	framesRelayed atomic.Uint32
	framesDropped atomic.Uint32
}

func NewOutputRelay(source *AudioSource, audioTrack *webrtc.TrackLocalStaticSample) *OutputRelay {
	logger := logging.GetSubsystemLogger("audio-output")

	r := &OutputRelay{
		source:  source,
		logger:  logger,
		stopped: make(chan struct{}),
		sample: media.Sample{
			Duration: 20 * time.Millisecond,
		},
	}
	if audioTrack != nil {
		r.audioTrack.Store(audioTrack)
	}
	return r
}

// SetAudioTrack updates the WebRTC audio track dynamically.
// This allows the track to be set after the relay is created,
// enabling RDP and WebRTC to connect in any order.
func (r *OutputRelay) SetAudioTrack(track *webrtc.TrackLocalStaticSample) {
	r.audioTrack.Store(track)
}

// SetPCMCallback sets a callback to receive raw PCM audio data.
// This is called for each audio frame with 16-bit stereo 48kHz PCM.
// Must be called before Start(); not safe for concurrent use with relayLoop.
func (r *OutputRelay) SetPCMCallback(cb PCMCallback) {
	r.pcmCallback = cb
}

// SetPCMEnabledCheck sets a function to check if PCM callback should be called.
// This avoids the GetLastPCM CGO overhead when no RDP audio subscribers exist.
// Must be called before Start(); not safe for concurrent use with relayLoop.
func (r *OutputRelay) SetPCMEnabledCheck(check PCMEnabledCheck) {
	r.pcmEnabledCheck = check
}

// IsRunning returns true if the relay goroutine is actively running.
// This can be false even if the relay object exists, if the goroutine
// exited due to errors.
func (r *OutputRelay) IsRunning() bool {
	return r.running.Load()
}

func (r *OutputRelay) Start() error {
	if r.running.Swap(true) {
		return fmt.Errorf("output relay already running")
	}

	// Create new stopped channel for this run (allows restart after previous stop)
	r.stopped = make(chan struct{})

	go r.relayLoop()
	r.logger.Debug().Msg("output relay started")
	return nil
}

func (r *OutputRelay) Stop() {
	if !r.running.Swap(false) {
		return
	}

	<-r.stopped

	r.logger.Debug().
		Uint32("frames_relayed", r.framesRelayed.Load()).
		Uint32("frames_dropped", r.framesDropped.Load()).
		Msg("output relay stopped")
}

func (r *OutputRelay) relayLoop() {
	defer func() {
		// Mark as not running when loop exits for any reason
		// This allows the relay to be restarted if needed
		r.running.Store(false)
		close(r.stopped)
	}()

	const maxConsecutiveWriteFailures = 50 // Allow some WebRTC write failures before reconnecting
	retryDelay := 1 * time.Second
	consecutiveFailures := 0
	consecutiveWriteFailures := 0

	for r.running.Load() {
		if !(*r.source).IsConnected() {
			if err := (*r.source).Connect(); err != nil {
				consecutiveFailures++
				// Log first few failures at WARN, then at DEBUG to avoid flooding
				if consecutiveFailures <= 3 {
					r.logger.Warn().Err(err).Int("failures", consecutiveFailures).Msg("Connection failed, retrying")
				} else if consecutiveFailures%10 == 0 {
					r.logger.Warn().Err(err).Int("failures", consecutiveFailures).Msg("Connection still failing")
				}
				time.Sleep(retryDelay)
				retryDelay = min(retryDelay*2, 30*time.Second)
				continue
			}
			if consecutiveFailures > 0 {
				r.logger.Info().Int("previous_failures", consecutiveFailures).Msg("Audio reconnected after failures")
			}
			consecutiveFailures = 0
			retryDelay = 1 * time.Second
		}

		msgType, payload, err := (*r.source).ReadMessage()
		if err != nil {
			if !r.running.Load() {
				break
			}

			// Sample rate changes are expected when the source PC switches audio
			// formats (e.g., 48kHz video → 44.1kHz music). Reconnect immediately
			// without backoff — the C layer's cooldown prevents oscillation.
			if errors.Is(err, ErrSampleRateChanged) {
				(*r.source).Disconnect()
				continue
			}

			consecutiveFailures++
			// Log first few failures at WARN, then only every 10th to avoid flooding
			if consecutiveFailures <= 3 {
				r.logger.Warn().Err(err).Int("failures", consecutiveFailures).Msg("Read error, reconnecting")
			} else if consecutiveFailures%10 == 0 {
				r.logger.Warn().Err(err).Int("failures", consecutiveFailures).Msg("Read errors continuing")
			}
			(*r.source).Disconnect()
			time.Sleep(retryDelay)
			retryDelay = min(retryDelay*2, 30*time.Second)
			continue
		}

		consecutiveFailures = 0
		retryDelay = 1 * time.Second

		// Call PCM callback for RDP audio output (if enabled and has subscribers)
		if r.pcmCallback != nil && (r.pcmEnabledCheck == nil || r.pcmEnabledCheck()) {
			if pcm := GetLastPCM(); pcm != nil {
				r.pcmCallback(pcm)
				ReleasePCMBuffer(pcm)
			}
		}

		if msgType == ipcMsgTypeOpus && len(payload) > 0 {
			// Write to WebRTC track if available (load atomically for thread-safety)
			if track := r.audioTrack.Load(); track != nil {
				r.sample.Data = payload
				if err := track.WriteSample(r.sample); err != nil {
					r.framesDropped.Add(1)
					consecutiveWriteFailures++

					// Log warning on first failure and every 10th failure
					if consecutiveWriteFailures == 1 || consecutiveWriteFailures%10 == 0 {
						r.logger.Warn().
							Err(err).
							Int("consecutive_failures", consecutiveWriteFailures).
							Msg("Failed to write sample to WebRTC")
					}

					if consecutiveWriteFailures >= maxConsecutiveWriteFailures {
						r.logger.Warn().
							Int("failures", consecutiveWriteFailures).
							Msg("Too many consecutive WebRTC write failures, clearing track")
						// Don't disconnect the audio source - that would break RDP audio too.
						// Just clear the WebRTC track so we stop trying to write to it.
						// WebRTC reconnection will set a new track when ready.
						r.audioTrack.Store(nil)
						consecutiveWriteFailures = 0
					}
				} else {
					r.framesRelayed.Add(1)
					consecutiveWriteFailures = 0
				}
			} else {
				// No WebRTC track - just count frames for RDP-only mode
				r.framesRelayed.Add(1)
			}
		}
	}
}

type InputRelay struct {
	logger  *zerolog.Logger
	running atomic.Bool
}

func NewInputRelay() *InputRelay {
	logger := logging.GetSubsystemLogger("audio-input")

	return &InputRelay{
		logger: logger,
	}
}

func (r *InputRelay) Start() error {
	if r.running.Swap(true) {
		return fmt.Errorf("input relay already running")
	}

	r.logger.Debug().Msg("input relay started")
	return nil
}

func (r *InputRelay) Stop() {
	if !r.running.Swap(false) {
		return
	}

	r.logger.Debug().Msg("input relay stopped")
}
