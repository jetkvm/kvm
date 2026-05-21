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

func startAudio(track *webrtc.TrackLocalStaticSample) {
	audioMu.Lock()
	defer audioMu.Unlock()
	stopAudioLocked()

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

	codec := audio.CodecPCMU
	if strings.EqualFold(track.Codec().MimeType, webrtc.MimeTypeG722) {
		codec = audio.CodecG722
	}

	device := alsaCaptureDevice()
	capture, err := audio.OpenALSACapture(device)
	if err != nil {
		audioLogger.Error().Err(err).Str("device", device).Msg("audio capture unavailable")
		return
	}
	defer capture.Close()

	audioLogger.Info().Str("device", device).Str("codec", codec.String()).Msg("audio capture started")
	defer audioLogger.Info().Msg("audio capture stopped")

	sample := media.Sample{Duration: 20 * time.Millisecond}

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		payload, err := capture.ReadEncoded(codec)
		if err != nil {
			if errors.Is(err, audio.ErrNoAudioData) {
				continue
			}
			audioLogger.Warn().Err(err).Msg("audio capture read failed")
			time.Sleep(100 * time.Millisecond)
			continue
		}
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
