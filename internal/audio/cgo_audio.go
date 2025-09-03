//go:build cgo

package audio

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
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
static int max_attempts_global = 5;     // Will be set from GetConfig().CGOMaxAttempts
static int max_backoff_us_global = 500000; // Will be set from GetConfig().CGOMaxBackoffMicroseconds

// Function to update constants from Go configuration
void update_audio_constants(int bitrate, int complexity, int vbr, int vbr_constraint,
                           int signal_type, int bandwidth, int dtx, int sr, int ch,
                           int fs, int max_pkt, int sleep_us, int max_attempts, int max_backoff) {
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
    max_attempts_global = max_attempts;
    max_backoff_us_global = max_backoff;
}

// State tracking to prevent race conditions during rapid start/stop
static volatile int capture_initializing = 0;
static volatile int capture_initialized = 0;
static volatile int playback_initializing = 0;
static volatile int playback_initialized = 0;

// Function to dynamically update Opus encoder parameters
int update_opus_encoder_params(int bitrate, int complexity, int vbr, int vbr_constraint,
                              int signal_type, int bandwidth, int dtx) {
    if (!encoder || !capture_initialized) {
        return -1; // Encoder not initialized
    }

    // Update the static variables
    opus_bitrate = bitrate;
    opus_complexity = complexity;
    opus_vbr = vbr;
    opus_vbr_constraint = vbr_constraint;
    opus_signal_type = signal_type;
    opus_bandwidth = bandwidth;
    opus_dtx = dtx;

    // Apply the new settings to the encoder
    int result = 0;
    result |= opus_encoder_ctl(encoder, OPUS_SET_BITRATE(opus_bitrate));
    result |= opus_encoder_ctl(encoder, OPUS_SET_COMPLEXITY(opus_complexity));
    result |= opus_encoder_ctl(encoder, OPUS_SET_VBR(opus_vbr));
    result |= opus_encoder_ctl(encoder, OPUS_SET_VBR_CONSTRAINT(opus_vbr_constraint));
    result |= opus_encoder_ctl(encoder, OPUS_SET_SIGNAL(opus_signal_type));
    result |= opus_encoder_ctl(encoder, OPUS_SET_BANDWIDTH(opus_bandwidth));
    result |= opus_encoder_ctl(encoder, OPUS_SET_DTX(opus_dtx));

    return result; // 0 on success, non-zero on error
}

// Enhanced ALSA device opening with exponential backoff retry logic
static int safe_alsa_open(snd_pcm_t **handle, const char *device, snd_pcm_stream_t stream) {
	int attempt = 0;
	int err;
	int backoff_us = sleep_microseconds; // Start with base sleep time

	while (attempt < max_attempts_global) {
		err = snd_pcm_open(handle, device, stream, SND_PCM_NONBLOCK);
		if (err >= 0) {
			// Switch to blocking mode after successful open
			snd_pcm_nonblock(*handle, 0);
			return 0;
		}

		attempt++;
		if (attempt >= max_attempts_global) break;

		// Enhanced error handling with specific retry strategies
		if (err == -EBUSY || err == -EAGAIN) {
			// Device busy or temporarily unavailable - retry with backoff
			usleep(backoff_us);
			backoff_us = (backoff_us * 2 < max_backoff_us_global) ? backoff_us * 2 : max_backoff_us_global;
		} else if (err == -ENODEV || err == -ENOENT) {
			// Device not found - longer wait as device might be initializing
			usleep(backoff_us * 2);
			backoff_us = (backoff_us * 2 < max_backoff_us_global) ? backoff_us * 2 : max_backoff_us_global;
		} else if (err == -EPERM || err == -EACCES) {
			// Permission denied - shorter wait, likely persistent issue
			usleep(backoff_us / 2);
		} else {
			// Other errors - standard backoff
			usleep(backoff_us);
			backoff_us = (backoff_us * 2 < max_backoff_us_global) ? backoff_us * 2 : max_backoff_us_global;
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

// jetkvm_audio_read_encode captures audio from ALSA, encodes with Opus, and handles errors.
// Implements robust error recovery for buffer underruns and device suspension.
// Returns: >0 (bytes written), -1 (init error), -2 (unrecoverable error)
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

// Error creation functions with enhanced context
func newBufferTooSmallError(actual, required int) error {
	baseErr := fmt.Errorf("buffer too small: got %d bytes, need at least %d bytes", actual, required)
	return WrapWithMetadata(baseErr, "cgo_audio", "buffer_validation", map[string]interface{}{
		"actual_size":   actual,
		"required_size": required,
		"error_type":    "buffer_undersize",
	})
}

func newBufferTooLargeError(actual, max int) error {
	baseErr := fmt.Errorf("buffer too large: got %d bytes, maximum allowed %d bytes", actual, max)
	return WrapWithMetadata(baseErr, "cgo_audio", "buffer_validation", map[string]interface{}{
		"actual_size": actual,
		"max_size":    max,
		"error_type":  "buffer_oversize",
	})
}

func newAudioInitError(cErrorCode int) error {
	baseErr := fmt.Errorf("%w: C error code %d", errAudioInitFailed, cErrorCode)
	return WrapWithMetadata(baseErr, "cgo_audio", "initialization", map[string]interface{}{
		"c_error_code": cErrorCode,
		"error_type":   "init_failure",
		"severity":     "critical",
	})
}

func newAudioPlaybackInitError(cErrorCode int) error {
	baseErr := fmt.Errorf("%w: C error code %d", errAudioPlaybackInit, cErrorCode)
	return WrapWithMetadata(baseErr, "cgo_audio", "playback_init", map[string]interface{}{
		"c_error_code": cErrorCode,
		"error_type":   "playback_init_failure",
		"severity":     "high",
	})
}

func newAudioReadEncodeError(cErrorCode int) error {
	baseErr := fmt.Errorf("%w: C error code %d", errAudioReadEncode, cErrorCode)
	return WrapWithMetadata(baseErr, "cgo_audio", "read_encode", map[string]interface{}{
		"c_error_code": cErrorCode,
		"error_type":   "read_encode_failure",
		"severity":     "medium",
	})
}

func newAudioDecodeWriteError(cErrorCode int) error {
	baseErr := fmt.Errorf("%w: C error code %d", errAudioDecodeWrite, cErrorCode)
	return WrapWithMetadata(baseErr, "cgo_audio", "decode_write", map[string]interface{}{
		"c_error_code": cErrorCode,
		"error_type":   "decode_write_failure",
		"severity":     "medium",
	})
}

func cgoAudioInit() error {
	// Get cached config and ensure it's updated
	cache := GetCachedConfig()
	cache.Update()

	// Update C constants from cached config (atomic access, no locks)
	C.update_audio_constants(
		C.int(cache.opusBitrate.Load()),
		C.int(cache.opusComplexity.Load()),
		C.int(cache.opusVBR.Load()),
		C.int(cache.opusVBRConstraint.Load()),
		C.int(cache.opusSignalType.Load()),
		C.int(cache.opusBandwidth.Load()),
		C.int(cache.opusDTX.Load()),
		C.int(cache.sampleRate.Load()),
		C.int(cache.channels.Load()),
		C.int(cache.frameSize.Load()),
		C.int(cache.maxPacketSize.Load()),
		C.int(GetConfig().CGOUsleepMicroseconds),
		C.int(GetConfig().CGOMaxAttempts),
		C.int(GetConfig().CGOMaxBackoffMicroseconds),
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

// AudioConfigCache provides a comprehensive caching system for audio configuration
// to minimize GetConfig() calls in the hot path
type AudioConfigCache struct {
	// Atomic fields for lock-free access to frequently used values
	minReadEncodeBuffer  atomic.Int32
	maxDecodeWriteBuffer atomic.Int32
	maxPacketSize        atomic.Int32
	maxPCMBufferSize     atomic.Int32
	opusBitrate          atomic.Int32
	opusComplexity       atomic.Int32
	opusVBR              atomic.Int32
	opusVBRConstraint    atomic.Int32
	opusSignalType       atomic.Int32
	opusBandwidth        atomic.Int32
	opusDTX              atomic.Int32
	sampleRate           atomic.Int32
	channels             atomic.Int32
	frameSize            atomic.Int32

	// Additional cached values for validation functions
	maxAudioFrameSize atomic.Int32
	maxChannels       atomic.Int32
	minFrameDuration  atomic.Int64 // Store as nanoseconds
	maxFrameDuration  atomic.Int64 // Store as nanoseconds
	minOpusBitrate    atomic.Int32
	maxOpusBitrate    atomic.Int32

	// Batch processing related values
	BatchProcessingTimeout       time.Duration
	BatchProcessorFramesPerBatch int
	BatchProcessorTimeout        time.Duration
	BatchProcessingDelay         time.Duration
	MinBatchSizeForThreadPinning int

	// Mutex for updating the cache
	mutex       sync.RWMutex
	lastUpdate  time.Time
	cacheExpiry time.Duration
	initialized atomic.Bool

	// Pre-allocated errors to avoid allocations in hot path
	bufferTooSmallReadEncode  error
	bufferTooLargeDecodeWrite error
}

// Global audio config cache instance
var globalAudioConfigCache = &AudioConfigCache{
	cacheExpiry: 30 * time.Second, // Increased from 10s to 30s to further reduce cache updates
}

// GetCachedConfig returns the global audio config cache instance
func GetCachedConfig() *AudioConfigCache {
	return globalAudioConfigCache
}

// Update refreshes the cached config values if needed
func (c *AudioConfigCache) Update() {
	// Fast path: if cache is initialized and not expired, return immediately
	if c.initialized.Load() {
		c.mutex.RLock()
		cacheExpired := time.Since(c.lastUpdate) > c.cacheExpiry
		c.mutex.RUnlock()
		if !cacheExpired {
			return
		}
	}

	// Slow path: update cache
	c.mutex.Lock()
	defer c.mutex.Unlock()

	// Double-check after acquiring lock
	if !c.initialized.Load() || time.Since(c.lastUpdate) > c.cacheExpiry {
		config := GetConfig() // Call GetConfig() only once

		// Update atomic values for lock-free access - CGO values
		c.minReadEncodeBuffer.Store(int32(config.MinReadEncodeBuffer))
		c.maxDecodeWriteBuffer.Store(int32(config.MaxDecodeWriteBuffer))
		c.maxPacketSize.Store(int32(config.CGOMaxPacketSize))
		c.maxPCMBufferSize.Store(int32(config.MaxPCMBufferSize))
		c.opusBitrate.Store(int32(config.CGOOpusBitrate))
		c.opusComplexity.Store(int32(config.CGOOpusComplexity))
		c.opusVBR.Store(int32(config.CGOOpusVBR))
		c.opusVBRConstraint.Store(int32(config.CGOOpusVBRConstraint))
		c.opusSignalType.Store(int32(config.CGOOpusSignalType))
		c.opusBandwidth.Store(int32(config.CGOOpusBandwidth))
		c.opusDTX.Store(int32(config.CGOOpusDTX))
		c.sampleRate.Store(int32(config.CGOSampleRate))
		c.channels.Store(int32(config.CGOChannels))
		c.frameSize.Store(int32(config.CGOFrameSize))

		// Update additional validation values
		c.maxAudioFrameSize.Store(int32(config.MaxAudioFrameSize))
		c.maxChannels.Store(int32(config.MaxChannels))
		c.minFrameDuration.Store(int64(config.MinFrameDuration))
		c.maxFrameDuration.Store(int64(config.MaxFrameDuration))
		c.minOpusBitrate.Store(int32(config.MinOpusBitrate))
		c.maxOpusBitrate.Store(int32(config.MaxOpusBitrate))

		// Update batch processing related values
		c.BatchProcessingTimeout = 100 * time.Millisecond // Fixed timeout for batch processing
		c.BatchProcessorFramesPerBatch = config.BatchProcessorFramesPerBatch
		c.BatchProcessorTimeout = config.BatchProcessorTimeout
		c.BatchProcessingDelay = config.BatchProcessingDelay
		c.MinBatchSizeForThreadPinning = config.MinBatchSizeForThreadPinning

		// Pre-allocate common errors
		c.bufferTooSmallReadEncode = newBufferTooSmallError(0, config.MinReadEncodeBuffer)
		c.bufferTooLargeDecodeWrite = newBufferTooLargeError(config.MaxDecodeWriteBuffer+1, config.MaxDecodeWriteBuffer)

		c.lastUpdate = time.Now()
		c.initialized.Store(true)

		// Update the global validation cache as well
		if cachedMaxFrameSize != 0 {
			cachedMaxFrameSize = config.MaxAudioFrameSize
		}
	}
}

// GetMinReadEncodeBuffer returns the cached MinReadEncodeBuffer value
func (c *AudioConfigCache) GetMinReadEncodeBuffer() int {
	return int(c.minReadEncodeBuffer.Load())
}

// GetMaxDecodeWriteBuffer returns the cached MaxDecodeWriteBuffer value
func (c *AudioConfigCache) GetMaxDecodeWriteBuffer() int {
	return int(c.maxDecodeWriteBuffer.Load())
}

// GetMaxPacketSize returns the cached MaxPacketSize value
func (c *AudioConfigCache) GetMaxPacketSize() int {
	return int(c.maxPacketSize.Load())
}

// GetMaxPCMBufferSize returns the cached MaxPCMBufferSize value
func (c *AudioConfigCache) GetMaxPCMBufferSize() int {
	return int(c.maxPCMBufferSize.Load())
}

// GetBufferTooSmallError returns the pre-allocated buffer too small error
func (c *AudioConfigCache) GetBufferTooSmallError() error {
	return c.bufferTooSmallReadEncode
}

// GetBufferTooLargeError returns the pre-allocated buffer too large error
func (c *AudioConfigCache) GetBufferTooLargeError() error {
	return c.bufferTooLargeDecodeWrite
}

// Removed duplicate config caching system - using AudioConfigCache instead

func cgoAudioReadEncode(buf []byte) (int, error) {
	// Fast path: Use AudioConfigCache to avoid GetConfig() in hot path
	cache := GetCachedConfig()
	// Only update cache if expired - avoid unnecessary overhead
	if time.Since(cache.lastUpdate) > cache.cacheExpiry {
		cache.Update()
	}

	// Fast validation with cached values - avoid lock with atomic access
	minRequired := cache.GetMinReadEncodeBuffer()

	// Buffer validation - use pre-allocated error for common case
	if len(buf) < minRequired {
		// Use pre-allocated error for common case, only create custom error for edge cases
		if len(buf) > 0 {
			return 0, newBufferTooSmallError(len(buf), minRequired)
		}
		return 0, cache.GetBufferTooSmallError()
	}

	// Skip initialization check for now to avoid CGO compilation issues
	// Note: The C code already has comprehensive state tracking with capture_initialized,
	// capture_initializing, playback_initialized, and playback_initializing flags.

	// Direct CGO call with minimal overhead - unsafe.Pointer(&slice[0]) is safe for validated non-empty buffers
	n := C.jetkvm_audio_read_encode(unsafe.Pointer(&buf[0]))

	// Fast path for success case
	if n > 0 {
		return int(n), nil
	}

	// Handle error cases - use static error codes to reduce allocations
	if n < 0 {
		// Common error cases
		switch n {
		case -1:
			return 0, errAudioInitFailed
		case -2:
			return 0, errAudioReadEncode
		default:
			return 0, newAudioReadEncodeError(int(n))
		}
	}

	// n == 0 case
	return 0, nil // No data available
}

// Audio playback functions
func cgoAudioPlaybackInit() error {
	// Get cached config and ensure it's updated
	cache := GetCachedConfig()
	cache.Update()

	// No need to update C constants here as they're already set in cgoAudioInit

	ret := C.jetkvm_audio_playback_init()
	if ret != 0 {
		return newAudioPlaybackInitError(int(ret))
	}
	return nil
}

func cgoAudioPlaybackClose() {
	C.jetkvm_audio_playback_close()
}

func cgoAudioDecodeWrite(buf []byte) (n int, err error) {
	// Fast validation with AudioConfigCache
	cache := GetCachedConfig()
	// Only update cache if expired - avoid unnecessary overhead
	if time.Since(cache.lastUpdate) > cache.cacheExpiry {
		cache.Update()
	}

	// Optimized buffer validation
	if len(buf) == 0 {
		return 0, errEmptyBuffer
	}

	// Use cached max buffer size with atomic access
	maxAllowed := cache.GetMaxDecodeWriteBuffer()
	if len(buf) > maxAllowed {
		// Use pre-allocated error for common case
		if len(buf) == maxAllowed+1 {
			return 0, cache.GetBufferTooLargeError()
		}
		return 0, newBufferTooLargeError(len(buf), maxAllowed)
	}

	// Direct CGO call with minimal overhead - unsafe.Pointer(&slice[0]) is safe for validated non-empty buffers
	n = int(C.jetkvm_audio_decode_write(unsafe.Pointer(&buf[0]), C.int(len(buf))))

	// Fast path for success case
	if n >= 0 {
		return n, nil
	}

	// Handle error cases with static error codes
	switch n {
	case -1:
		n = 0
		err = errAudioInitFailed
	case -2:
		n = 0
		err = errAudioDecodeWrite
	default:
		n = 0
		err = newAudioDecodeWriteError(n)
	}
	return
}

// updateOpusEncoderParams dynamically updates OPUS encoder parameters
func updateOpusEncoderParams(bitrate, complexity, vbr, vbrConstraint, signalType, bandwidth, dtx int) error {
	result := C.update_opus_encoder_params(
		C.int(bitrate),
		C.int(complexity),
		C.int(vbr),
		C.int(vbrConstraint),
		C.int(signalType),
		C.int(bandwidth),
		C.int(dtx),
	)
	if result != 0 {
		return fmt.Errorf("failed to update OPUS encoder parameters: C error code %d", result)
	}
	return nil
}

// Buffer pool for reusing buffers in CGO functions
var (
	// Using SizedBufferPool for better memory management
	// Track buffer pool usage for monitoring
	cgoBufferPoolGets atomic.Int64
	cgoBufferPoolPuts atomic.Int64
	// Batch processing statistics - only enabled in debug builds
	batchProcessingCount atomic.Int64
	batchFrameCount      atomic.Int64
	batchProcessingTime  atomic.Int64
	// Flag to control time tracking overhead
	enableBatchTimeTracking atomic.Bool
)

// GetBufferFromPool gets a buffer from the pool with at least the specified capacity
func GetBufferFromPool(minCapacity int) []byte {
	cgoBufferPoolGets.Add(1)
	// Use the SizedBufferPool for better memory management
	return GetOptimalBuffer(minCapacity)
}

// ReturnBufferToPool returns a buffer to the pool
func ReturnBufferToPool(buf []byte) {
	cgoBufferPoolPuts.Add(1)
	// Use the SizedBufferPool for better memory management
	ReturnOptimalBuffer(buf)
}

// Note: AudioFrameBatch is now defined in batch_audio.go
// This is kept here for reference but commented out to avoid conflicts
/*
// AudioFrameBatch represents a batch of audio frames for processing
type AudioFrameBatch struct {
	// Buffer for batch processing
	buffer []byte
	// Number of frames in the batch
	frameCount int
	// Size of each frame
	frameSize int
	// Current position in the buffer
	position int
}

// NewAudioFrameBatch creates a new audio frame batch with the specified capacity
func NewAudioFrameBatch(maxFrames int) *AudioFrameBatch {
	// Get cached config
	cache := GetCachedConfig()
	cache.Update()

	// Calculate frame size based on cached config
	frameSize := cache.GetMinReadEncodeBuffer()

	// Create batch with buffer sized for maxFrames
	return &AudioFrameBatch{
		buffer:     GetBufferFromPool(maxFrames * frameSize),
		frameCount: 0,
		frameSize:  frameSize,
		position:   0,
	}
}

// AddFrame adds a frame to the batch
// Returns true if the batch is full after adding this frame
func (b *AudioFrameBatch) AddFrame(frame []byte) bool {
	// Calculate position in buffer for this frame
	pos := b.position

	// Copy frame data to batch buffer
	copy(b.buffer[pos:pos+len(frame)], frame)

	// Update position and frame count
	b.position += len(frame)
	b.frameCount++

	// Check if batch is full (buffer capacity reached)
	return b.position >= len(b.buffer)
}

// Reset resets the batch for reuse
func (b *AudioFrameBatch) Reset() {
	b.frameCount = 0
	b.position = 0
}

// Release returns the batch buffer to the pool
func (b *AudioFrameBatch) Release() {
	ReturnBufferToPool(b.buffer)
	b.buffer = nil
	b.frameCount = 0
	b.frameSize = 0
	b.position = 0
}
*/

// ReadEncodeWithPooledBuffer reads audio data and encodes it using a buffer from the pool
// This reduces memory allocations by reusing buffers
func ReadEncodeWithPooledBuffer() ([]byte, int, error) {
	// Get cached config
	cache := GetCachedConfig()
	// Only update cache if expired - avoid unnecessary overhead
	if time.Since(cache.lastUpdate) > cache.cacheExpiry {
		cache.Update()
	}

	// Get a buffer from the pool with appropriate capacity
	bufferSize := cache.GetMinReadEncodeBuffer()
	if bufferSize == 0 {
		bufferSize = 1500 // Fallback if cache not initialized
	}

	// Get buffer from pool
	buf := GetBufferFromPool(bufferSize)

	// Perform read/encode operation
	n, err := cgoAudioReadEncode(buf)
	if err != nil {
		// Return buffer to pool on error
		ReturnBufferToPool(buf)
		return nil, 0, err
	}

	// Resize buffer to actual data size
	result := buf[:n]

	// Return the buffer with data
	return result, n, nil
}

// DecodeWriteWithPooledBuffer decodes and writes audio data using a pooled buffer
// The caller is responsible for returning the input buffer to the pool if needed
func DecodeWriteWithPooledBuffer(data []byte) (int, error) {
	// Validate input
	if len(data) == 0 {
		return 0, errEmptyBuffer
	}

	// Get cached config
	cache := GetCachedConfig()
	// Only update cache if expired - avoid unnecessary overhead
	if time.Since(cache.lastUpdate) > cache.cacheExpiry {
		cache.Update()
	}

	// Ensure data doesn't exceed max packet size
	maxPacketSize := cache.GetMaxPacketSize()
	if len(data) > maxPacketSize {
		return 0, newBufferTooLargeError(len(data), maxPacketSize)
	}

	// Get a PCM buffer from the pool for optimized decode-write
	pcmBuffer := GetBufferFromPool(cache.GetMaxPCMBufferSize())
	defer ReturnBufferToPool(pcmBuffer)

	// Perform decode/write operation using optimized implementation
	n, err := CGOAudioDecodeWrite(data, pcmBuffer)

	// Return result
	return n, err
}

// BatchReadEncode reads and encodes multiple audio frames in a single batch
// This reduces CGO call overhead by processing multiple frames at once
func BatchReadEncode(batchSize int) ([][]byte, error) {
	// Get cached config
	cache := GetCachedConfig()
	// Only update cache if expired - avoid unnecessary overhead
	if time.Since(cache.lastUpdate) > cache.cacheExpiry {
		cache.Update()
	}

	// Calculate total buffer size needed for batch
	frameSize := cache.GetMinReadEncodeBuffer()
	totalSize := frameSize * batchSize

	// Get a single large buffer for all frames
	batchBuffer := GetBufferFromPool(totalSize)
	defer ReturnBufferToPool(batchBuffer)

	// Pre-allocate frame result buffers from pool to avoid allocations in loop
	frameBuffers := make([][]byte, 0, batchSize)
	for i := 0; i < batchSize; i++ {
		frameBuffers = append(frameBuffers, GetBufferFromPool(frameSize))
	}
	defer func() {
		// Return all frame buffers to pool
		for _, buf := range frameBuffers {
			ReturnBufferToPool(buf)
		}
	}()

	// Track batch processing statistics - only if enabled
	var startTime time.Time
	trackTime := enableBatchTimeTracking.Load()
	if trackTime {
		startTime = time.Now()
	}
	batchProcessingCount.Add(1)

	// Process frames in batch
	frames := make([][]byte, 0, batchSize)
	for i := 0; i < batchSize; i++ {
		// Calculate offset for this frame in the batch buffer
		offset := i * frameSize
		frameBuf := batchBuffer[offset : offset+frameSize]

		// Process this frame
		n, err := cgoAudioReadEncode(frameBuf)
		if err != nil {
			// Return partial batch on error
			if i > 0 {
				batchFrameCount.Add(int64(i))
				if trackTime {
					batchProcessingTime.Add(time.Since(startTime).Microseconds())
				}
				return frames, nil
			}
			return nil, err
		}

		// Reuse pre-allocated buffer instead of make([]byte, n)
		frameCopy := frameBuffers[i][:n] // Slice to actual size
		copy(frameCopy, frameBuf[:n])
		frames = append(frames, frameCopy)
	}

	// Update statistics
	batchFrameCount.Add(int64(len(frames)))
	if trackTime {
		batchProcessingTime.Add(time.Since(startTime).Microseconds())
	}

	return frames, nil
}

// BatchDecodeWrite decodes and writes multiple audio frames in a single batch
// This reduces CGO call overhead by processing multiple frames at once
func BatchDecodeWrite(frames [][]byte) error {
	// Validate input
	if len(frames) == 0 {
		return nil
	}

	// Get cached config
	cache := GetCachedConfig()
	// Only update cache if expired - avoid unnecessary overhead
	if time.Since(cache.lastUpdate) > cache.cacheExpiry {
		cache.Update()
	}

	// Track batch processing statistics - only if enabled
	var startTime time.Time
	trackTime := enableBatchTimeTracking.Load()
	if trackTime {
		startTime = time.Now()
	}
	batchProcessingCount.Add(1)

	// Get a PCM buffer from the pool for optimized decode-write
	pcmBuffer := GetBufferFromPool(cache.GetMaxPCMBufferSize())
	defer ReturnBufferToPool(pcmBuffer)

	// Process each frame
	frameCount := 0
	for _, frame := range frames {
		// Skip empty frames
		if len(frame) == 0 {
			continue
		}

		// Process this frame using optimized implementation
		_, err := CGOAudioDecodeWrite(frame, pcmBuffer)
		if err != nil {
			// Update statistics before returning error
			batchFrameCount.Add(int64(frameCount))
			if trackTime {
				batchProcessingTime.Add(time.Since(startTime).Microseconds())
			}
			return err
		}

		frameCount++
	}

	// Update statistics
	batchFrameCount.Add(int64(frameCount))
	if trackTime {
		batchProcessingTime.Add(time.Since(startTime).Microseconds())
	}

	return nil
}

// GetBatchProcessingStats returns statistics about batch processing
func GetBatchProcessingStats() (count, frames, avgTimeUs int64) {
	count = batchProcessingCount.Load()
	frames = batchFrameCount.Load()
	totalTime := batchProcessingTime.Load()

	// Calculate average time per batch
	if count > 0 {
		avgTimeUs = totalTime / count
	}

	return count, frames, avgTimeUs
}

// cgoAudioDecodeWriteWithBuffers decodes opus data and writes to PCM buffer
// This implementation uses separate buffers for opus data and PCM output
func cgoAudioDecodeWriteWithBuffers(opusData []byte, pcmBuffer []byte) (int, error) {
	// Validate input
	if len(opusData) == 0 {
		return 0, errEmptyBuffer
	}
	if len(pcmBuffer) == 0 {
		return 0, errEmptyBuffer
	}

	// Get cached config
	cache := GetCachedConfig()
	// Only update cache if expired - avoid unnecessary overhead
	if time.Since(cache.lastUpdate) > cache.cacheExpiry {
		cache.Update()
	}

	// Ensure data doesn't exceed max packet size
	maxPacketSize := cache.GetMaxPacketSize()
	if len(opusData) > maxPacketSize {
		return 0, newBufferTooLargeError(len(opusData), maxPacketSize)
	}

	// Direct CGO call with minimal overhead - unsafe.Pointer(&slice[0]) is never nil for non-empty slices
	n := int(C.jetkvm_audio_decode_write(unsafe.Pointer(&opusData[0]), C.int(len(opusData))))

	// Fast path for success case
	if n >= 0 {
		return n, nil
	}

	// Handle error cases with static error codes to reduce allocations
	switch n {
	case -1:
		return 0, errAudioInitFailed
	case -2:
		return 0, errAudioDecodeWrite
	default:
		return 0, newAudioDecodeWriteError(n)
	}
}

// CGO function aliases
var (
	CGOAudioInit               = cgoAudioInit
	CGOAudioClose              = cgoAudioClose
	CGOAudioReadEncode         = cgoAudioReadEncode
	CGOAudioPlaybackInit       = cgoAudioPlaybackInit
	CGOAudioPlaybackClose      = cgoAudioPlaybackClose
	CGOAudioDecodeWriteLegacy  = cgoAudioDecodeWrite
	CGOAudioDecodeWrite        = cgoAudioDecodeWriteWithBuffers
	CGOUpdateOpusEncoderParams = updateOpusEncoderParams
)
