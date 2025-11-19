/*
 * JetKVM Audio Processing Module
 *
 * Bidirectional audio processing optimized for ARM NEON SIMD:
 * - OUTPUT PATH: TC358743 HDMI or USB Gadget audio → Client speakers
 *   Pipeline: ALSA hw:0,0 or hw:1,0 capture → Opus encode (192kbps, FEC enabled)
 *
 * - INPUT PATH: Client microphone → Device speakers
 *   Pipeline: Opus decode (with FEC) → ALSA hw:1,0 playback
 *
 * Key features:
 * - ARM NEON SIMD optimization for all audio operations
 * - Opus in-band FEC for packet loss resilience
 * - S16_LE stereo, 20ms frames, auto-adapts to source rate (32/44.1/48/96 kHz)
 */

#include <alsa/asoundlib.h>
#include <opus.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <errno.h>
#include <sched.h>
#include <time.h>
#include <signal.h>
#include <pthread.h>
#include <stdatomic.h>

// ARM NEON SIMD support (required - JetKVM hardware provides ARM Cortex-A7 with NEON)
#include <arm_neon.h>

// RV1106 (Cortex-A7) has 64-byte cache lines
#define CACHE_LINE_SIZE 64
#define SIMD_ALIGN __attribute__((aligned(16)))
#define CACHE_ALIGN __attribute__((aligned(CACHE_LINE_SIZE)))
#define SIMD_PREFETCH(addr, rw, locality) __builtin_prefetch(addr, rw, locality)

static snd_pcm_t *pcm_capture_handle = NULL;  // OUTPUT: TC358743 HDMI audio → client
static snd_pcm_t *pcm_playback_handle = NULL; // INPUT: Client microphone → device speakers

static const char *alsa_capture_device = NULL;
static const char *alsa_playback_device = NULL;

static OpusEncoder *encoder = NULL;
static OpusDecoder *decoder = NULL;

// Audio format (S16_LE @ 48kHz)
static uint32_t sample_rate = 48000;
static uint8_t capture_channels = 2;   // OUTPUT: Audio source (HDMI or USB) → client (always stereo for current hardware)
static uint8_t playback_channels = 1;  // INPUT: Client mono mic → device (always mono for USB audio gadget)
static uint16_t frame_size = 960;  // 20ms frames at 48kHz

static uint32_t opus_bitrate = 192000;
static uint8_t opus_complexity = 8;
static uint16_t max_packet_size = 1500;

// Opus encoder configuration constants (see opus_defines.h for full enum values)
#define OPUS_VBR 1                    // Variable bitrate mode enabled
#define OPUS_VBR_CONSTRAINT 1         // Constrained VBR maintains bitrate ceiling
#define OPUS_SIGNAL_TYPE 3002         // OPUS_SIGNAL_MUSIC (optimized for music/audio content)
#define OPUS_BANDWIDTH 1104           // OPUS_BANDWIDTH_FULLBAND (0-20kHz frequency range)
#define OPUS_LSB_DEPTH 16             // 16-bit PCM sample depth (S16_LE format)

static uint8_t opus_dtx_enabled = 1;
static uint8_t opus_fec_enabled = 1;
static uint8_t opus_packet_loss_perc = 20;  // Default packet loss compensation percentage
static uint8_t buffer_period_count = 24;

static uint32_t sleep_microseconds = 1000;
static uint32_t sleep_milliseconds = 1;
static uint8_t max_attempts_global = 5;
static uint32_t max_backoff_us_global = 500000;

static atomic_int capture_stop_requested = 0;
static atomic_int playback_stop_requested = 0;

// Mutexes to protect concurrent access to ALSA handles during close
// These prevent race conditions when jetkvm_audio_*_close() is called while
// jetkvm_audio_read_encode() or jetkvm_audio_decode_write() are executing.
// The hot path functions acquire these mutexes briefly to validate handle
// pointers, then release before slow ALSA/Opus operations to avoid holding
// locks during I/O. Handle comparison checks detect races after operations.
static pthread_mutex_t capture_mutex = PTHREAD_MUTEX_INITIALIZER;
static pthread_mutex_t playback_mutex = PTHREAD_MUTEX_INITIALIZER;

int jetkvm_audio_capture_init();
void jetkvm_audio_capture_close();
int jetkvm_audio_read_encode(void *opus_buf);

int jetkvm_audio_playback_init();
void jetkvm_audio_playback_close();
int jetkvm_audio_decode_write(void *opus_buf, int opus_size);

void update_audio_constants(uint32_t bitrate, uint8_t complexity,
                           uint32_t sr, uint8_t ch, uint16_t fs, uint16_t max_pkt,
                           uint32_t sleep_us, uint8_t max_attempts, uint32_t max_backoff,
                           uint8_t dtx_enabled, uint8_t fec_enabled, uint8_t buf_periods, uint8_t pkt_loss_perc);
void update_audio_decoder_constants(uint32_t sr, uint8_t ch, uint16_t fs, uint16_t max_pkt,
                                    uint32_t sleep_us, uint8_t max_attempts, uint32_t max_backoff,
                                    uint8_t buf_periods);


void update_audio_constants(uint32_t bitrate, uint8_t complexity,
                           uint32_t sr, uint8_t ch, uint16_t fs, uint16_t max_pkt,
                           uint32_t sleep_us, uint8_t max_attempts, uint32_t max_backoff,
                           uint8_t dtx_enabled, uint8_t fec_enabled, uint8_t buf_periods, uint8_t pkt_loss_perc) {
    opus_bitrate = (bitrate >= 64000 && bitrate <= 256000) ? bitrate : 192000;
    opus_complexity = (complexity <= 10) ? complexity : 5;
    sample_rate = sr > 0 ? sr : 48000;
    capture_channels = (ch == 1 || ch == 2) ? ch : 2;
    frame_size = fs > 0 ? fs : 960;
    max_packet_size = max_pkt > 0 ? max_pkt : 1500;
    sleep_microseconds = sleep_us > 0 ? sleep_us : 1000;
    sleep_milliseconds = sleep_microseconds / 1000;
    max_attempts_global = max_attempts > 0 ? max_attempts : 5;
    max_backoff_us_global = max_backoff > 0 ? max_backoff : 500000;
    opus_dtx_enabled = dtx_enabled ? 1 : 0;
    opus_fec_enabled = fec_enabled ? 1 : 0;
    buffer_period_count = (buf_periods >= 2 && buf_periods <= 24) ? buf_periods : 12;
    opus_packet_loss_perc = (pkt_loss_perc <= 100) ? pkt_loss_perc : 20;
}

void update_audio_decoder_constants(uint32_t sr, uint8_t ch, uint16_t fs, uint16_t max_pkt,
                                    uint32_t sleep_us, uint8_t max_attempts, uint32_t max_backoff,
                                    uint8_t buf_periods) {
    sample_rate = sr > 0 ? sr : 48000;
    playback_channels = (ch == 1 || ch == 2) ? ch : 2;
    frame_size = fs > 0 ? fs : 960;
    max_packet_size = max_pkt > 0 ? max_pkt : 1500;
    sleep_microseconds = sleep_us > 0 ? sleep_us : 1000;
    sleep_milliseconds = sleep_microseconds / 1000;
    max_attempts_global = max_attempts > 0 ? max_attempts : 5;
    max_backoff_us_global = max_backoff > 0 ? max_backoff : 500000;
    buffer_period_count = (buf_periods >= 2 && buf_periods <= 24) ? buf_periods : 12;
}

/**
 * Initialize ALSA device names from environment variables
 * Must be called before jetkvm_audio_capture_init or jetkvm_audio_playback_init
 *
 * Device mapping (set via ALSA_CAPTURE_DEVICE/ALSA_PLAYBACK_DEVICE):
 *   hw:0,0 = TC358743 HDMI audio input (for OUTPUT path capture)
 *   hw:1,0 = USB Audio Gadget (for OUTPUT path capture or INPUT path playback)
 */
static void init_alsa_devices_from_env(void) {
    // Always read from environment to support device switching
    alsa_capture_device = getenv("ALSA_CAPTURE_DEVICE");
    if (alsa_capture_device == NULL || alsa_capture_device[0] == '\0') {
        alsa_capture_device = "hw:1,0"; // Default: USB gadget audio for capture
    }

    alsa_playback_device = getenv("ALSA_PLAYBACK_DEVICE");
    if (alsa_playback_device == NULL || alsa_playback_device[0] == '\0') {
        alsa_playback_device = "hw:1,0"; // Default: USB gadget audio for playback
    }
}

// SIMD-OPTIMIZED BUFFER OPERATIONS (ARM NEON)

/**
 * Clear audio buffer using NEON (16 samples/iteration with 2x unrolling)
 */
static inline void simd_clear_samples_s16(short * __restrict__ buffer, uint32_t samples) {
    const int16x8_t zero = vdupq_n_s16(0);
    uint32_t i = 0;

    // Process 16 samples at a time (2x unrolled for better pipeline utilization)
    uint32_t simd_samples = samples & ~15U;
    for (; i < simd_samples; i += 16) {
        vst1q_s16(&buffer[i], zero);
        vst1q_s16(&buffer[i + 8], zero);
    }

    // Handle remaining 8 samples
    if (i + 8 <= samples) {
        vst1q_s16(&buffer[i], zero);
        i += 8;
    }

    // Scalar: remaining samples
    for (; i < samples; i++) {
        buffer[i] = 0;
    }
}

// INITIALIZATION STATE TRACKING

static volatile sig_atomic_t capture_initializing = 0;
static volatile sig_atomic_t capture_initialized = 0;
static volatile sig_atomic_t playback_initializing = 0;
static volatile sig_atomic_t playback_initialized = 0;

// ALSA UTILITY FUNCTIONS

/**
 * Open ALSA device with exponential backoff retry
 * @return 0 on success, negative error code on failure
 */
// Helper: High-precision sleep using nanosleep (better than usleep)
static inline void precise_sleep_us(uint32_t microseconds) {
	struct timespec ts = {
		.tv_sec = microseconds / 1000000,
		.tv_nsec = (microseconds % 1000000) * 1000
	};
	nanosleep(&ts, NULL);
}

static int safe_alsa_open(snd_pcm_t **handle, const char *device, snd_pcm_stream_t stream) {
	uint8_t attempt = 0;
	int err;
	uint32_t backoff_us = sleep_microseconds;

	while (attempt < max_attempts_global) {
		err = snd_pcm_open(handle, device, stream, SND_PCM_NONBLOCK);
		if (err >= 0) {
			snd_pcm_nonblock(*handle, 0);
			return 0;
		}

		attempt++;

		// Apply different sleep strategies based on error type
		if (err == -EPERM || err == -EACCES) {
			precise_sleep_us(backoff_us >> 1);  // Shorter wait for permission errors
		} else {
			precise_sleep_us(backoff_us);
			// Exponential backoff for all retry-worthy errors
			if (err == -EBUSY || err == -EAGAIN || err == -ENODEV || err == -ENOENT) {
				backoff_us = (backoff_us < 50000) ? (backoff_us << 1) : 50000;
			}
		}
	}
	return err;
}

/**
 * Handle ALSA I/O errors with recovery attempts
 * @param handle Pointer to PCM handle to use for recovery operations
 * @param valid_handle Pointer to the valid handle to check against (for race detection)
 * @param stop_flag Pointer to atomic stop flag
 * @param mutex Mutex to unlock on error
 * @param pcm_rc Error code from ALSA I/O operation
 * @param recovery_attempts Pointer to uint8_t recovery attempt counter
 * @param sleep_ms Milliseconds to sleep during recovery
 * @param max_attempts Maximum recovery attempts allowed
 * @return Three possible outcomes:
 *   1  = Retry operation (error was recovered, mutex still held by caller)
 *   0  = Skip this frame and continue (mutex ALREADY UNLOCKED by this function)
 *  -1  = Fatal error, abort operation (mutex ALREADY UNLOCKED by this function)
 *
 * CRITICAL: On return values 0 and -1, the mutex has already been unlocked.
 * Only return value 1 requires the caller to maintain mutex ownership.
 */
static int handle_alsa_error(snd_pcm_t *handle, snd_pcm_t **valid_handle,
                             atomic_int *stop_flag, pthread_mutex_t *mutex,
                             int pcm_rc, uint8_t *recovery_attempts,
                             uint32_t sleep_ms, uint8_t max_attempts) {
	int err;

	if (pcm_rc == -EPIPE) {
		(*recovery_attempts)++;
		if (*recovery_attempts > max_attempts || handle != *valid_handle) {
			pthread_mutex_unlock(mutex);
			return -1;
		}
		err = snd_pcm_prepare(handle);
		if (err < 0) {
			if (handle != *valid_handle) {
				pthread_mutex_unlock(mutex);
				return -1;
			}
			snd_pcm_drop(handle);
			err = snd_pcm_prepare(handle);
			if (err < 0 || handle != *valid_handle) {
				pthread_mutex_unlock(mutex);
				return -1;
			}
		}
		return 1;
	} else if (pcm_rc == -EAGAIN) {
		if (handle != *valid_handle) {
			pthread_mutex_unlock(mutex);
			return -1;
		}
		snd_pcm_wait(handle, sleep_ms);
		return 1;
	} else if (pcm_rc == -ESTRPIPE) {
		(*recovery_attempts)++;
		if (*recovery_attempts > max_attempts || handle != *valid_handle) {
			pthread_mutex_unlock(mutex);
			return -1;
		}
		uint8_t resume_attempts = 0;
		while ((err = snd_pcm_resume(handle)) == -EAGAIN && resume_attempts < 10) {
			if (*stop_flag || handle != *valid_handle) {
				pthread_mutex_unlock(mutex);
				return -1;
			}
			snd_pcm_wait(handle, sleep_ms);
			resume_attempts++;
		}
		if (err < 0) {
			if (handle != *valid_handle) {
				pthread_mutex_unlock(mutex);
				return -1;
			}
			err = snd_pcm_prepare(handle);
			if (err < 0 || handle != *valid_handle) {
				pthread_mutex_unlock(mutex);
				return -1;
			}
		}
		pthread_mutex_unlock(mutex);
		return 0;
	} else if (pcm_rc == -ENODEV) {
		pthread_mutex_unlock(mutex);
		return -1;
	} else if (pcm_rc == -EIO) {
		(*recovery_attempts)++;
		if (*recovery_attempts <= max_attempts && handle == *valid_handle) {
			snd_pcm_drop(handle);
			if (handle != *valid_handle) {
				pthread_mutex_unlock(mutex);
				return -1;
			}
			err = snd_pcm_prepare(handle);
			if (err >= 0 && handle == *valid_handle) {
				return 1;
			}
		}
		pthread_mutex_unlock(mutex);
		return -1;
	} else {
		(*recovery_attempts)++;
		if (*recovery_attempts <= 1 && pcm_rc == -EINTR) {
			return 1;
		} else if (*recovery_attempts <= 1 && pcm_rc == -EBUSY && handle == *valid_handle) {
			snd_pcm_wait(handle, 1);
			return 1;
		}
		pthread_mutex_unlock(mutex);
		return -1;
	}
}

/**
 * Configure ALSA device (S16_LE @ variable rate with optimized buffering)
 * @param handle ALSA PCM handle
 * @param device_name Device name for logging
 * @param num_channels Number of channels (1=mono, 2=stereo)
 * @param actual_rate_out Pointer to store the actual rate the device was configured to use
 * @param actual_frame_size_out Pointer to store the actual frame size (samples per channel)
 * @return 0 on success, negative error code on failure
 */
static int configure_alsa_device(snd_pcm_t *handle, const char *device_name, uint8_t num_channels,
                                 unsigned int *actual_rate_out, uint16_t *actual_frame_size_out) {
	snd_pcm_hw_params_t *params;
	snd_pcm_sw_params_t *sw_params;
	int err;

	if (!handle) return -1;

	snd_pcm_hw_params_alloca(&params);
	snd_pcm_sw_params_alloca(&sw_params);

	err = snd_pcm_hw_params_any(handle, params);
	if (err < 0) return err;

	err = snd_pcm_hw_params_set_access(handle, params, SND_PCM_ACCESS_RW_INTERLEAVED);
	if (err < 0) return err;

	err = snd_pcm_hw_params_set_format(handle, params, SND_PCM_FORMAT_S16_LE);
	if (err < 0) return err;

	err = snd_pcm_hw_params_set_channels(handle, params, num_channels);
	if (err < 0) return err;

	unsigned int requested_rate = sample_rate;
	unsigned int actual_rate = sample_rate;
	err = snd_pcm_hw_params_set_rate_near(handle, params, &actual_rate, 0);
	if (err < 0) return err;

	uint16_t actual_frame_size = frame_size;
	if (actual_rate != requested_rate) {
		fprintf(stderr, "INFO: %s: Requested sample rate %u Hz, device supports %u Hz (%.1f%% difference)\n",
		        device_name, requested_rate, actual_rate, ((float)actual_rate - requested_rate) / requested_rate * 100.0f);
		fprintf(stderr, "INFO: %s: Adapting to device rate %u Hz to avoid pitch/speed distortion\n", device_name, actual_rate);
		fflush(stderr);

		actual_frame_size = (actual_rate * 20) / 1000;
		if (num_channels == 2) {
			if (actual_frame_size < 480) actual_frame_size = 480;
			if (actual_frame_size > 2880) actual_frame_size = 2880;
		}

		fprintf(stderr, "INFO: %s: Adjusted frame size to %u samples (20ms at %u Hz)\n",
		        device_name, actual_frame_size, actual_rate);
		fflush(stderr);
	}

	snd_pcm_uframes_t period_size = actual_frame_size;
	if (period_size < 64) period_size = 64;

	err = snd_pcm_hw_params_set_period_size_near(handle, params, &period_size, 0);
	if (err < 0) return err;

	snd_pcm_uframes_t buffer_size = period_size * buffer_period_count;
	err = snd_pcm_hw_params_set_buffer_size_near(handle, params, &buffer_size);
	if (err < 0) return err;

	err = snd_pcm_hw_params(handle, params);
	if (err < 0) return err;

	unsigned int verified_rate = 0;
	err = snd_pcm_hw_params_get_rate(params, &verified_rate, 0);
	if (err < 0 || verified_rate != actual_rate) {
		fprintf(stderr, "WARNING: %s: Rate verification failed - expected %u Hz, got %u Hz\n",
		        device_name, actual_rate, verified_rate);
		fflush(stderr);
	}

	err = snd_pcm_sw_params_current(handle, sw_params);
	if (err < 0) return err;

	err = snd_pcm_sw_params_set_start_threshold(handle, sw_params, period_size);
	if (err < 0) return err;

	err = snd_pcm_sw_params_set_avail_min(handle, sw_params, period_size);
	if (err < 0) return err;

	err = snd_pcm_sw_params(handle, sw_params);
	if (err < 0) return err;

	err = snd_pcm_prepare(handle);
	if (err < 0) return err;

	if (actual_rate_out) *actual_rate_out = actual_rate;
	if (actual_frame_size_out) *actual_frame_size_out = actual_frame_size;

	return 0;
}

// AUDIO OUTPUT PATH FUNCTIONS (TC358743 HDMI Audio → Client Speakers)

/**
 * Initialize OUTPUT path (HDMI or USB Gadget audio capture → Opus encoder)
 * Opens ALSA capture device from ALSA_CAPTURE_DEVICE env (default: hw:1,0, set to hw:0,0 for TC358743 HDMI)
 * and creates Opus encoder with optimized settings
 * @return 0 on success, -EBUSY if initializing, -1/-2/-3 on errors
 */
int jetkvm_audio_capture_init() {
	int err;

	init_alsa_devices_from_env();

	if (__sync_bool_compare_and_swap(&capture_initializing, 0, 1) == 0) {
		return -EBUSY;
	}

	if (capture_initialized) {
		capture_initializing = 0;
		return 0;
	}

	if (encoder != NULL || pcm_capture_handle != NULL) {
		capture_initialized = 0;
		atomic_store(&capture_stop_requested, 1);

		if (pcm_capture_handle) {
			snd_pcm_drop(pcm_capture_handle);
		}

		pthread_mutex_lock(&capture_mutex);

		if (encoder) {
			opus_encoder_destroy(encoder);
			encoder = NULL;
		}
		if (pcm_capture_handle) {
			snd_pcm_close(pcm_capture_handle);
			pcm_capture_handle = NULL;
		}

		pthread_mutex_unlock(&capture_mutex);
	}

	err = safe_alsa_open(&pcm_capture_handle, alsa_capture_device, SND_PCM_STREAM_CAPTURE);
	if (err < 0) {
		fprintf(stderr, "Failed to open ALSA capture device %s: %s\n",
		        alsa_capture_device, snd_strerror(err));
		fflush(stderr);
		atomic_store(&capture_stop_requested, 0);
		capture_initializing = 0;
		return -1;
	}

	unsigned int actual_rate = 0;
	uint16_t actual_frame_size = 0;
	err = configure_alsa_device(pcm_capture_handle, "capture", capture_channels, &actual_rate, &actual_frame_size);
	if (err < 0) {
		snd_pcm_t *handle = pcm_capture_handle;
		pcm_capture_handle = NULL;
		snd_pcm_close(handle);
		atomic_store(&capture_stop_requested, 0);
		capture_initializing = 0;
		return -2;
	}

	fprintf(stderr, "INFO: capture: Initializing Opus encoder at %u Hz, %u channels, frame size %u\n",
	        actual_rate, capture_channels, actual_frame_size);
	fflush(stderr);

	int opus_err = 0;
	encoder = opus_encoder_create(actual_rate, capture_channels, OPUS_APPLICATION_AUDIO, &opus_err);
	if (!encoder || opus_err != OPUS_OK) {
		if (pcm_capture_handle) {
			snd_pcm_t *handle = pcm_capture_handle;
			pcm_capture_handle = NULL;
			snd_pcm_close(handle);
		}
		atomic_store(&capture_stop_requested, 0);
		capture_initializing = 0;
		return -3;
	}

	opus_encoder_ctl(encoder, OPUS_SET_BITRATE(opus_bitrate));
	opus_encoder_ctl(encoder, OPUS_SET_COMPLEXITY(opus_complexity));
	opus_encoder_ctl(encoder, OPUS_SET_VBR(OPUS_VBR));
	opus_encoder_ctl(encoder, OPUS_SET_VBR_CONSTRAINT(OPUS_VBR_CONSTRAINT));
	opus_encoder_ctl(encoder, OPUS_SET_SIGNAL(OPUS_SIGNAL_TYPE));
	opus_encoder_ctl(encoder, OPUS_SET_BANDWIDTH(OPUS_BANDWIDTH));
	opus_encoder_ctl(encoder, OPUS_SET_DTX(opus_dtx_enabled));
	opus_encoder_ctl(encoder, OPUS_SET_LSB_DEPTH(OPUS_LSB_DEPTH));

	opus_encoder_ctl(encoder, OPUS_SET_INBAND_FEC(opus_fec_enabled));
	opus_encoder_ctl(encoder, OPUS_SET_PACKET_LOSS_PERC(opus_packet_loss_perc));

	capture_initialized = 1;
	atomic_store(&capture_stop_requested, 0);
	capture_initializing = 0;
	return 0;
}

/**
 * Read HDMI audio, encode to Opus (OUTPUT path hot function)
 * @param opus_buf Output buffer for encoded Opus packet
 * @return >0 = Opus packet size in bytes, -1 = error
 */
__attribute__((hot)) int jetkvm_audio_read_encode(void * __restrict__ opus_buf) {
	static short CACHE_ALIGN pcm_buffer[960 * 2];  // Cache-aligned
	unsigned char * __restrict__ out = (unsigned char*)opus_buf;
	int32_t pcm_rc, nb_bytes;
	int32_t err = 0;
	uint8_t recovery_attempts = 0;
	const uint8_t max_recovery_attempts = 3;

	if (__builtin_expect(atomic_load(&capture_stop_requested), 0)) {
		return -1;
	}

	SIMD_PREFETCH(out, 1, 0);
	SIMD_PREFETCH(pcm_buffer, 0, 0);
	SIMD_PREFETCH(pcm_buffer + 64, 0, 1);

	// Acquire mutex to protect against concurrent close
	pthread_mutex_lock(&capture_mutex);

	if (__builtin_expect(!capture_initialized || !pcm_capture_handle || !encoder || !opus_buf, 0)) {
		pthread_mutex_unlock(&capture_mutex);
		return -1;
	}

retry_read:
	if (__builtin_expect(atomic_load(&capture_stop_requested), 0)) {
		pthread_mutex_unlock(&capture_mutex);
		return -1;
	}

	snd_pcm_t *handle = pcm_capture_handle;

	pcm_rc = snd_pcm_readi(handle, pcm_buffer, frame_size);

	if (handle != pcm_capture_handle) {
		pthread_mutex_unlock(&capture_mutex);
		return -1;
	}

	if (__builtin_expect(pcm_rc < 0, 0)) {
		int err_result = handle_alsa_error(handle, &pcm_capture_handle, &capture_stop_requested,
		                                    &capture_mutex, pcm_rc, &recovery_attempts,
		                                    sleep_milliseconds, max_recovery_attempts);
		if (err_result == 1) {
			goto retry_read;
		} else if (err_result == 0) {
			return 0;
		} else {
			return -1;
		}
	}

	// Zero-pad if we got a short read
	if (__builtin_expect(pcm_rc < frame_size, 0)) {
		uint32_t remaining_samples = (frame_size - pcm_rc) * capture_channels;
		simd_clear_samples_s16(&pcm_buffer[pcm_rc * capture_channels], remaining_samples);
	}

	OpusEncoder *enc = encoder;
	if (!enc || enc != encoder) {
		pthread_mutex_unlock(&capture_mutex);
		return -1;
	}

	nb_bytes = opus_encode(enc, pcm_buffer, frame_size, out, max_packet_size);

	pthread_mutex_unlock(&capture_mutex);
	return nb_bytes;
}

// AUDIO INPUT PATH FUNCTIONS (Client Microphone → Device Speakers)

/**
 * Initialize INPUT path (Opus decoder → device speakers)
 * Opens ALSA playback device from ALSA_PLAYBACK_DEVICE env (default: hw:1,0), falls back to "default" on error
 * and creates Opus decoder
 * @return 0 on success, -EBUSY if initializing, -1/-2 on errors
 */
int jetkvm_audio_playback_init() {
	int err;

	init_alsa_devices_from_env();

	if (__sync_bool_compare_and_swap(&playback_initializing, 0, 1) == 0) {
		return -EBUSY;
	}

	if (playback_initialized) {
		playback_initializing = 0;
		return 0;
	}

	if (decoder != NULL || pcm_playback_handle != NULL) {
		playback_initialized = 0;
		atomic_store(&playback_stop_requested, 1);
		__sync_synchronize();

		if (pcm_playback_handle) {
			snd_pcm_drop(pcm_playback_handle);
		}

		pthread_mutex_lock(&playback_mutex);

		if (decoder) {
			opus_decoder_destroy(decoder);
			decoder = NULL;
		}
		if (pcm_playback_handle) {
			snd_pcm_close(pcm_playback_handle);
			pcm_playback_handle = NULL;
		}

		pthread_mutex_unlock(&playback_mutex);
	}

	err = safe_alsa_open(&pcm_playback_handle, alsa_playback_device, SND_PCM_STREAM_PLAYBACK);
	if (err < 0) {
		fprintf(stderr, "Failed to open ALSA playback device %s: %s\n",
		        alsa_playback_device, snd_strerror(err));
		fflush(stderr);
		err = safe_alsa_open(&pcm_playback_handle, "default", SND_PCM_STREAM_PLAYBACK);
		if (err < 0) {
			atomic_store(&playback_stop_requested, 0);
			playback_initializing = 0;
			return -1;
		}
	}

	unsigned int actual_rate = 0;
	uint16_t actual_frame_size = 0;
	err = configure_alsa_device(pcm_playback_handle, "playback", playback_channels, &actual_rate, &actual_frame_size);
	if (err < 0) {
		snd_pcm_t *handle = pcm_playback_handle;
		pcm_playback_handle = NULL;
		snd_pcm_close(handle);
		atomic_store(&playback_stop_requested, 0);
		playback_initializing = 0;
		return -1;
	}

	fprintf(stderr, "INFO: playback: Initializing Opus decoder at %u Hz, %u channels, frame size %u\n",
	        actual_rate, playback_channels, actual_frame_size);
	fflush(stderr);

	int opus_err = 0;
	decoder = opus_decoder_create(actual_rate, playback_channels, &opus_err);
	if (!decoder || opus_err != OPUS_OK) {
		snd_pcm_t *handle = pcm_playback_handle;
		pcm_playback_handle = NULL;
		snd_pcm_close(handle);
		atomic_store(&playback_stop_requested, 0);
		playback_initializing = 0;
		return -2;
	}

	playback_initialized = 1;
	atomic_store(&playback_stop_requested, 0);
	playback_initializing = 0;
	return 0;
}

/**
 * Decode Opus, write to device speakers (INPUT path hot function)
 * Processing pipeline: Opus decode (with FEC) → ALSA playback with error recovery
 * @param opus_buf Encoded Opus packet from client
 * @param opus_size Size of Opus packet in bytes
 * @return >0 = PCM frames written, 0 = frame skipped, -1/-2 = error
 */
__attribute__((hot)) int jetkvm_audio_decode_write(void * __restrict__ opus_buf, int32_t opus_size) {
	static short CACHE_ALIGN pcm_buffer[960 * 2];  // Cache-aligned
	unsigned char * __restrict__ in = (unsigned char*)opus_buf;
	int32_t pcm_frames, pcm_rc, err = 0;
	uint8_t recovery_attempts = 0;
	const uint8_t max_recovery_attempts = 3;

	if (__builtin_expect(atomic_load(&playback_stop_requested), 0)) {
		return -1;
	}

	SIMD_PREFETCH(in, 0, 0);

	// Acquire mutex to protect against concurrent close
	pthread_mutex_lock(&playback_mutex);

	if (__builtin_expect(!playback_initialized || !pcm_playback_handle || !decoder || !opus_buf || opus_size <= 0, 0)) {
		pthread_mutex_unlock(&playback_mutex);
		return -1;
	}

	if (opus_size > max_packet_size) {
		pthread_mutex_unlock(&playback_mutex);
		return -1;
	}

	OpusDecoder *dec = decoder;
	if (!dec || dec != decoder) {
		pthread_mutex_unlock(&playback_mutex);
		return -1;
	}

	// Decode Opus packet to PCM (FEC automatically applied if embedded in packet)
	// decode_fec=0 means normal decode (FEC data is used automatically when present)
	pcm_frames = opus_decode(dec, in, opus_size, pcm_buffer, frame_size, 0);

	if (__builtin_expect(pcm_frames < 0, 0)) {
		pcm_frames = opus_decode(dec, NULL, 0, pcm_buffer, frame_size, 1);

		if (pcm_frames < 0) {
			pthread_mutex_unlock(&playback_mutex);
			return -1;
		}
	}

retry_write:
	if (__builtin_expect(atomic_load(&playback_stop_requested), 0)) {
		pthread_mutex_unlock(&playback_mutex);
		return -1;
	}

	snd_pcm_t *handle = pcm_playback_handle;
	pcm_rc = snd_pcm_writei(handle, pcm_buffer, pcm_frames);

	if (handle != pcm_playback_handle) {
		pthread_mutex_unlock(&playback_mutex);
		return -1;
	}
	if (__builtin_expect(pcm_rc < 0, 0)) {
		int err_result = handle_alsa_error(handle, &pcm_playback_handle, &playback_stop_requested,
		                                    &playback_mutex, pcm_rc, &recovery_attempts,
		                                    sleep_milliseconds, max_recovery_attempts);
		if (err_result == 1) {
			goto retry_write;
		} else if (err_result == 0) {
			return 0;
		} else {
			return -2;
		}
	}
	pthread_mutex_unlock(&playback_mutex);
	return pcm_frames;
}

// CLEANUP FUNCTIONS

/**
 * Close audio stream (shared cleanup logic for capture and playback)
 * @param stop_requested Pointer to stop flag
 * @param initializing Pointer to initializing flag
 * @param initialized Pointer to initialized flag
 * @param mutex Mutex to protect cleanup
 * @param pcm_handle Pointer to PCM handle
 * @param codec Pointer to codec (encoder or decoder)
 * @param destroy_codec Function to destroy the codec
 */
typedef void (*codec_destroy_fn)(void*);

static void close_audio_stream(atomic_int *stop_requested, volatile int *initializing,
                                volatile int *initialized, pthread_mutex_t *mutex,
                                snd_pcm_t **pcm_handle, void **codec,
                                codec_destroy_fn destroy_codec) {
	atomic_store(stop_requested, 1);

	while (*initializing) {
		sched_yield();
	}

	if (__sync_bool_compare_and_swap(initialized, 1, 0) == 0) {
		atomic_store(stop_requested, 0);
		return;
	}

	struct timespec short_delay = { .tv_sec = 0, .tv_nsec = 5000000 };
	nanosleep(&short_delay, NULL);

	pthread_mutex_lock(mutex);

	snd_pcm_t *handle_to_close = *pcm_handle;
	void *codec_to_destroy = *codec;
	*pcm_handle = NULL;
	*codec = NULL;

	pthread_mutex_unlock(mutex);

	if (handle_to_close) {
		snd_pcm_drop(handle_to_close);
		snd_pcm_close(handle_to_close);
	}

	if (codec_to_destroy) {
		destroy_codec(codec_to_destroy);
	}

	atomic_store(stop_requested, 0);
}

void jetkvm_audio_playback_close() {
	close_audio_stream(&playback_stop_requested, &playback_initializing,
	                   &playback_initialized, &playback_mutex,
	                   &pcm_playback_handle, (void**)&decoder,
	                   (codec_destroy_fn)opus_decoder_destroy);
}

void jetkvm_audio_capture_close() {
	close_audio_stream(&capture_stop_requested, &capture_initializing,
	                   &capture_initialized, &capture_mutex,
	                   &pcm_capture_handle, (void**)&encoder,
	                   (codec_destroy_fn)opus_encoder_destroy);
}
