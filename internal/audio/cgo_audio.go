package audio

import (
	"errors"
	"unsafe"
)

/*
#cgo CFLAGS: -I${SRCDIR}/../../tools/alsa-opus-includes
#cgo LDFLAGS: -L$HOME/.jetkvm/audio-libs/alsa-lib-1.2.14/src/.libs -lasound -L$HOME/.jetkvm/audio-libs/opus-1.5.2/.libs -lopus -lm -ldl -static
#include <alsa/asoundlib.h>
#include <opus.h>
#include <stdlib.h>
#include <string.h>

// C state for ALSA/Opus
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
	
	// Handle ALSA errors with recovery
	if (pcm_rc < 0) {
		if (pcm_rc == -EPIPE) {
			// Buffer underrun - try to recover
			snd_pcm_prepare(pcm_handle);
			pcm_rc = snd_pcm_readi(pcm_handle, pcm_buffer, frame_size);
			if (pcm_rc < 0) return -1;
		} else if (pcm_rc == -EAGAIN) {
			// No data available - return 0 to indicate no frame
			return 0;
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

// Initialize ALSA playback for microphone input (browser -> USB gadget)
int jetkvm_audio_playback_init() {
	int err;
	snd_pcm_hw_params_t *params;
	if (pcm_playback_handle) return 0;
	
	// Try to open the USB gadget audio device for playback
	// This should correspond to the capture endpoint of the USB gadget
	if (snd_pcm_open(&pcm_playback_handle, "hw:1,0", SND_PCM_STREAM_PLAYBACK, 0) < 0) {
		// Fallback to default device if hw:1,0 doesn't work for playback
		if (snd_pcm_open(&pcm_playback_handle, "default", SND_PCM_STREAM_PLAYBACK, 0) < 0)
			return -1;
	}
	
	snd_pcm_hw_params_malloc(&params);
	snd_pcm_hw_params_any(pcm_playback_handle, params);
	snd_pcm_hw_params_set_access(pcm_playback_handle, params, SND_PCM_ACCESS_RW_INTERLEAVED);
	snd_pcm_hw_params_set_format(pcm_playback_handle, params, SND_PCM_FORMAT_S16_LE);
	snd_pcm_hw_params_set_channels(pcm_playback_handle, params, channels);
	snd_pcm_hw_params_set_rate(pcm_playback_handle, params, sample_rate, 0);
	snd_pcm_hw_params_set_period_size(pcm_playback_handle, params, frame_size, 0);
	snd_pcm_hw_params(pcm_playback_handle, params);
	snd_pcm_hw_params_free(params);
	snd_pcm_prepare(pcm_playback_handle);
	
	// Initialize Opus decoder
	decoder = opus_decoder_create(sample_rate, channels, &err);
	if (!decoder) return -2;
	
	return 0;
}

// Decode Opus and write PCM to playback device
int jetkvm_audio_decode_write(void *opus_buf, int opus_size) {
	short pcm_buffer[1920]; // max 2ch*960
	unsigned char *in = (unsigned char*)opus_buf;
	
	// Decode Opus to PCM
	int pcm_frames = opus_decode(decoder, in, opus_size, pcm_buffer, frame_size, 0);
	if (pcm_frames < 0) return -1;
	
	// Write PCM to playback device
	int pcm_rc = snd_pcm_writei(pcm_playback_handle, pcm_buffer, pcm_frames);
	if (pcm_rc < 0) {
		// Try to recover from underrun
		if (pcm_rc == -EPIPE) {
			snd_pcm_prepare(pcm_playback_handle);
			pcm_rc = snd_pcm_writei(pcm_playback_handle, pcm_buffer, pcm_frames);
		}
		if (pcm_rc < 0) return -2;
	}
	
	return pcm_frames;
}

void jetkvm_audio_playback_close() {
	if (decoder) { opus_decoder_destroy(decoder); decoder = NULL; }
	if (pcm_playback_handle) { snd_pcm_close(pcm_playback_handle); pcm_playback_handle = NULL; }
}

void jetkvm_audio_close() {
	if (encoder) { opus_encoder_destroy(encoder); encoder = NULL; }
	if (pcm_handle) { snd_pcm_close(pcm_handle); pcm_handle = NULL; }
	jetkvm_audio_playback_close();
}
*/
import "C"



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
	if n == 0 {
		// No data available - this is not an error, just no audio frame
		return 0, nil
	}
	return int(n), nil
}



// Go wrappers for audio playback (microphone input)
func cgoAudioPlaybackInit() error {
	ret := C.jetkvm_audio_playback_init()
	if ret != 0 {
		return errors.New("failed to init ALSA playback/Opus decoder")
	}
	return nil
}

func cgoAudioPlaybackClose() {
	C.jetkvm_audio_playback_close()
}

// Decodes Opus frame and writes to playback device
func cgoAudioDecodeWrite(buf []byte) (int, error) {
	if len(buf) == 0 {
		return 0, errors.New("empty buffer")
	}
	n := C.jetkvm_audio_decode_write(unsafe.Pointer(&buf[0]), C.int(len(buf)))
	if n < 0 {
		return 0, errors.New("audio decode/write error")
	}
	return int(n), nil
}



// Wrapper functions for non-blocking audio manager
func CGOAudioInit() error {
	return cgoAudioInit()
}

func CGOAudioClose() {
	cgoAudioClose()
}

func CGOAudioReadEncode(buf []byte) (int, error) {
	return cgoAudioReadEncode(buf)
}

func CGOAudioPlaybackInit() error {
	return cgoAudioPlaybackInit()
}

func CGOAudioPlaybackClose() {
	cgoAudioPlaybackClose()
}

func CGOAudioDecodeWrite(buf []byte) (int, error) {
	return cgoAudioDecodeWrite(buf)
}
