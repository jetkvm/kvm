//go:build cgo

package audio

import (
	"errors"
	"unsafe"
)

/*
#cgo CFLAGS: -I$HOME/.jetkvm/audio-libs/alsa-lib-$ALSA_VERSION/include -I$HOME/.jetkvm/audio-libs/opus-$OPUS_VERSION/include -I$HOME/.jetkvm/audio-libs/opus-$OPUS_VERSION/celt
#cgo LDFLAGS: -L$HOME/.jetkvm/audio-libs/alsa-lib-$ALSA_VERSION/src/.libs -lasound -L$HOME/.jetkvm/audio-libs/opus-$OPUS_VERSION/.libs -lopus -lm -ldl -static
#include <alsa/asoundlib.h>
#include <opus.h>
#include <stdlib.h>
#include <string.h>
#include <errno.h>
#include <unistd.h>

// C state for ALSA/Opus with safety flags
static snd_pcm_t *pcm_handle = NULL;
static snd_pcm_t *pcm_playback_handle = NULL;
static OpusEncoder *encoder = NULL;
static OpusDecoder *decoder = NULL;
static int opus_bitrate = 64000;
static int opus_complexity = 5;
static int sample_rate = 48000;
static int channels = 2;
static int frame_size = 960; // 20ms for 48kHz
static int max_packet_size = 1500;

// State tracking to prevent race conditions during rapid start/stop
static volatile int capture_initializing = 0;
static volatile int capture_initialized = 0;
static volatile int playback_initializing = 0;
static volatile int playback_initialized = 0;

// Safe ALSA device opening with retry logic
static int safe_alsa_open(snd_pcm_t **handle, const char *device, snd_pcm_stream_t stream) {
	int attempts = 3;
	int err;

	while (attempts-- > 0) {
		err = snd_pcm_open(handle, device, stream, SND_PCM_NONBLOCK);
		if (err >= 0) {
			// Switch to blocking mode after successful open
			snd_pcm_nonblock(*handle, 0);
			return 0;
		}

		if (err == -EBUSY && attempts > 0) {
			// Device busy, wait and retry
			usleep(50000); // 50ms
			continue;
		}
		break;
	}
	return err;
}

// Optimized ALSA configuration with stack allocation and performance tuning
static int configure_alsa_device(snd_pcm_t *handle, const char *device_name) {
	snd_pcm_hw_params_t *params;
	snd_pcm_sw_params_t *sw_params;
	int err;

	if (!handle) return -1;

	// Use stack allocation for better performance
	snd_pcm_hw_params_alloca(&params);
	snd_pcm_sw_params_alloca(&sw_params);

	// Hardware parameters
	err = snd_pcm_hw_params_any(handle, params);
	if (err < 0) return err;

	err = snd_pcm_hw_params_set_access(handle, params, SND_PCM_ACCESS_RW_INTERLEAVED);
	if (err < 0) return err;

	err = snd_pcm_hw_params_set_format(handle, params, SND_PCM_FORMAT_S16_LE);
	if (err < 0) return err;

	err = snd_pcm_hw_params_set_channels(handle, params, channels);
	if (err < 0) return err;

	// Set exact rate for better performance
	err = snd_pcm_hw_params_set_rate(handle, params, sample_rate, 0);
	if (err < 0) {
		// Fallback to near rate if exact fails
		unsigned int rate = sample_rate;
		err = snd_pcm_hw_params_set_rate_near(handle, params, &rate, 0);
		if (err < 0) return err;
	}

	// Optimize buffer sizes for low latency
	snd_pcm_uframes_t period_size = frame_size;
	err = snd_pcm_hw_params_set_period_size_near(handle, params, &period_size, 0);
	if (err < 0) return err;

	// Set buffer size to 4 periods for good latency/stability balance
	snd_pcm_uframes_t buffer_size = period_size * 4;
	err = snd_pcm_hw_params_set_buffer_size_near(handle, params, &buffer_size);
	if (err < 0) return err;

	err = snd_pcm_hw_params(handle, params);
	if (err < 0) return err;

	// Software parameters for optimal performance
	err = snd_pcm_sw_params_current(handle, sw_params);
	if (err < 0) return err;

	// Start playback/capture when buffer is period_size frames
	err = snd_pcm_sw_params_set_start_threshold(handle, sw_params, period_size);
	if (err < 0) return err;

	// Allow transfers when at least period_size frames are available
	err = snd_pcm_sw_params_set_avail_min(handle, sw_params, period_size);
	if (err < 0) return err;

	err = snd_pcm_sw_params(handle, sw_params);
	if (err < 0) return err;

	return snd_pcm_prepare(handle);
}

// Initialize ALSA and Opus encoder with improved safety
int jetkvm_audio_init() {
	int err;

	// Prevent concurrent initialization
	if (__sync_bool_compare_and_swap(&capture_initializing, 0, 1) == 0) {
		return -EBUSY; // Already initializing
	}

	// Check if already initialized
	if (capture_initialized) {
		capture_initializing = 0;
		return 0;
	}

	// Clean up any existing resources first
	if (encoder) {
		opus_encoder_destroy(encoder);
		encoder = NULL;
	}
	if (pcm_handle) {
		snd_pcm_close(pcm_handle);
		pcm_handle = NULL;
	}

	// Try to open ALSA capture device
	err = safe_alsa_open(&pcm_handle, "hw:1,0", SND_PCM_STREAM_CAPTURE);
	if (err < 0) {
		capture_initializing = 0;
		return -1;
	}

	// Configure the device
	err = configure_alsa_device(pcm_handle, "capture");
	if (err < 0) {
		snd_pcm_close(pcm_handle);
		pcm_handle = NULL;
		capture_initializing = 0;
		return -1;
	}

	// Initialize Opus encoder
	int opus_err = 0;
	encoder = opus_encoder_create(sample_rate, channels, OPUS_APPLICATION_AUDIO, &opus_err);
	if (!encoder || opus_err != OPUS_OK) {
		if (pcm_handle) { snd_pcm_close(pcm_handle); pcm_handle = NULL; }
		capture_initializing = 0;
		return -2;
	}

	opus_encoder_ctl(encoder, OPUS_SET_BITRATE(opus_bitrate));
	opus_encoder_ctl(encoder, OPUS_SET_COMPLEXITY(opus_complexity));

	capture_initialized = 1;
	capture_initializing = 0;
	return 0;
}

// Read and encode one frame with enhanced error handling
int jetkvm_audio_read_encode(void *opus_buf) {
	short pcm_buffer[1920]; // max 2ch*960
	unsigned char *out = (unsigned char*)opus_buf;
	int err = 0;

	// Safety checks
	if (!capture_initialized || !pcm_handle || !encoder || !opus_buf) {
		return -1;
	}

	int pcm_rc = snd_pcm_readi(pcm_handle, pcm_buffer, frame_size);

	// Handle ALSA errors with enhanced recovery
	if (pcm_rc < 0) {
		if (pcm_rc == -EPIPE) {
			// Buffer underrun - try to recover
			err = snd_pcm_prepare(pcm_handle);
			if (err < 0) return -1;

			pcm_rc = snd_pcm_readi(pcm_handle, pcm_buffer, frame_size);
			if (pcm_rc < 0) return -1;
		} else if (pcm_rc == -EAGAIN) {
			// No data available - return 0 to indicate no frame
			return 0;
		} else if (pcm_rc == -ESTRPIPE) {
			// Device suspended, try to resume
			while ((err = snd_pcm_resume(pcm_handle)) == -EAGAIN) {
				usleep(1000); // 1ms
			}
			if (err < 0) {
				err = snd_pcm_prepare(pcm_handle);
				if (err < 0) return -1;
			}
			return 0; // Skip this frame
		} else {
			// Other error - return error code
			return -1;
		}
	}

	// If we got fewer frames than expected, pad with silence
	if (pcm_rc < frame_size) {
		memset(&pcm_buffer[pcm_rc * channels], 0, (frame_size - pcm_rc) * channels * sizeof(short));
	}

	int nb_bytes = opus_encode(encoder, pcm_buffer, frame_size, out, max_packet_size);
	return nb_bytes;
}

// Initialize ALSA playback with improved safety
int jetkvm_audio_playback_init() {
	int err;

	// Prevent concurrent initialization
	if (__sync_bool_compare_and_swap(&playback_initializing, 0, 1) == 0) {
		return -EBUSY; // Already initializing
	}

	// Check if already initialized
	if (playback_initialized) {
		playback_initializing = 0;
		return 0;
	}

	// Clean up any existing resources first
	if (decoder) {
		opus_decoder_destroy(decoder);
		decoder = NULL;
	}
	if (pcm_playback_handle) {
		snd_pcm_close(pcm_playback_handle);
		pcm_playback_handle = NULL;
	}

	// Try to open the USB gadget audio device for playback
	err = safe_alsa_open(&pcm_playback_handle, "hw:1,0", SND_PCM_STREAM_PLAYBACK);
	if (err < 0) {
		// Fallback to default device
		err = safe_alsa_open(&pcm_playback_handle, "default", SND_PCM_STREAM_PLAYBACK);
		if (err < 0) {
			playback_initializing = 0;
			return -1;
		}
	}

	// Configure the device
	err = configure_alsa_device(pcm_playback_handle, "playback");
	if (err < 0) {
		snd_pcm_close(pcm_playback_handle);
		pcm_playback_handle = NULL;
		playback_initializing = 0;
		return -1;
	}

	// Initialize Opus decoder
	int opus_err = 0;
	decoder = opus_decoder_create(sample_rate, channels, &opus_err);
	if (!decoder || opus_err != OPUS_OK) {
		snd_pcm_close(pcm_playback_handle);
		pcm_playback_handle = NULL;
		playback_initializing = 0;
		return -2;
	}

	playback_initialized = 1;
	playback_initializing = 0;
	return 0;
}

// Decode Opus and write PCM with enhanced error handling
int jetkvm_audio_decode_write(void *opus_buf, int opus_size) {
	short pcm_buffer[1920]; // max 2ch*960
	unsigned char *in = (unsigned char*)opus_buf;
	int err = 0;

	// Safety checks
	if (!playback_initialized || !pcm_playback_handle || !decoder || !opus_buf || opus_size <= 0) {
		return -1;
	}

	// Additional bounds checking
	if (opus_size > max_packet_size) {
		return -1;
	}

	// Decode Opus to PCM
	int pcm_frames = opus_decode(decoder, in, opus_size, pcm_buffer, frame_size, 0);
	if (pcm_frames < 0) return -1;

	// Write PCM to playback device with enhanced recovery
	int pcm_rc = snd_pcm_writei(pcm_playback_handle, pcm_buffer, pcm_frames);
	if (pcm_rc < 0) {
		if (pcm_rc == -EPIPE) {
			// Buffer underrun - try to recover
			err = snd_pcm_prepare(pcm_playback_handle);
			if (err < 0) return -2;

			pcm_rc = snd_pcm_writei(pcm_playback_handle, pcm_buffer, pcm_frames);
		} else if (pcm_rc == -ESTRPIPE) {
			// Device suspended, try to resume
			while ((err = snd_pcm_resume(pcm_playback_handle)) == -EAGAIN) {
				usleep(1000); // 1ms
			}
			if (err < 0) {
				err = snd_pcm_prepare(pcm_playback_handle);
				if (err < 0) return -2;
			}
			return 0; // Skip this frame
		}
		if (pcm_rc < 0) return -2;
	}

	return pcm_frames;
}

// Safe playback cleanup with double-close protection
void jetkvm_audio_playback_close() {
	// Wait for any ongoing operations to complete
	while (playback_initializing) {
		usleep(1000); // 1ms
	}

	// Atomic check and set to prevent double cleanup
	if (__sync_bool_compare_and_swap(&playback_initialized, 1, 0) == 0) {
		return; // Already cleaned up
	}

	if (decoder) {
		opus_decoder_destroy(decoder);
		decoder = NULL;
	}
	if (pcm_playback_handle) {
		snd_pcm_drain(pcm_playback_handle);
		snd_pcm_close(pcm_playback_handle);
		pcm_playback_handle = NULL;
	}
}

// Safe capture cleanup
void jetkvm_audio_close() {
	// Wait for any ongoing operations to complete
	while (capture_initializing) {
		usleep(1000); // 1ms
	}

	capture_initialized = 0;

	if (encoder) {
		opus_encoder_destroy(encoder);
		encoder = NULL;
	}
	if (pcm_handle) {
		snd_pcm_drop(pcm_handle); // Drop pending samples
		snd_pcm_close(pcm_handle);
		pcm_handle = NULL;
	}

	// Also clean up playback
	jetkvm_audio_playback_close();
}
*/
import "C"

// Optimized Go wrappers with reduced overhead
var (
	errAudioInitFailed   = errors.New("failed to init ALSA/Opus")
	errBufferTooSmall    = errors.New("buffer too small")
	errAudioReadEncode   = errors.New("audio read/encode error")
	errAudioDecodeWrite  = errors.New("audio decode/write error")
	errAudioPlaybackInit = errors.New("failed to init ALSA playback/Opus decoder")
	errEmptyBuffer       = errors.New("empty buffer")
	errNilBuffer         = errors.New("nil buffer")
	errBufferTooLarge    = errors.New("buffer too large")
	errInvalidBufferPtr  = errors.New("invalid buffer pointer")
)

func cgoAudioInit() error {
	ret := C.jetkvm_audio_init()
	if ret != 0 {
		return errAudioInitFailed
	}
	return nil
}

func cgoAudioClose() {
	C.jetkvm_audio_close()
}

func cgoAudioReadEncode(buf []byte) (int, error) {
	if len(buf) < 1276 {
		return 0, errBufferTooSmall
	}

	n := C.jetkvm_audio_read_encode(unsafe.Pointer(&buf[0]))
	if n < 0 {
		return 0, errAudioReadEncode
	}
	if n == 0 {
		return 0, nil // No data available
	}
	return int(n), nil
}

// Audio playback functions
func cgoAudioPlaybackInit() error {
	ret := C.jetkvm_audio_playback_init()
	if ret != 0 {
		return errAudioPlaybackInit
	}
	return nil
}

func cgoAudioPlaybackClose() {
	C.jetkvm_audio_playback_close()
}

func cgoAudioDecodeWrite(buf []byte) (int, error) {
	if len(buf) == 0 {
		return 0, errEmptyBuffer
	}
	if buf == nil {
		return 0, errNilBuffer
	}
	if len(buf) > 4096 {
		return 0, errBufferTooLarge
	}

	bufPtr := unsafe.Pointer(&buf[0])
	if bufPtr == nil {
		return 0, errInvalidBufferPtr
	}

	defer func() {
		if r := recover(); r != nil {
			_ = r
		}
	}()

	n := C.jetkvm_audio_decode_write(bufPtr, C.int(len(buf)))
	if n < 0 {
		return 0, errAudioDecodeWrite
	}
	return int(n), nil
}

// CGO function aliases
var (
	CGOAudioInit          = cgoAudioInit
	CGOAudioClose         = cgoAudioClose
	CGOAudioReadEncode    = cgoAudioReadEncode
	CGOAudioPlaybackInit  = cgoAudioPlaybackInit
	CGOAudioPlaybackClose = cgoAudioPlaybackClose
	CGOAudioDecodeWrite   = cgoAudioDecodeWrite
)
