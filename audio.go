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
	audioMu      sync.Mutex
)

// startAudio stops any running capture and, if track is non-nil, starts a new
// capture writing to it. Calling with nil simply stops the current capture.
func startAudio(track *webrtc.TrackLocalStaticSample) {
	audioMu.Lock()
	defer audioMu.Unlock()
	stopAudioLocked()

	if track == nil {
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	audioCancel = cancel
	audioStopped = make(chan struct{})

	go runAudioCapture(ctx, track, audioStopped)
}

func stopAudio() {
	audioMu.Lock()
	defer audioMu.Unlock()
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
}

func runAudioCapture(ctx context.Context, track *webrtc.TrackLocalStaticSample, stopped chan<- struct{}) {
	defer close(stopped)

	device := alsaCaptureDevice()
	codec := audioCodecForTrack(track)

	capture, err := audio.OpenALSACapture(device)
	if err != nil {
		audioLogger.Error().Err(err).Str("device", device).Msg("audio capture unavailable")
		return
	}
	defer capture.Close()

	audioLogger.Info().Str("device", device).Str("codec", codec.String()).Msg("audio capture started")
	defer audioLogger.Info().Msg("audio capture stopped")

	sample := media.Sample{Duration: 20 * time.Millisecond}
	idleReads := 0

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		payload, err := capture.ReadEncoded(codec)
		if err != nil {
			if errors.Is(err, audio.ErrNoAudioData) {
				if idleReads++; idleReads%500 == 0 {
					audioLogger.Debug().Int("reads", idleReads).Msg("audio capture idle")
				}
				continue
			}
			audioLogger.Warn().Err(err).Msg("audio capture read failed")
			time.Sleep(100 * time.Millisecond)
			continue
		}

		idleReads = 0
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

func audioCodecForTrack(track *webrtc.TrackLocalStaticSample) audio.Codec {
	if strings.EqualFold(track.Codec().MimeType, webrtc.MimeTypeG722) {
		return audio.CodecG722
	}
	return audio.CodecPCMU
}

// alsaCaptureDevice returns the ALSA device string for the UAC1 gadget card.
// Falls back to hw:1,0 if the sysfs lookup fails (typically only during early boot).
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
