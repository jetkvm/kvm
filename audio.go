package kvm

import (
	"context"
	"errors"
	"fmt"
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

var audioCancel context.CancelFunc
var audioStopped chan struct{}
var audioRuntimeMu sync.Mutex

type audioCaptureSource struct {
	name   string
	device string
}

func startAudio(track *webrtc.TrackLocalStaticSample) {
	if track == nil {
		return
	}

	audioRuntimeMu.Lock()
	defer audioRuntimeMu.Unlock()

	stopAudioUnderLock()

	ctx, cancel := context.WithCancel(context.Background())
	audioCancel = cancel
	audioStopped = make(chan struct{})

	go runAudioCapture(ctx, track, audioStopped)
}

func stopAudio() {
	audioRuntimeMu.Lock()
	defer audioRuntimeMu.Unlock()

	stopAudioUnderLock()
}

func stopAudioUnderLock() {
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

	codec := audioCodecForTrack(track)
	sources := audioCaptureSources()
	capture, source, sourceIndex, err := openAudioCaptureFrom(sources, 0)
	if err != nil {
		audioLogger.Error().Err(err).Msg("audio capture unavailable")
		return
	}
	defer func() {
		_ = capture.Close()
	}()

	sample := media.Sample{Duration: 20 * time.Millisecond}
	audioLogger.Info().
		Str("source", source.name).
		Str("device", source.device).
		Str("codec", codec.String()).
		Msg("audio capture started")
	defer audioLogger.Info().Msg("audio capture stopped")

	noDataReads := 0
	wroteFirstSample := false

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		payload, err := capture.ReadEncoded(codec)
		if err != nil {
			if errors.Is(err, audio.ErrNoAudioData) {
				noDataReads++
				if noDataReads == 50 || noDataReads%500 == 0 {
					audioLogger.Info().Int("reads", noDataReads).Msg("audio capture has no data")
				}
				if noDataReads == 50 {
					_ = capture.Close()
					reopened, reopenedSource, reopenedSourceIndex, openErr := openAudioCaptureFrom(sources, sourceIndex+1)
					if openErr != nil {
						audioLogger.Error().Err(openErr).Msg("audio capture reopen failed")
						time.Sleep(500 * time.Millisecond)
						noDataReads = 0
						continue
					}
					capture = reopened
					source = reopenedSource
					sourceIndex = reopenedSourceIndex
					noDataReads = 0
					audioLogger.Info().
						Str("source", source.name).
						Str("device", source.device).
						Msg("audio capture reopened after no data")
				}
				continue
			}
			audioLogger.Error().Err(err).Msg("audio capture read failed")
			time.Sleep(100 * time.Millisecond)
			continue
		}
		if len(payload) == 0 {
			continue
		}
		noDataReads = 0

		sample.Data = payload
		if err := track.WriteSample(sample); err != nil {
			select {
			case <-ctx.Done():
				return
			default:
				audioLogger.Warn().Err(err).Msg("audio sample write failed")
				time.Sleep(100 * time.Millisecond)
			}
		} else if !wroteFirstSample {
			wroteFirstSample = true
			audioLogger.Info().Str("codec", codec.String()).Int("bytes", len(payload)).Msg("audio sample written")
		}
	}
}

func audioCodecForTrack(track *webrtc.TrackLocalStaticSample) audio.Codec {
	if strings.EqualFold(track.Codec().MimeType, webrtc.MimeTypeG722) {
		return audio.CodecG722
	}
	return audio.CodecPCMU
}

func openAudioCaptureFrom(sources []audioCaptureSource, startIndex int) (audio.Reader, audioCaptureSource, int, error) {
	var errs []string
	if len(sources) == 0 {
		return nil, audioCaptureSource{}, 0, fmt.Errorf("no ALSA capture sources configured")
	}

	for offset := range sources {
		index := (startIndex + offset) % len(sources)
		source := sources[index]
		capture, err := audio.OpenALSACapture(source.device)
		if err == nil {
			return capture, source, index, nil
		}
		errs = append(errs, fmt.Sprintf("%s %s: %v", source.name, source.device, err))
	}
	return nil, audioCaptureSource{}, 0, fmt.Errorf("no ALSA capture device opened (%s)", strings.Join(errs, "; "))
}

func audioCaptureSources() []audioCaptureSource {
	var sources []audioCaptureSource
	if card, ok := findALSACard("UAC1Gadget"); ok {
		sources = append(sources, audioCaptureSource{
			name:   "uac1",
			device: "hw:" + strconv.Itoa(card) + ",0",
		})
	}

	return append(sources,
		audioCaptureSource{name: "uac1-default", device: "hw:1,0"},
	)
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

		card, err := strconv.Atoi(strings.TrimPrefix(name, "card"))
		if err == nil {
			return card, true
		}
	}
	return 0, false
}
