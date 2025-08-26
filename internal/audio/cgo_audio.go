//go:build cgo

package audio

import (
	"errors"
	"fmt"
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
// Opus encoder settings - initialized from Go configuration
static int opus_bitrate = 96000;        // Will be set from GetConfig().CGOOpusBitrate
static int opus_complexity = 3;         // Will be set from GetConfig().CGOOpusComplexity
static int opus_vbr = 1;                // Will be set from GetConfig().CGOOpusVBR
static int opus_vbr_constraint = 1;     // Will be set from GetConfig().CGOOpusVBRConstraint
static int opus_signal_type = 3;        // Will be set from GetConfig().CGOOpusSignalType
static int opus_bandwidth = 1105;       // Will be set from GetConfig().CGOOpusBandwidth
static int opus_dtx = 0;                // Will be set from GetConfig().CGOOpusDTX
static int sample_rate = 48000;         // Will be set from GetConfig().CGOSampleRate
static int channels = 2;                // Will be set from GetConfig().CGOChannels
static int frame_size = 960;            // Will be set from GetConfig().CGOFrameSize
static int max_packet_size = 1500;      // Will be set from GetConfig().CGOMaxPacketSize
static int sleep_microseconds = 1000;   // Will be set from GetConfig().CGOUsleepMicroseconds

// Function to update constants from Go configuration
void update_audio_constants(int bitrate, int complexity, int vbr, int vbr_constraint,
                           int signal_type, int bandwidth, int dtx, int sr, int ch,
                           int fs, int max_pkt, int sleep_us) {
    opus_bitrate = bitrate;
    opus_complexity = complexity;
    opus_vbr = vbr;
    opus_vbr_constraint = vbr_constraint;
    opus_signal_type = signal_type;
    opus_bandwidth = bandwidth;
    opus_dtx = dtx;
    sample_rate = sr;
    channels = ch;
    frame_size = fs;
    max_packet_size = max_pkt;
    sleep_microseconds = sleep_us;
}

// State tracking to prevent race conditions during rapid start/stop
static volatile int capture_initializing = 0;
static volatile int capture_initialized = 0;
static volatile int playback_initializing = 0;
static volatile int playback_initialized = 0;

// Enhanced ALSA device opening with exponential backoff retry logic
static int safe_alsa_open(snd_pcm_t **handle, const char *device, snd_pcm_stream_t stream) {
	int max_attempts = 5; // Increased from 3 to 5
	int attempt = 0;
	int err;
	int backoff_us = sleep_microseconds; // Start with base sleep time
	const int max_backoff_us = 500000; // Max 500ms backoff

	while (attempt < max_attempts) {
		err = snd_pcm_open(handle, device, stream, SND_PCM_NONBLOCK);
		if (err >= 0) {
			// Switch to blocking mode after successful open
			snd_pcm_nonblock(*handle, 0);
			return 0;
		}

		attempt++;
		if (attempt >= max_attempts) break;

		// Enhanced error handling with specific retry strategies
		if (err == -EBUSY || err == -EAGAIN) {
			// Device busy or temporarily unavailable - retry with backoff
			usleep(backoff_us);
			backoff_us = (backoff_us * 2 < max_backoff_us) ? backoff_us * 2 : max_backoff_us;
		} else if (err == -ENODEV || err == -ENOENT) {
			// Device not found - longer wait as device might be initializing
			usleep(backoff_us * 2);
			backoff_us = (backoff_us * 2 < max_backoff_us) ? backoff_us * 2 : max_backoff_us;
		} else if (err == -EPERM || err == -EACCES) {
			// Permission denied - shorter wait, likely persistent issue
			usleep(backoff_us / 2);
		} else {
			// Other errors - standard backoff
			usleep(backoff_us);
			backoff_us = (backoff_us * 2 < max_backoff_us) ? backoff_us * 2 : max_backoff_us;
		}
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

	// Initialize Opus encoder with optimized settings
	int opus_err = 0;
	encoder = opus_encoder_create(sample_rate, channels, OPUS_APPLICATION_AUDIO, &opus_err);
	if (!encoder || opus_err != OPUS_OK) {
		if (pcm_handle) { snd_pcm_close(pcm_handle); pcm_handle = NULL; }
		capture_initializing = 0;
		return -2;
	}

	// Apply optimized Opus encoder settings
	opus_encoder_ctl(encoder, OPUS_SET_BITRATE(opus_bitrate));
	opus_encoder_ctl(encoder, OPUS_SET_COMPLEXITY(opus_complexity));
	opus_encoder_ctl(encoder, OPUS_SET_VBR(opus_vbr));
	opus_encoder_ctl(encoder, OPUS_SET_VBR_CONSTRAINT(opus_vbr_constraint));
	opus_encoder_ctl(encoder, OPUS_SET_SIGNAL(opus_signal_type));
	opus_encoder_ctl(encoder, OPUS_SET_BANDWIDTH(opus_bandwidth));
	opus_encoder_ctl(encoder, OPUS_SET_DTX(opus_dtx));
	// Enable packet loss concealment for better resilience
	opus_encoder_ctl(encoder, OPUS_SET_PACKET_LOSS_PERC(5));
	// Set prediction disabled for lower latency
	opus_encoder_ctl(encoder, OPUS_SET_PREDICTION_DISABLED(1));

	capture_initialized = 1;
	capture_initializing = 0;
	return 0;
}

// jetkvm_audio_read_encode reads one audio frame from ALSA, encodes it with Opus, and handles errors.
//
// This function implements a robust audio capture pipeline with the following features:
// - ALSA PCM capture with automatic device recovery
// - Opus encoding with optimized settings for real-time processing
// - Progressive error recovery with exponential backoff
// - Buffer underrun and device suspension handling
//
// Error Recovery Strategy:
// 1. EPIPE (buffer underrun): Prepare device and retry with progressive delays
// 2. ESTRPIPE (device suspended): Resume device with timeout and fallback to prepare
// 3. Other errors: Log and attempt recovery up to max_recovery_attempts
//
// Performance Optimizations:
// - Stack-allocated PCM buffer to avoid heap allocations
// - Direct memory access for Opus encoding
// - Minimal system calls in the hot path
//
// Parameters:
//   opus_buf: Output buffer for encoded Opus data (must be at least max_packet_size bytes)
//
// Returns:
//   >0: Number of bytes written to opus_buf
//   -1: Initialization error or safety check failure
//   -2: Unrecoverable ALSA or Opus error after all retry attempts
int jetkvm_audio_read_encode(void *opus_buf) {
	short pcm_buffer[1920]; // max 2ch*960
	unsigned char *out = (unsigned char*)opus_buf;
	int err = 0;
	int recovery_attempts = 0;
	const int max_recovery_attempts = 3;

	// Safety checks
	if (!capture_initialized || !pcm_handle || !encoder || !opus_buf) {
		return -1;
	}

retry_read:
	;
	int pcm_rc = snd_pcm_readi(pcm_handle, pcm_buffer, frame_size);

	// Handle ALSA errors with robust recovery strategies
	if (pcm_rc < 0) {
		if (pcm_rc == -EPIPE) {
			// Buffer underrun - implement progressive recovery
			recovery_attempts++;
			if (recovery_attempts > max_recovery_attempts) {
				return -1; // Give up after max attempts
			}

			// Try to recover with prepare
			err = snd_pcm_prepare(pcm_handle);
			if (err < 0) {
				// If prepare fails, try drop and prepare
				snd_pcm_drop(pcm_handle);
				err = snd_pcm_prepare(pcm_handle);
				if (err < 0) return -1;
			}

			// Wait before retry to allow device to stabilize
			usleep(sleep_microseconds * recovery_attempts);
			goto retry_read;
		} else if (pcm_rc == -EAGAIN) {
			// No data available - return 0 to indicate no frame
			return 0;
		} else if (pcm_rc == -ESTRPIPE) {
			// Device suspended, implement robust resume logic
			recovery_attempts++;
			if (recovery_attempts > max_recovery_attempts) {
				return -1;
			}

			// Try to resume with timeout
			int resume_attempts = 0;
			while ((err = snd_pcm_resume(pcm_handle)) == -EAGAIN && resume_attempts < 10) {
				usleep(sleep_microseconds);
				resume_attempts++;
			}
			if (err < 0) {
				// Resume failed, try prepare as fallback
				err = snd_pcm_prepare(pcm_handle);
				if (err < 0) return -1;
			}
			// Wait before retry to allow device to stabilize
			usleep(sleep_microseconds * recovery_attempts);
			return 0; // Skip this frame but don't fail
		} else if (pcm_rc == -ENODEV) {
			// Device disconnected - critical error
			return -1;
		} else if (pcm_rc == -EIO) {
			// I/O error - try recovery once
			recovery_attempts++;
			if (recovery_attempts <= max_recovery_attempts) {
				snd_pcm_drop(pcm_handle);
				err = snd_pcm_prepare(pcm_handle);
				if (err >= 0) {
					usleep(sleep_microseconds);
					goto retry_read;
				}
			}
			return -1;
		} else {
			// Other errors - limited retry for transient issues
			recovery_attempts++;
			if (recovery_attempts <= 1 && (pcm_rc == -EINTR || pcm_rc == -EBUSY)) {
				usleep(sleep_microseconds / 2);
				goto retry_read;
			}
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

// jetkvm_audio_decode_write decodes Opus data and writes PCM to ALSA playback device.
//
// This function implements a robust audio playback pipeline with the following features:
// - Opus decoding with packet loss concealment
// - ALSA PCM playback with automatic device recovery
// - Progressive error recovery with exponential backoff
// - Buffer underrun and device suspension handling
//
// Error Recovery Strategy:
// 1. EPIPE (buffer underrun): Prepare device, optionally drop+prepare, retry with delays
// 2. ESTRPIPE (device suspended): Resume with timeout, fallback to prepare if needed
// 3. Opus decode errors: Attempt packet loss concealment before failing
//
// Performance Optimizations:
// - Stack-allocated PCM buffer to minimize heap allocations
// - Bounds checking to prevent buffer overruns
// - Direct ALSA device access for minimal latency
//
// Parameters:
//   opus_buf: Input buffer containing Opus-encoded audio data
//   opus_size: Size of the Opus data in bytes (must be > 0 and <= max_packet_size)
//
// Returns:
//   0: Success - audio frame decoded and written to playback device
//   -1: Invalid parameters, initialization error, or bounds check failure
//   -2: Unrecoverable ALSA or Opus error after all retry attempts
int jetkvm_audio_decode_write(void *opus_buf, int opus_size) {
	short pcm_buffer[1920]; // max 2ch*960
	unsigned char *in = (unsigned char*)opus_buf;
	int err = 0;
	int recovery_attempts = 0;
	const int max_recovery_attempts = 3;

	// Safety checks
	if (!playback_initialized || !pcm_playback_handle || !decoder || !opus_buf || opus_size <= 0) {
		return -1;
	}

	// Additional bounds checking
	if (opus_size > max_packet_size) {
		return -1;
	}

	// Decode Opus to PCM with error handling
	int pcm_frames = opus_decode(decoder, in, opus_size, pcm_buffer, frame_size, 0);
	if (pcm_frames < 0) {
		// Try packet loss concealment on decode error
		pcm_frames = opus_decode(decoder, NULL, 0, pcm_buffer, frame_size, 0);
		if (pcm_frames < 0) return -1;
	}

retry_write:
	;
	// Write PCM to playback device with robust recovery
	int pcm_rc = snd_pcm_writei(pcm_playback_handle, pcm_buffer, pcm_frames);
	if (pcm_rc < 0) {
		if (pcm_rc == -EPIPE) {
			// Buffer underrun - implement progressive recovery
			recovery_attempts++;
			if (recovery_attempts > max_recovery_attempts) {
				return -2;
			}

			// Try to recover with prepare
			err = snd_pcm_prepare(pcm_playback_handle);
			if (err < 0) {
				// If prepare fails, try drop and prepare
				snd_pcm_drop(pcm_playback_handle);
				err = snd_pcm_prepare(pcm_playback_handle);
				if (err < 0) return -2;
			}

			// Wait before retry to allow device to stabilize
			usleep(sleep_microseconds * recovery_attempts);
			goto retry_write;
		} else if (pcm_rc == -ESTRPIPE) {
			// Device suspended, implement robust resume logic
			recovery_attempts++;
			if (recovery_attempts > max_recovery_attempts) {
				return -2;
			}

			// Try to resume with timeout
			int resume_attempts = 0;
			while ((err = snd_pcm_resume(pcm_playback_handle)) == -EAGAIN && resume_attempts < 10) {
				usleep(sleep_microseconds);
				resume_attempts++;
			}
			if (err < 0) {
				// Resume failed, try prepare as fallback
				err = snd_pcm_prepare(pcm_playback_handle);
				if (err < 0) return -2;
			}
			// Wait before retry to allow device to stabilize
			usleep(sleep_microseconds * recovery_attempts);
			return 0; // Skip this frame but don't fail
		} else if (pcm_rc == -ENODEV) {
			// Device disconnected - critical error
			return -2;
		} else if (pcm_rc == -EIO) {
			// I/O error - try recovery once
			recovery_attempts++;
			if (recovery_attempts <= max_recovery_attempts) {
				snd_pcm_drop(pcm_playback_handle);
				err = snd_pcm_prepare(pcm_playback_handle);
				if (err >= 0) {
					usleep(sleep_microseconds);
					goto retry_write;
				}
			}
			return -2;
		} else if (pcm_rc == -EAGAIN) {
			// Device not ready - brief wait and retry
			recovery_attempts++;
			if (recovery_attempts <= max_recovery_attempts) {
				usleep(sleep_microseconds / 4);
				goto retry_write;
			}
			return -2;
		} else {
			// Other errors - limited retry for transient issues
			recovery_attempts++;
			if (recovery_attempts <= 1 && (pcm_rc == -EINTR || pcm_rc == -EBUSY)) {
				usleep(sleep_microseconds / 2);
				goto retry_write;
			}
			return -2;
		}
	}

	return pcm_frames;
}

// Safe playback cleanup with double-close protection
void jetkvm_audio_playback_close() {
	// Wait for any ongoing operations to complete
	while (playback_initializing) {
		usleep(sleep_microseconds); // Use centralized constant
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
		usleep(sleep_microseconds); // Use centralized constant
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
	// Base error types for wrapping with context
	errAudioInitFailed   = errors.New("failed to init ALSA/Opus")
	errAudioReadEncode   = errors.New("audio read/encode error")
	errAudioDecodeWrite  = errors.New("audio decode/write error")
	errAudioPlaybackInit = errors.New("failed to init ALSA playback/Opus decoder")
	errEmptyBuffer       = errors.New("empty buffer")
	errNilBuffer         = errors.New("nil buffer")
	errInvalidBufferPtr  = errors.New("invalid buffer pointer")
)

// Error creation functions with context
func newBufferTooSmallError(actual, required int) error {
	return fmt.Errorf("buffer too small: got %d bytes, need at least %d bytes", actual, required)
}

func newBufferTooLargeError(actual, max int) error {
	return fmt.Errorf("buffer too large: got %d bytes, maximum allowed %d bytes", actual, max)
}

func newAudioInitError(cErrorCode int) error {
	return fmt.Errorf("%w: C error code %d", errAudioInitFailed, cErrorCode)
}

func newAudioPlaybackInitError(cErrorCode int) error {
	return fmt.Errorf("%w: C error code %d", errAudioPlaybackInit, cErrorCode)
}

func newAudioReadEncodeError(cErrorCode int) error {
	return fmt.Errorf("%w: C error code %d", errAudioReadEncode, cErrorCode)
}

func newAudioDecodeWriteError(cErrorCode int) error {
	return fmt.Errorf("%w: C error code %d", errAudioDecodeWrite, cErrorCode)
}

func cgoAudioInit() error {
	// Update C constants from Go configuration
	config := GetConfig()
	C.update_audio_constants(
		C.int(config.CGOOpusBitrate),
		C.int(config.CGOOpusComplexity),
		C.int(config.CGOOpusVBR),
		C.int(config.CGOOpusVBRConstraint),
		C.int(config.CGOOpusSignalType),
		C.int(config.CGOOpusBandwidth),
		C.int(config.CGOOpusDTX),
		C.int(config.CGOSampleRate),
		C.int(config.CGOChannels),
		C.int(config.CGOFrameSize),
		C.int(config.CGOMaxPacketSize),
		C.int(config.CGOUsleepMicroseconds),
	)

	result := C.jetkvm_audio_init()
	if result != 0 {
		return newAudioInitError(int(result))
	}
	return nil
}

func cgoAudioClose() {
	C.jetkvm_audio_close()
}

func cgoAudioReadEncode(buf []byte) (int, error) {
	minRequired := GetConfig().MinReadEncodeBuffer
	if len(buf) < minRequired {
		return 0, newBufferTooSmallError(len(buf), minRequired)
	}

	n := C.jetkvm_audio_read_encode(unsafe.Pointer(&buf[0]))
	if n < 0 {
		return 0, newAudioReadEncodeError(int(n))
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
		return newAudioPlaybackInitError(int(ret))
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
	maxAllowed := GetConfig().MaxDecodeWriteBuffer
	if len(buf) > maxAllowed {
		return 0, newBufferTooLargeError(len(buf), maxAllowed)
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
		return 0, newAudioDecodeWriteError(int(n))
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
