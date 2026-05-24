//go:build linux && cgo

package audio

/*
#cgo LDFLAGS: -ldl

#include <dlfcn.h>
#include <errno.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>

#include <string.h>

typedef struct _snd_pcm snd_pcm_t;
typedef struct _snd_pcm_hw_params snd_pcm_hw_params_t;
typedef struct _snd_pcm_sw_params snd_pcm_sw_params_t;
typedef long snd_pcm_sframes_t;
typedef unsigned long snd_pcm_uframes_t;

enum {
	JK_PB_SND_PCM_STREAM_PLAYBACK = 0,
	JK_PB_SND_PCM_ACCESS_RW_INTERLEAVED = 3,
	JK_PB_SND_PCM_FORMAT_S16_LE = 2,
	JK_PB_SND_PCM_NONBLOCK = 1,
};

typedef struct {
	void *lib;
	int (*pcm_open)(snd_pcm_t **pcm, const char *name, int stream, int mode);
	int (*pcm_nonblock)(snd_pcm_t *pcm, int nonblock);
	int (*pcm_close)(snd_pcm_t *pcm);
	size_t (*hw_params_sizeof)(void);
	size_t (*sw_params_sizeof)(void);
	int (*hw_params_any)(snd_pcm_t *pcm, snd_pcm_hw_params_t *params);
	int (*hw_params_set_access)(snd_pcm_t *pcm, snd_pcm_hw_params_t *params, int access);
	int (*hw_params_set_format)(snd_pcm_t *pcm, snd_pcm_hw_params_t *params, int format);
	int (*hw_params_set_channels)(snd_pcm_t *pcm, snd_pcm_hw_params_t *params, unsigned int channels);
	int (*hw_params_set_rate_near)(snd_pcm_t *pcm, snd_pcm_hw_params_t *params, unsigned int *rate, int *dir);
	int (*hw_params_set_period_size_near)(snd_pcm_t *pcm, snd_pcm_hw_params_t *params, snd_pcm_uframes_t *val, int *dir);
	int (*hw_params_set_buffer_size_near)(snd_pcm_t *pcm, snd_pcm_hw_params_t *params, snd_pcm_uframes_t *val);
	int (*hw_params)(snd_pcm_t *pcm, snd_pcm_hw_params_t *params);
	int (*sw_params_current)(snd_pcm_t *pcm, snd_pcm_sw_params_t *params);
	int (*sw_params_set_start_threshold)(snd_pcm_t *pcm, snd_pcm_sw_params_t *params, snd_pcm_uframes_t val);
	int (*sw_params_set_avail_min)(snd_pcm_t *pcm, snd_pcm_sw_params_t *params, snd_pcm_uframes_t val);
	int (*sw_params)(snd_pcm_t *pcm, snd_pcm_sw_params_t *params);
	int (*pcm_prepare)(snd_pcm_t *pcm);
	int (*pcm_wait)(snd_pcm_t *pcm, int timeout);
	snd_pcm_sframes_t (*pcm_writei)(snd_pcm_t *pcm, const void *buffer, snd_pcm_uframes_t size);
	int (*pcm_recover)(snd_pcm_t *pcm, int err, int silent);
	const char *(*strerror)(int errnum);
} jk_playback_alsa_api;

typedef struct {
	snd_pcm_t *pcm;
	unsigned int channels;
	snd_pcm_uframes_t period_frames;
} jk_alsa_playback;

static jk_playback_alsa_api jk_playback_alsa;
static int jk_playback_alsa_loaded = 0;

static int jk_playback_load_sym(void **target, const char *name) {
	*target = dlsym(jk_playback_alsa.lib, name);
	return *target == NULL ? -1 : 0;
}

static int jk_playback_alsa_load(char *errbuf, int errbuf_len) {
	if (jk_playback_alsa_loaded) {
		return 0;
	}

	jk_playback_alsa.lib = dlopen("libasound.so.2", RTLD_NOW | RTLD_LOCAL);
	if (jk_playback_alsa.lib == NULL) {
		snprintf(errbuf, errbuf_len, "dlopen libasound.so.2 failed: %s", dlerror());
		return -1;
	}

	int err = 0;
	err |= jk_playback_load_sym((void **)&jk_playback_alsa.pcm_open, "snd_pcm_open");
	err |= jk_playback_load_sym((void **)&jk_playback_alsa.pcm_nonblock, "snd_pcm_nonblock");
	err |= jk_playback_load_sym((void **)&jk_playback_alsa.pcm_close, "snd_pcm_close");
	err |= jk_playback_load_sym((void **)&jk_playback_alsa.hw_params_sizeof, "snd_pcm_hw_params_sizeof");
	err |= jk_playback_load_sym((void **)&jk_playback_alsa.sw_params_sizeof, "snd_pcm_sw_params_sizeof");
	err |= jk_playback_load_sym((void **)&jk_playback_alsa.hw_params_any, "snd_pcm_hw_params_any");
	err |= jk_playback_load_sym((void **)&jk_playback_alsa.hw_params_set_access, "snd_pcm_hw_params_set_access");
	err |= jk_playback_load_sym((void **)&jk_playback_alsa.hw_params_set_format, "snd_pcm_hw_params_set_format");
	err |= jk_playback_load_sym((void **)&jk_playback_alsa.hw_params_set_channels, "snd_pcm_hw_params_set_channels");
	err |= jk_playback_load_sym((void **)&jk_playback_alsa.hw_params_set_rate_near, "snd_pcm_hw_params_set_rate_near");
	err |= jk_playback_load_sym((void **)&jk_playback_alsa.hw_params_set_period_size_near, "snd_pcm_hw_params_set_period_size_near");
	err |= jk_playback_load_sym((void **)&jk_playback_alsa.hw_params_set_buffer_size_near, "snd_pcm_hw_params_set_buffer_size_near");
	err |= jk_playback_load_sym((void **)&jk_playback_alsa.hw_params, "snd_pcm_hw_params");
	err |= jk_playback_load_sym((void **)&jk_playback_alsa.sw_params_current, "snd_pcm_sw_params_current");
	err |= jk_playback_load_sym((void **)&jk_playback_alsa.sw_params_set_start_threshold, "snd_pcm_sw_params_set_start_threshold");
	err |= jk_playback_load_sym((void **)&jk_playback_alsa.sw_params_set_avail_min, "snd_pcm_sw_params_set_avail_min");
	err |= jk_playback_load_sym((void **)&jk_playback_alsa.sw_params, "snd_pcm_sw_params");
	err |= jk_playback_load_sym((void **)&jk_playback_alsa.pcm_prepare, "snd_pcm_prepare");
	err |= jk_playback_load_sym((void **)&jk_playback_alsa.pcm_wait, "snd_pcm_wait");
	err |= jk_playback_load_sym((void **)&jk_playback_alsa.pcm_writei, "snd_pcm_writei");
	err |= jk_playback_load_sym((void **)&jk_playback_alsa.pcm_recover, "snd_pcm_recover");
	err |= jk_playback_load_sym((void **)&jk_playback_alsa.strerror, "snd_strerror");
	if (err != 0) {
		snprintf(errbuf, errbuf_len, "failed to load required ALSA symbol");
		return -1;
	}

	jk_playback_alsa_loaded = 1;
	return 0;
}

static void jk_playback_set_error(char *errbuf, int errbuf_len, const char *op, int err) {
	if (jk_playback_alsa.strerror) {
		snprintf(errbuf, errbuf_len, "%s: %s", op, jk_playback_alsa.strerror(err));
	} else {
		snprintf(errbuf, errbuf_len, "%s: %d", op, err);
	}
}

static int jk_configure_playback(jk_alsa_playback *playback, int format, unsigned int rate, unsigned int channels, unsigned long period_frames, unsigned int periods, char *errbuf, int errbuf_len) {
	int err = 0;
	snd_pcm_hw_params_t *hw = calloc(1, jk_playback_alsa.hw_params_sizeof());
	snd_pcm_sw_params_t *sw = calloc(1, jk_playback_alsa.sw_params_sizeof());
	if (hw == NULL || sw == NULL) {
		free(hw);
		free(sw);
		snprintf(errbuf, errbuf_len, "failed to allocate ALSA params");
		return -ENOMEM;
	}

	err = jk_playback_alsa.hw_params_any(playback->pcm, hw);
	if (err < 0) goto fail_hw_any;
	err = jk_playback_alsa.hw_params_set_access(playback->pcm, hw, JK_PB_SND_PCM_ACCESS_RW_INTERLEAVED);
	if (err < 0) goto fail_access;
	err = jk_playback_alsa.hw_params_set_format(playback->pcm, hw, format);
	if (err < 0) goto fail_format;
	err = jk_playback_alsa.hw_params_set_channels(playback->pcm, hw, channels);
	if (err < 0) goto fail_channels;
	err = jk_playback_alsa.hw_params_set_rate_near(playback->pcm, hw, &rate, NULL);
	if (err < 0) goto fail_rate;

	snd_pcm_uframes_t period = period_frames;
	err = jk_playback_alsa.hw_params_set_period_size_near(playback->pcm, hw, &period, NULL);
	if (err < 0) goto fail_period;

	snd_pcm_uframes_t buffer = period * periods;
	err = jk_playback_alsa.hw_params_set_buffer_size_near(playback->pcm, hw, &buffer);
	if (err < 0) goto fail_buffer;

	err = jk_playback_alsa.hw_params(playback->pcm, hw);
	if (err < 0) goto fail_hw;

	err = jk_playback_alsa.sw_params_current(playback->pcm, sw);
	if (err < 0) goto fail_sw_current;
	err = jk_playback_alsa.sw_params_set_start_threshold(playback->pcm, sw, period);
	if (err < 0) goto fail_start;
	err = jk_playback_alsa.sw_params_set_avail_min(playback->pcm, sw, period);
	if (err < 0) goto fail_avail;
	err = jk_playback_alsa.sw_params(playback->pcm, sw);
	if (err < 0) goto fail_sw;
	err = jk_playback_alsa.pcm_prepare(playback->pcm);
	if (err < 0) goto fail_prepare;
	err = jk_playback_alsa.pcm_nonblock(playback->pcm, 0);
	if (err < 0) goto fail_blocking_mode;

	playback->channels = channels;
	playback->period_frames = period;
	free(hw);
	free(sw);
	return 0;

fail_blocking_mode:
	jk_playback_set_error(errbuf, errbuf_len, "snd_pcm_nonblock", err); goto fail;
fail_prepare:
	jk_playback_set_error(errbuf, errbuf_len, "snd_pcm_prepare", err); goto fail;
fail_sw:
	jk_playback_set_error(errbuf, errbuf_len, "snd_pcm_sw_params", err); goto fail;
fail_avail:
	jk_playback_set_error(errbuf, errbuf_len, "snd_pcm_sw_params_set_avail_min", err); goto fail;
fail_start:
	jk_playback_set_error(errbuf, errbuf_len, "snd_pcm_sw_params_set_start_threshold", err); goto fail;
fail_sw_current:
	jk_playback_set_error(errbuf, errbuf_len, "snd_pcm_sw_params_current", err); goto fail;
fail_hw:
	jk_playback_set_error(errbuf, errbuf_len, "snd_pcm_hw_params", err); goto fail;
fail_buffer:
	jk_playback_set_error(errbuf, errbuf_len, "snd_pcm_hw_params_set_buffer_size_near", err); goto fail;
fail_period:
	jk_playback_set_error(errbuf, errbuf_len, "snd_pcm_hw_params_set_period_size_near", err); goto fail;
fail_rate:
	jk_playback_set_error(errbuf, errbuf_len, "snd_pcm_hw_params_set_rate_near", err); goto fail;
fail_channels:
	jk_playback_set_error(errbuf, errbuf_len, "snd_pcm_hw_params_set_channels", err); goto fail;
fail_format:
	jk_playback_set_error(errbuf, errbuf_len, "snd_pcm_hw_params_set_format", err); goto fail;
fail_access:
	jk_playback_set_error(errbuf, errbuf_len, "snd_pcm_hw_params_set_access", err); goto fail;
fail_hw_any:
	jk_playback_set_error(errbuf, errbuf_len, "snd_pcm_hw_params_any", err); goto fail;
fail:
	free(hw);
	free(sw);
	return err;
}

static jk_alsa_playback *jk_alsa_playback_open(const char *device, int format, unsigned int rate, unsigned int channels, unsigned long period_frames, unsigned int periods, char *errbuf, int errbuf_len) {
	if (jk_playback_alsa_load(errbuf, errbuf_len) != 0) {
		return NULL;
	}

	jk_alsa_playback *playback = calloc(1, sizeof(jk_alsa_playback));
	if (playback == NULL) {
		snprintf(errbuf, errbuf_len, "failed to allocate playback");
		return NULL;
	}

	int err = jk_playback_alsa.pcm_open(&playback->pcm, device, JK_PB_SND_PCM_STREAM_PLAYBACK, JK_PB_SND_PCM_NONBLOCK);
	if (err < 0) {
		jk_playback_set_error(errbuf, errbuf_len, "snd_pcm_open", err);
		free(playback);
		return NULL;
	}

	err = jk_configure_playback(playback, format, rate, channels, period_frames, periods, errbuf, errbuf_len);
	if (err < 0) {
		jk_playback_alsa.pcm_close(playback->pcm);
		free(playback);
		return NULL;
	}

	return playback;
}

static int jk_alsa_playback_write(jk_alsa_playback *playback, const void *buffer, unsigned long frames) {
	unsigned long written = 0;
	const int16_t *samples = (const int16_t *)buffer;

	while (written < frames) {
		int wait_rc = jk_playback_alsa.pcm_wait(playback->pcm, 100);
		if (wait_rc == 0) {
			return (int)written;
		}
		if (wait_rc < 0) {
			int recovered = jk_playback_alsa.pcm_recover(playback->pcm, wait_rc, 1);
			if (recovered >= 0) {
				continue;
			}
			return wait_rc;
		}

		snd_pcm_sframes_t rc = jk_playback_alsa.pcm_writei(
			playback->pcm,
			samples + (written * playback->channels),
			frames - written
		);
		if (rc > 0) {
			written += (unsigned long)rc;
			continue;
		}
		if (rc == -EAGAIN) {
			continue;
		}

		int recovered = jk_playback_alsa.pcm_recover(playback->pcm, (int)rc, 1);
		if (recovered >= 0) {
			continue;
		}
		return (int)rc;
	}

	return (int)written;
}

static void jk_alsa_playback_close(jk_alsa_playback *playback) {
	if (playback == NULL) {
		return;
	}
	if (playback->pcm != NULL) {
		jk_playback_alsa.pcm_close(playback->pcm);
	}
	free(playback);
}
*/
import "C"

import (
	"fmt"
	"unsafe"
)

const (
	PlaybackSampleRate = 48000
	PlaybackChannels   = 2
	PlaybackFrameSize  = 960
	// Browser microphone capture levels are conservative, and PCMU is
	// narrowband. Boost before presenting the stream as a USB microphone so
	// the host receives a usable level without depending on host-side gain.
	MicrophonePlaybackGain = 6
)

type ALSAPlayback struct {
	handle unsafe.Pointer
	pcm16  []int16
}

func OpenALSAPlayback(device string) (*ALSAPlayback, error) {
	cDevice := C.CString(device)
	defer C.free(unsafe.Pointer(cDevice))

	errBuf := make([]byte, 256)
	handle := C.jk_alsa_playback_open(
		cDevice,
		C.int(C.JK_PB_SND_PCM_FORMAT_S16_LE),
		C.uint(PlaybackSampleRate),
		C.uint(PlaybackChannels),
		C.ulong(PlaybackFrameSize),
		C.uint(4),
		(*C.char)(unsafe.Pointer(&errBuf[0])),
		C.int(len(errBuf)),
	)
	if handle == nil {
		return nil, fmt.Errorf("%s", C.GoString((*C.char)(unsafe.Pointer(&errBuf[0]))))
	}

	return &ALSAPlayback{handle: unsafe.Pointer(handle)}, nil
}

func (p *ALSAPlayback) WritePCMU(payload []byte) error {
	const outputFramesPerInputSample = PlaybackSampleRate / 8000
	frames := len(payload) * outputFramesPerInputSample
	sampleCount := frames * PlaybackChannels
	if cap(p.pcm16) < sampleCount {
		p.pcm16 = make([]int16, sampleCount)
	}
	pcm16 := p.pcm16[:sampleCount]

	out := 0
	for _, encoded := range payload {
		sample := ApplyPCM16Gain(PCMUToLinear(encoded), MicrophonePlaybackGain)
		for i := 0; i < outputFramesPerInputSample; i++ {
			pcm16[out] = sample
			pcm16[out+1] = sample
			out += PlaybackChannels
		}
	}

	return p.WritePCM16(pcm16)
}

func (p *ALSAPlayback) WritePCM16(samples []int16) error {
	if len(samples) == 0 {
		return nil
	}
	if len(samples)%PlaybackChannels != 0 {
		return fmt.Errorf("PCM sample count %d is not divisible by channel count %d", len(samples), PlaybackChannels)
	}

	frames := len(samples) / PlaybackChannels
	rc := C.jk_alsa_playback_write(
		(*C.jk_alsa_playback)(p.handle),
		unsafe.Pointer(&samples[0]),
		C.ulong(frames),
	)
	if rc < 0 {
		return fmt.Errorf("snd_pcm_writei: %d", int(rc))
	}
	if int(rc) < frames {
		return ErrNoAudioData
	}
	return nil
}

func (p *ALSAPlayback) Close() error {
	if p.handle != nil {
		C.jk_alsa_playback_close((*C.jk_alsa_playback)(p.handle))
		p.handle = nil
	}
	return nil
}
