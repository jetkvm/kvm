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

// OutputRelay forwards audio from subprocess (HDMI) to WebRTC (browser)
type OutputRelay struct {
	client     *IPCClient
	audioTrack *webrtc.TrackLocalStaticSample
	ctx        context.Context
	cancel     context.CancelFunc
	logger     zerolog.Logger
	running    atomic.Bool
	sample     media.Sample // Reusable sample for zero-allocation hot path

	// Stats (Uint32: overflows after 2.7 years @ 50fps, faster atomics on 32-bit ARM)
	framesRelayed atomic.Uint32
	framesDropped atomic.Uint32
}

// NewOutputRelay creates a relay for output audio (device → browser)
func NewOutputRelay(client *IPCClient, audioTrack *webrtc.TrackLocalStaticSample) *OutputRelay {
	ctx, cancel := context.WithCancel(context.Background())
	logger := logging.GetDefaultLogger().With().Str("component", "audio-output-relay").Logger()

	return &OutputRelay{
		client:     client,
		audioTrack: audioTrack,
		ctx:        ctx,
		cancel:     cancel,
		logger:     logger,
		sample: media.Sample{
			Duration: 20 * time.Millisecond, // Constant for all Opus frames
		},
	}
}

// Start begins relaying audio frames
func (r *OutputRelay) Start() error {
	if r.running.Swap(true) {
		return fmt.Errorf("output relay already running")
	}

	go r.relayLoop()
	r.logger.Debug().Msg("output relay started")
	return nil
}

// Stop stops the relay
func (r *OutputRelay) Stop() {
	if !r.running.Swap(false) {
		return
	}

	r.cancel()
	r.logger.Debug().
		Uint32("frames_relayed", r.framesRelayed.Load()).
		Uint32("frames_dropped", r.framesDropped.Load()).
		Msg("output relay stopped")
}

// relayLoop continuously reads from IPC and writes to WebRTC
func (r *OutputRelay) relayLoop() {
	const reconnectDelay = 1 * time.Second

	for r.running.Load() {
		// Ensure connected
		if !r.client.IsConnected() {
			if err := r.client.Connect(); err != nil {
				r.logger.Debug().Err(err).Msg("failed to connect, will retry")
				time.Sleep(reconnectDelay)
				continue
			}
		}

		// Read message from subprocess
		msgType, payload, err := r.client.ReadMessage()
		if err != nil {
			// Connection error - reconnect
			if r.running.Load() {
				r.logger.Warn().Err(err).Msg("read error, reconnecting")
				r.client.Disconnect()
				time.Sleep(reconnectDelay)
			}
			continue
		}

		// Handle message
		if msgType == ipcMsgTypeOpus && len(payload) > 0 {
			// Reuse sample struct (zero-allocation hot path)
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

// InputRelay forwards audio from WebRTC (browser microphone) to subprocess (USB audio)
type InputRelay struct {
	client  *IPCClient
	ctx     context.Context
	cancel  context.CancelFunc
	logger  zerolog.Logger
	running atomic.Bool
}

// NewInputRelay creates a relay for input audio (browser → device)
func NewInputRelay(client *IPCClient) *InputRelay {
	ctx, cancel := context.WithCancel(context.Background())
	logger := logging.GetDefaultLogger().With().Str("component", "audio-input-relay").Logger()

	return &InputRelay{
		client: client,
		ctx:    ctx,
		cancel: cancel,
		logger: logger,
	}
}

// Start begins relaying audio frames
func (r *InputRelay) Start() error {
	if r.running.Swap(true) {
		return fmt.Errorf("input relay already running")
	}

	r.logger.Debug().Msg("input relay started")
	return nil
}

// Stop stops the relay
func (r *InputRelay) Stop() {
	if !r.running.Swap(false) {
		return
	}

	r.cancel()
	r.logger.Debug().Msg("input relay stopped")
}
