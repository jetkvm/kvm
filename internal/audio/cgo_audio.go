//go:build linux && arm
// +build linux,arm

package audio

import (
	"errors"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/jetkvm/kvm/internal/logging"
)

/*
#cgo CFLAGS: -I${SRCDIR}/../../tools/alsa-opus-includes
#cgo LDFLAGS: -L$HOME/.jetkvm/audio-libs/alsa-lib-1.2.14/src/.libs -lasound -L$HOME/.jetkvm/audio-libs/opus-1.5.2/.libs -lopus -lm -ldl -static
#include <alsa/asoundlib.h>
#include <opus.h>
#include <stdlib.h>

// C state for ALSA/Opus
static snd_pcm_t *pcm_handle = NULL;
static OpusEncoder *encoder = NULL;
static int opus_bitrate = 64000;
static int opus_complexity = 5;
static int sample_rate = 48000;
static int channels = 2;
static int frame_size = 960; // 20ms for 48kHz
static int max_packet_size = 1500;

// Initialize ALSA and Opus encoder
int jetkvm_audio_init() {
	int err;
	snd_pcm_hw_params_t *params;
	if (pcm_handle) return 0;
	if (snd_pcm_open(&pcm_handle, "hw:1,0", SND_PCM_STREAM_CAPTURE, 0) < 0)
		return -1;
	snd_pcm_hw_params_malloc(&params);
	snd_pcm_hw_params_any(pcm_handle, params);
	snd_pcm_hw_params_set_access(pcm_handle, params, SND_PCM_ACCESS_RW_INTERLEAVED);
	snd_pcm_hw_params_set_format(pcm_handle, params, SND_PCM_FORMAT_S16_LE);
	snd_pcm_hw_params_set_channels(pcm_handle, params, channels);
	snd_pcm_hw_params_set_rate(pcm_handle, params, sample_rate, 0);
	snd_pcm_hw_params_set_period_size(pcm_handle, params, frame_size, 0);
	snd_pcm_hw_params(pcm_handle, params);
	snd_pcm_hw_params_free(params);
	snd_pcm_prepare(pcm_handle);
	encoder = opus_encoder_create(sample_rate, channels, OPUS_APPLICATION_AUDIO, &err);
	if (!encoder) return -2;
	opus_encoder_ctl(encoder, OPUS_SET_BITRATE(opus_bitrate));
	opus_encoder_ctl(encoder, OPUS_SET_COMPLEXITY(opus_complexity));
	return 0;
}

// Read and encode one frame, returns encoded size or <0 on error
int jetkvm_audio_read_encode(void *opus_buf) {
	short pcm_buffer[1920]; // max 2ch*960
	unsigned char *out = (unsigned char*)opus_buf;
	int pcm_rc = snd_pcm_readi(pcm_handle, pcm_buffer, frame_size);
	if (pcm_rc < 0) return -1;
	int nb_bytes = opus_encode(encoder, pcm_buffer, frame_size, out, max_packet_size);
	return nb_bytes;
}

void jetkvm_audio_close() {
	if (encoder) { opus_encoder_destroy(encoder); encoder = NULL; }
	if (pcm_handle) { snd_pcm_close(pcm_handle); pcm_handle = NULL; }
}
*/
import "C"

var (
	audioStreamRunning int32
)

// Go wrappers for initializing, starting, stopping, and controlling audio
func cgoAudioInit() error {
	ret := C.jetkvm_audio_init()
	if ret != 0 {
		return errors.New("failed to init ALSA/Opus")
	}
	return nil
}

func cgoAudioClose() {
	C.jetkvm_audio_close()
}

// Reads and encodes one frame, returns encoded bytes or error
func cgoAudioReadEncode(buf []byte) (int, error) {
	if len(buf) < 1500 {
		return 0, errors.New("buffer too small")
	}
	n := C.jetkvm_audio_read_encode(unsafe.Pointer(&buf[0]))
	if n < 0 {
		return 0, errors.New("audio read/encode error")
	}
	return int(n), nil
}

func StartCGOAudioStream(send func([]byte)) error {
	if !atomic.CompareAndSwapInt32(&audioStreamRunning, 0, 1) {
		return errors.New("audio stream already running")
	}
	go func() {
		defer atomic.StoreInt32(&audioStreamRunning, 0)
		logger := logging.GetDefaultLogger().With().Str("component", "audio").Logger()
		err := cgoAudioInit()
		if err != nil {
			logger.Error().Err(err).Msg("cgoAudioInit failed")
			return
		}
		defer cgoAudioClose()
		buf := make([]byte, 1500)
		errorCount := 0
		for atomic.LoadInt32(&audioStreamRunning) == 1 {
			m := IsAudioMuted()
			// (debug) logger.Debug().Msgf("audio loop: IsAudioMuted=%v", m)
			if m {
				time.Sleep(20 * time.Millisecond)
				continue
			}
			n, err := cgoAudioReadEncode(buf)
			if err != nil {
				logger.Warn().Err(err).Msg("cgoAudioReadEncode error")
				RecordFrameDropped()
				errorCount++
				if errorCount >= 10 {
					logger.Warn().Msg("Too many audio read errors, reinitializing ALSA/Opus state")
					cgoAudioClose()
					time.Sleep(100 * time.Millisecond)
					if err := cgoAudioInit(); err != nil {
						logger.Error().Err(err).Msg("cgoAudioInit failed during recovery")
						time.Sleep(500 * time.Millisecond)
						continue
					}
					errorCount = 0
				} else {
					time.Sleep(5 * time.Millisecond)
				}
				continue
			}
			errorCount = 0
			// (debug) logger.Debug().Msgf("frame encoded: %d bytes", n)
			RecordFrameReceived(n)
			send(buf[:n])
		}
		logger.Info().Msg("audio loop exited")
	}()
	return nil
}

// StopCGOAudioStream signals the audio stream goroutine to stop
func StopCGOAudioStream() {
	atomic.StoreInt32(&audioStreamRunning, 0)
}
