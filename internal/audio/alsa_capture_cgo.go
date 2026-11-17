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
	JK_SND_PCM_STREAM_CAPTURE = 1,
	JK_SND_PCM_ACCESS_RW_INTERLEAVED = 3,
	JK_SND_PCM_FORMAT_S16_LE = 2,
	JK_SND_PCM_NONBLOCK = 1,
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
	int (*pcm_start)(snd_pcm_t *pcm);
	int (*pcm_wait)(snd_pcm_t *pcm, int timeout);
	snd_pcm_sframes_t (*pcm_readi)(snd_pcm_t *pcm, void *buffer, snd_pcm_uframes_t size);
	int (*pcm_recover)(snd_pcm_t *pcm, int err, int silent);
	const char *(*strerror)(int errnum);
} jk_alsa_api;

typedef struct {
	snd_pcm_t *pcm;
	unsigned int channels;
	snd_pcm_uframes_t period_frames;
} jk_alsa_capture;

static jk_alsa_api jk_alsa;
static int jk_alsa_loaded = 0;

static int jk_load_sym(void **target, const char *name) {
	*target = dlsym(jk_alsa.lib, name);
	return *target == NULL ? -1 : 0;
}

static int jk_alsa_load(char *errbuf, int errbuf_len) {
	if (jk_alsa_loaded) {
		return 0;
	}

	jk_alsa.lib = dlopen("libasound.so.2", RTLD_NOW | RTLD_LOCAL);
	if (jk_alsa.lib == NULL) {
		snprintf(errbuf, errbuf_len, "dlopen libasound.so.2 failed: %s", dlerror());
		return -1;
	}

	int err = 0;
	err |= jk_load_sym((void **)&jk_alsa.pcm_open, "snd_pcm_open");
	err |= jk_load_sym((void **)&jk_alsa.pcm_nonblock, "snd_pcm_nonblock");
	err |= jk_load_sym((void **)&jk_alsa.pcm_close, "snd_pcm_close");
	err |= jk_load_sym((void **)&jk_alsa.hw_params_sizeof, "snd_pcm_hw_params_sizeof");
	err |= jk_load_sym((void **)&jk_alsa.sw_params_sizeof, "snd_pcm_sw_params_sizeof");
	err |= jk_load_sym((void **)&jk_alsa.hw_params_any, "snd_pcm_hw_params_any");
	err |= jk_load_sym((void **)&jk_alsa.hw_params_set_access, "snd_pcm_hw_params_set_access");
	err |= jk_load_sym((void **)&jk_alsa.hw_params_set_format, "snd_pcm_hw_params_set_format");
	err |= jk_load_sym((void **)&jk_alsa.hw_params_set_channels, "snd_pcm_hw_params_set_channels");
	err |= jk_load_sym((void **)&jk_alsa.hw_params_set_rate_near, "snd_pcm_hw_params_set_rate_near");
	err |= jk_load_sym((void **)&jk_alsa.hw_params_set_period_size_near, "snd_pcm_hw_params_set_period_size_near");
	err |= jk_load_sym((void **)&jk_alsa.hw_params_set_buffer_size_near, "snd_pcm_hw_params_set_buffer_size_near");
	err |= jk_load_sym((void **)&jk_alsa.hw_params, "snd_pcm_hw_params");
	err |= jk_load_sym((void **)&jk_alsa.sw_params_current, "snd_pcm_sw_params_current");
	err |= jk_load_sym((void **)&jk_alsa.sw_params_set_start_threshold, "snd_pcm_sw_params_set_start_threshold");
	err |= jk_load_sym((void **)&jk_alsa.sw_params_set_avail_min, "snd_pcm_sw_params_set_avail_min");
	err |= jk_load_sym((void **)&jk_alsa.sw_params, "snd_pcm_sw_params");
	err |= jk_load_sym((void **)&jk_alsa.pcm_prepare, "snd_pcm_prepare");
	err |= jk_load_sym((void **)&jk_alsa.pcm_start, "snd_pcm_start");
	err |= jk_load_sym((void **)&jk_alsa.pcm_wait, "snd_pcm_wait");
	err |= jk_load_sym((void **)&jk_alsa.pcm_readi, "snd_pcm_readi");
	err |= jk_load_sym((void **)&jk_alsa.pcm_recover, "snd_pcm_recover");
	err |= jk_load_sym((void **)&jk_alsa.strerror, "snd_strerror");
	if (err != 0) {
		snprintf(errbuf, errbuf_len, "failed to load required ALSA symbol");
		return -1;
	}

	jk_alsa_loaded = 1;
	return 0;
}

static void jk_set_error(char *errbuf, int errbuf_len, const char *op, int err) {
	if (jk_alsa.strerror) {
		snprintf(errbuf, errbuf_len, "%s: %s", op, jk_alsa.strerror(err));
	} else {
		snprintf(errbuf, errbuf_len, "%s: %d", op, err);
	}
}

static int jk_configure_capture(jk_alsa_capture *capture, int format, unsigned int rate, unsigned int channels, unsigned long period_frames, unsigned int periods, char *errbuf, int errbuf_len) {
	int err = 0;
	snd_pcm_hw_params_t *hw = calloc(1, jk_alsa.hw_params_sizeof());
	snd_pcm_sw_params_t *sw = calloc(1, jk_alsa.sw_params_sizeof());
	if (hw == NULL || sw == NULL) {
		free(hw);
		free(sw);
		snprintf(errbuf, errbuf_len, "failed to allocate ALSA params");
		return -ENOMEM;
	}

	err = jk_alsa.hw_params_any(capture->pcm, hw);
	if (err < 0) goto fail_hw_any;
	err = jk_alsa.hw_params_set_access(capture->pcm, hw, JK_SND_PCM_ACCESS_RW_INTERLEAVED);
	if (err < 0) goto fail_access;
	err = jk_alsa.hw_params_set_format(capture->pcm, hw, format);
	if (err < 0) goto fail_format;
	err = jk_alsa.hw_params_set_channels(capture->pcm, hw, channels);
	if (err < 0) goto fail_channels;
	err = jk_alsa.hw_params_set_rate_near(capture->pcm, hw, &rate, NULL);
	if (err < 0) goto fail_rate;

	snd_pcm_uframes_t period = period_frames;
	err = jk_alsa.hw_params_set_period_size_near(capture->pcm, hw, &period, NULL);
	if (err < 0) goto fail_period;

	snd_pcm_uframes_t buffer = period * periods;
	err = jk_alsa.hw_params_set_buffer_size_near(capture->pcm, hw, &buffer);
	if (err < 0) goto fail_buffer;

	err = jk_alsa.hw_params(capture->pcm, hw);
	if (err < 0) goto fail_hw;

	err = jk_alsa.sw_params_current(capture->pcm, sw);
	if (err < 0) goto fail_sw_current;
	err = jk_alsa.sw_params_set_start_threshold(capture->pcm, sw, period);
	if (err < 0) goto fail_start;
	err = jk_alsa.sw_params_set_avail_min(capture->pcm, sw, period);
	if (err < 0) goto fail_avail;
	err = jk_alsa.sw_params(capture->pcm, sw);
	if (err < 0) goto fail_sw;
	err = jk_alsa.pcm_prepare(capture->pcm);
	if (err < 0) goto fail_prepare;
	err = jk_alsa.pcm_start(capture->pcm);
	if (err < 0) goto fail_start_stream;
	err = jk_alsa.pcm_nonblock(capture->pcm, 0);
	if (err < 0) goto fail_blocking_mode;

	capture->channels = channels;
	capture->period_frames = period;
	free(hw);
	free(sw);
	return 0;

fail_blocking_mode:
	jk_set_error(errbuf, errbuf_len, "snd_pcm_nonblock", err); goto fail;
fail_start_stream:
	jk_set_error(errbuf, errbuf_len, "snd_pcm_start", err); goto fail;
fail_prepare:
	jk_set_error(errbuf, errbuf_len, "snd_pcm_prepare", err); goto fail;
fail_sw:
	jk_set_error(errbuf, errbuf_len, "snd_pcm_sw_params", err); goto fail;
fail_avail:
	jk_set_error(errbuf, errbuf_len, "snd_pcm_sw_params_set_avail_min", err); goto fail;
fail_start:
	jk_set_error(errbuf, errbuf_len, "snd_pcm_sw_params_set_start_threshold", err); goto fail;
fail_sw_current:
	jk_set_error(errbuf, errbuf_len, "snd_pcm_sw_params_current", err); goto fail;
fail_hw:
	jk_set_error(errbuf, errbuf_len, "snd_pcm_hw_params", err); goto fail;
fail_buffer:
	jk_set_error(errbuf, errbuf_len, "snd_pcm_hw_params_set_buffer_size_near", err); goto fail;
fail_period:
	jk_set_error(errbuf, errbuf_len, "snd_pcm_hw_params_set_period_size_near", err); goto fail;
fail_rate:
	jk_set_error(errbuf, errbuf_len, "snd_pcm_hw_params_set_rate_near", err); goto fail;
fail_channels:
	jk_set_error(errbuf, errbuf_len, "snd_pcm_hw_params_set_channels", err); goto fail;
fail_format:
	jk_set_error(errbuf, errbuf_len, "snd_pcm_hw_params_set_format", err); goto fail;
fail_access:
	jk_set_error(errbuf, errbuf_len, "snd_pcm_hw_params_set_access", err); goto fail;
fail_hw_any:
	jk_set_error(errbuf, errbuf_len, "snd_pcm_hw_params_any", err); goto fail;
fail:
	free(hw);
	free(sw);
	return err;
}

static jk_alsa_capture *jk_alsa_capture_open(const char *device, int format, unsigned int rate, unsigned int channels, unsigned long period_frames, unsigned int periods, char *errbuf, int errbuf_len) {
	if (jk_alsa_load(errbuf, errbuf_len) != 0) {
		return NULL;
	}

	jk_alsa_capture *capture = calloc(1, sizeof(jk_alsa_capture));
	if (capture == NULL) {
		snprintf(errbuf, errbuf_len, "failed to allocate capture");
		return NULL;
	}

	int err = jk_alsa.pcm_open(&capture->pcm, device, JK_SND_PCM_STREAM_CAPTURE, JK_SND_PCM_NONBLOCK);
	if (err < 0) {
		jk_set_error(errbuf, errbuf_len, "snd_pcm_open", err);
		free(capture);
		return NULL;
	}

	err = jk_configure_capture(capture, format, rate, channels, period_frames, periods, errbuf, errbuf_len);
	if (err < 0) {
		jk_alsa.pcm_close(capture->pcm);
		free(capture);
		return NULL;
	}

	return capture;
}

static int jk_alsa_capture_read(jk_alsa_capture *capture, void *buffer, unsigned long frames) {
	int wait_rc = jk_alsa.pcm_wait(capture->pcm, 100);
	if (wait_rc == 0) {
		return 0;
	}
	if (wait_rc < 0) {
		int recovered = jk_alsa.pcm_recover(capture->pcm, wait_rc, 1);
		return recovered >= 0 ? 0 : wait_rc;
	}

	snd_pcm_sframes_t rc = jk_alsa.pcm_readi(capture->pcm, buffer, frames);
	if (rc >= 0) {
		return (int)rc;
	}

	if (rc == -EAGAIN) {
		jk_alsa.pcm_wait(capture->pcm, 100);
		return 0;
	}

	int recovered = jk_alsa.pcm_recover(capture->pcm, (int)rc, 1);
	if (recovered >= 0) {
		return 0;
	}

	return (int)rc;
}

static void jk_alsa_capture_close(jk_alsa_capture *capture) {
	if (capture == NULL) {
		return;
	}
	if (capture->pcm != NULL) {
		jk_alsa.pcm_close(capture->pcm);
	}
	free(capture);
}
*/
import "C"

import (
	"fmt"
	"unsafe"
)

type ALSACapture struct {
	handle      unsafe.Pointer
	pcm16       []int16
	pcmu        []byte
	g722        []byte
	mono16k     []int16
	g722Encoder *G722Encoder
}

func OpenALSACapture(device string) (*ALSACapture, error) {
	cDevice := C.CString(device)
	defer C.free(unsafe.Pointer(cDevice))

	errBuf := make([]byte, 256)
	handle := C.jk_alsa_capture_open(
		cDevice,
		C.int(C.JK_SND_PCM_FORMAT_S16_LE),
		C.uint(CaptureSampleRate),
		C.uint(CaptureChannels),
		C.ulong(CaptureFrameSize),
		C.uint(4),
		(*C.char)(unsafe.Pointer(&errBuf[0])),
		C.int(len(errBuf)),
	)
	if handle == nil {
		return nil, fmt.Errorf("%s", C.GoString((*C.char)(unsafe.Pointer(&errBuf[0]))))
	}

	capture := &ALSACapture{
		handle:      unsafe.Pointer(handle),
		pcmu:        make([]byte, PCMUFrameSize),
		g722:        make([]byte, G722EncodedFrameSize),
		mono16k:     make([]int16, G722InputFrameSize),
		g722Encoder: NewG722Encoder(),
		pcm16:       make([]int16, CaptureFrameSize*CaptureChannels),
	}
	return capture, nil
}

func (c *ALSACapture) ReadEncoded(codec Codec) ([]byte, error) {
	if err := c.readPCM(); err != nil {
		return nil, err
	}

	switch codec {
	case CodecG722:
		return c.encodeG722(), nil
	case CodecPCMU:
		return c.encodePCMU(), nil
	default:
		return nil, fmt.Errorf("unsupported audio codec: %s", codec)
	}
}

// readPCM fills c.pcm16 with exactly CaptureFrameSize stereo frames from ALSA.
// Returns ErrNoAudioData on a short or empty read so the caller emits no frame
// for this cycle (the encoders depend on the full buffer being valid).
func (c *ALSACapture) readPCM() error {
	rc := C.jk_alsa_capture_read(
		(*C.jk_alsa_capture)(c.handle),
		unsafe.Pointer(&c.pcm16[0]),
		C.ulong(CaptureFrameSize),
	)
	if rc < 0 {
		return fmt.Errorf("snd_pcm_readi: %d", int(rc))
	}
	if int(rc) < CaptureFrameSize {
		return ErrNoAudioData
	}
	return nil
}

// encodePCMU downsamples 48 kHz stereo to 8 kHz mono (6 stereo pairs → 1
// sample) and mu-law encodes each. Sum across all source samples before the
// single divide keeps every LSB of precision.
func (c *ALSACapture) encodePCMU() []byte {
	const pairsPerSample = 6
	for i := 0; i < PCMUFrameSize; i++ {
		base := i * pairsPerSample * CaptureChannels
		var sum int32
		for j := 0; j < pairsPerSample*CaptureChannels; j++ {
			sum += int32(c.pcm16[base+j])
		}
		c.pcmu[i] = LinearToPCMU(int16(sum / (pairsPerSample * CaptureChannels)))
	}
	return c.pcmu
}

// encodeG722 downsamples 48 kHz stereo to 16 kHz mono (3 stereo pairs → 1
// sample) then runs the G.722 encoder.
func (c *ALSACapture) encodeG722() []byte {
	const pairsPerSample = 3
	for i := 0; i < G722InputFrameSize; i++ {
		base := i * pairsPerSample * CaptureChannels
		var sum int32
		for j := 0; j < pairsPerSample*CaptureChannels; j++ {
			sum += int32(c.pcm16[base+j])
		}
		c.mono16k[i] = int16(sum / (pairsPerSample * CaptureChannels))
	}

	n := c.g722Encoder.Encode(c.g722, c.mono16k)
	return c.g722[:n]
}

func (c *ALSACapture) Close() error {
	if c.handle != nil {
		C.jk_alsa_capture_close((*C.jk_alsa_capture)(c.handle))
		c.handle = nil
	}
	return nil
}
