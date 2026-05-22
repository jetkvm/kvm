package kvm

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jetkvm/kvm/internal/audio"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
)

var (
	audioCancel  context.CancelFunc
	audioStopped chan struct{}
	audioTrack   *webrtc.TrackLocalStaticSample
	audioMu      sync.Mutex
)

func startAudio(track *webrtc.TrackLocalStaticSample) {
	audioMu.Lock()
	defer audioMu.Unlock()
	stopAudioLocked()

	ctx, cancel := context.WithCancel(context.Background())
	audioCancel = cancel
	audioStopped = make(chan struct{})
	audioTrack = track

	go runAudioCapture(ctx, track, audioStopped)
}

func stopAudio() {
	audioMu.Lock()
	defer audioMu.Unlock()
	stopAudioLocked()
}

// stopAudioIfOwner stops the audio capture only if it is currently bound to
// track. Used on session teardown so capture doesn't keep writing samples to
// a track whose peer connection has closed.
func stopAudioIfOwner(track *webrtc.TrackLocalStaticSample) {
	audioMu.Lock()
	defer audioMu.Unlock()
	if audioTrack != track {
		return
	}
	stopAudioLocked()
}

func stopAudioLocked() {
	if audioCancel == nil {
		return
	}
	audioCancel()
	<-audioStopped
	audioCancel = nil
	audioStopped = nil
	audioTrack = nil
}

// reopenThreshold is the number of consecutive non-idle read errors that
// triggers a close+reopen of the ALSA handle. The C-side already recovers
// EPIPE/ESTRPIPE; errors that surface here (EBADFD, ENODEV, …) usually mean
// the handle is dead — typically a USB gadget rebuild or host reattach.
const reopenThreshold = 5

func runAudioCapture(ctx context.Context, track *webrtc.TrackLocalStaticSample, stopped chan<- struct{}) {
	defer close(stopped)

	codec := audio.CodecPCMU
	if strings.EqualFold(track.Codec().MimeType, webrtc.MimeTypeG722) {
		codec = audio.CodecG722
	}

	capture, err := openCaptureWithBackoff(ctx)
	if err != nil {
		return
	}
	defer func() { capture.Close() }()

	audioLogger.Info().Str("codec", codec.String()).Msg("audio capture started")
	defer audioLogger.Info().Msg("audio capture stopped")

	sample := media.Sample{Duration: 20 * time.Millisecond}
	consecutiveErrors := 0

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		payload, err := capture.ReadEncoded(codec)
		if err != nil {
			if errors.Is(err, audio.ErrNoAudioData) {
				// Partial period or idle ALSA — back off ~half a frame so we
				// don't spin while the buffer fills.
				select {
				case <-ctx.Done():
					return
				case <-time.After(10 * time.Millisecond):
				}
				continue
			}
			consecutiveErrors++
			audioLogger.Warn().Err(err).Int("errs", consecutiveErrors).Msg("audio capture read failed")
			if consecutiveErrors >= reopenThreshold {
				capture.Close()
				next, err := openCaptureWithBackoff(ctx)
				if err != nil {
					return
				}
				capture = next
				consecutiveErrors = 0
				continue
			}
			time.Sleep(100 * time.Millisecond)
			continue
		}

		consecutiveErrors = 0
		if len(payload) == 0 {
			continue
		}

		sample.Data = payload
		if err := track.WriteSample(sample); err != nil {
			audioLogger.Warn().Err(err).Msg("audio sample write failed")
			time.Sleep(100 * time.Millisecond)
		}
	}
}

// openCaptureWithBackoff opens the ALSA capture device, retrying with
// exponential backoff (capped at 2 s) until success or ctx cancellation.
// Re-resolves the card on every attempt so a USB re-enumeration that hands
// the gadget a new card number is picked up automatically.
func openCaptureWithBackoff(ctx context.Context) (*audio.ALSACapture, error) {
	backoff := 100 * time.Millisecond
	for {
		device := alsaCaptureDevice()
		capture, err := audio.OpenALSACapture(device)
		if err == nil {
			audioLogger.Info().Str("device", device).Msg("audio capture opened")
			return capture, nil
		}
		audioLogger.Warn().Err(err).Str("device", device).Dur("retry_in", backoff).Msg("audio capture open failed")
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(backoff):
		}
		if backoff *= 2; backoff > 2*time.Second {
			backoff = 2 * time.Second
		}
	}
}

// alsaCaptureDevice returns the ALSA device for the UAC1 gadget card.
func alsaCaptureDevice() string {
	if card, ok := findALSACard("UAC1Gadget"); ok {
		return "hw:" + strconv.Itoa(card) + ",0"
	}
	return "hw:1,0"
}

func findALSACard(cardID string) (int, bool) {
	entries, err := os.ReadDir("/sys/class/sound")
	if err != nil {
		return 0, false
	}

	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, "card") {
			continue
		}
		id, err := os.ReadFile(filepath.Join("/sys/class/sound", name, "id"))
		if err != nil || strings.TrimSpace(string(id)) != cardID {
			continue
		}
		if card, err := strconv.Atoi(strings.TrimPrefix(name, "card")); err == nil {
			return card, true
		}
	}
	return 0, false
}
