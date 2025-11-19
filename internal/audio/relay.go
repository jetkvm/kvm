package audio

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/jetkvm/kvm/internal/logging"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
	"github.com/rs/zerolog"
)

type OutputRelay struct {
	source     *AudioSource
	audioTrack *webrtc.TrackLocalStaticSample
	ctx        context.Context
	cancel     context.CancelFunc
	logger     zerolog.Logger
	running    atomic.Bool
	sample     media.Sample
	stopped    chan struct{}

	framesRelayed atomic.Uint32
	framesDropped atomic.Uint32
}

func NewOutputRelay(source *AudioSource, audioTrack *webrtc.TrackLocalStaticSample) *OutputRelay {
	ctx, cancel := context.WithCancel(context.Background())
	logger := logging.GetDefaultLogger().With().Str("component", "audio-output-relay").Logger()

	return &OutputRelay{
		source:     source,
		audioTrack: audioTrack,
		ctx:        ctx,
		cancel:     cancel,
		logger:     logger,
		stopped:    make(chan struct{}),
		sample: media.Sample{
			Duration: 20 * time.Millisecond,
		},
	}
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

	r.cancel()
	<-r.stopped

	r.logger.Debug().
		Uint32("frames_relayed", r.framesRelayed.Load()).
		Uint32("frames_dropped", r.framesDropped.Load()).
		Msg("output relay stopped")
}

func (r *OutputRelay) relayLoop() {
	defer close(r.stopped)

	const initialDelay = 1 * time.Second
	const maxDelay = 30 * time.Second
	const maxRetries = 10

	retryDelay := initialDelay
	consecutiveFailures := 0

	for r.running.Load() {
		if !(*r.source).IsConnected() {
			if err := (*r.source).Connect(); err != nil {
				consecutiveFailures++
				if consecutiveFailures >= maxRetries {
					r.logger.Error().Int("failures", consecutiveFailures).Msg("Max connection retries exceeded, stopping relay")
					return
				}
				r.logger.Debug().Err(err).Int("failures", consecutiveFailures).Dur("retry_delay", retryDelay).Msg("failed to connect, will retry")
				time.Sleep(retryDelay)
				retryDelay = min(retryDelay*2, maxDelay)
				continue
			}
			consecutiveFailures = 0
			retryDelay = initialDelay
		}

		msgType, payload, err := (*r.source).ReadMessage()
		if err != nil {
			if r.running.Load() {
				consecutiveFailures++
				if consecutiveFailures >= maxRetries {
					r.logger.Error().Int("failures", consecutiveFailures).Msg("Max read retries exceeded, stopping relay")
					return
				}
				r.logger.Warn().Err(err).Int("failures", consecutiveFailures).Msg("read error, reconnecting")
				(*r.source).Disconnect()
				time.Sleep(retryDelay)
				retryDelay = min(retryDelay*2, maxDelay)
			}
			continue
		}

		consecutiveFailures = 0
		retryDelay = initialDelay

		if msgType == ipcMsgTypeOpus && len(payload) > 0 {
			r.sample.Data = payload
			if err := r.audioTrack.WriteSample(r.sample); err != nil {
				r.framesDropped.Add(1)
				r.logger.Warn().Err(err).Msg("failed to write sample to WebRTC")
			} else {
				r.framesRelayed.Add(1)
			}
		}
	}
}

type InputRelay struct {
	source  *AudioSource
	ctx     context.Context
	cancel  context.CancelFunc
	logger  zerolog.Logger
	running atomic.Bool
}

func NewInputRelay(source *AudioSource) *InputRelay {
	ctx, cancel := context.WithCancel(context.Background())
	logger := logging.GetDefaultLogger().With().Str("component", "audio-input-relay").Logger()

	return &InputRelay{
		source: source,
		ctx:    ctx,
		cancel: cancel,
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

	r.cancel()
	r.logger.Debug().Msg("input relay stopped")
}
