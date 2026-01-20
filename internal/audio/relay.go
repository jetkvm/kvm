package audio

import (
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

type OutputRelay struct {
	source     *AudioSource
	audioTrack *webrtc.TrackLocalStaticSample
	logger     zerolog.Logger
	running    atomic.Bool
	sample     media.Sample
	stopped    chan struct{}

	// Callback for raw PCM data (for RDP audio)
	pcmCallback PCMCallback

	framesRelayed atomic.Uint32
	framesDropped atomic.Uint32
}

func NewOutputRelay(source *AudioSource, audioTrack *webrtc.TrackLocalStaticSample) *OutputRelay {
	logger := logging.GetDefaultLogger().With().Str("component", "audio-output-relay").Logger()

	return &OutputRelay{
		source:     source,
		audioTrack: audioTrack,
		logger:     logger,
		stopped:    make(chan struct{}),
		sample: media.Sample{
			Duration: 20 * time.Millisecond,
		},
	}
}

// SetPCMCallback sets a callback to receive raw PCM audio data.
// This is called for each audio frame with 16-bit stereo 48kHz PCM.
func (r *OutputRelay) SetPCMCallback(cb PCMCallback) {
	r.pcmCallback = cb
}

func (r *OutputRelay) Start() error {
	if r.running.Swap(true) {
		return fmt.Errorf("output relay already running")
	}

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
	defer close(r.stopped)

	const maxRetries = 10
	const maxConsecutiveWriteFailures = 50 // Allow some WebRTC write failures before reconnecting
	retryDelay := 1 * time.Second
	consecutiveFailures := 0
	consecutiveWriteFailures := 0

	for r.running.Load() {
		if !(*r.source).IsConnected() {
			if err := (*r.source).Connect(); err != nil {
				if consecutiveFailures++; consecutiveFailures >= maxRetries {
					r.logger.Error().Int("failures", consecutiveFailures).Msg("Max retries exceeded, stopping relay")
					return
				}
				r.logger.Debug().Err(err).Int("failures", consecutiveFailures).Msg("Connection failed, retrying")
				time.Sleep(retryDelay)
				retryDelay = min(retryDelay*2, 30*time.Second)
				continue
			}
			consecutiveFailures = 0
			retryDelay = 1 * time.Second
		}

		msgType, payload, err := (*r.source).ReadMessage()
		if err != nil {
			if !r.running.Load() {
				break
			}
			if consecutiveFailures++; consecutiveFailures >= maxRetries {
				r.logger.Error().Int("failures", consecutiveFailures).Msg("Max read retries exceeded, stopping relay")
				return
			}
			r.logger.Warn().Err(err).Int("failures", consecutiveFailures).Msg("Read error, reconnecting")
			(*r.source).Disconnect()
			time.Sleep(retryDelay)
			retryDelay = min(retryDelay*2, 30*time.Second)
			continue
		}

		consecutiveFailures = 0
		retryDelay = 1 * time.Second

		// Call PCM callback for RDP audio output (if set)
		if r.pcmCallback != nil {
			if pcm := GetLastPCM(); pcm != nil {
				r.pcmCallback(pcm)
			}
		}

		if msgType == ipcMsgTypeOpus && len(payload) > 0 {
			r.sample.Data = payload
			if err := r.audioTrack.WriteSample(r.sample); err != nil {
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
					r.logger.Error().
						Int("failures", consecutiveWriteFailures).
						Msg("Too many consecutive WebRTC write failures, reconnecting source")
					(*r.source).Disconnect()
					consecutiveWriteFailures = 0
					consecutiveFailures = 0
				}
			} else {
				r.framesRelayed.Add(1)
				consecutiveWriteFailures = 0
			}
		}
	}
}

type InputRelay struct {
	logger  zerolog.Logger
	running atomic.Bool
}

func NewInputRelay() *InputRelay {
	logger := logging.GetDefaultLogger().With().Str("component", "audio-input-relay").Logger()

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
