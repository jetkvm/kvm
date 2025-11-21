/*
 * JetKVM Audio Processing Module
 *
 * Bidirectional audio processing optimized for ARM NEON SIMD:
 * - OUTPUT PATH: TC358743 HDMI or USB Gadget audio → Client speakers
 *   Pipeline: ALSA hw:0,0 or hw:1,0 capture → SpeexDSP resample → Opus encode (192kbps, FEC enabled)
 *
 * - INPUT PATH: Client microphone → Device speakers
 *   Pipeline: Opus decode (with FEC) → ALSA hw:1,0 playback
 *
 * Key features:
 * - ARM NEON SIMD optimization for all audio operations
 * - SpeexDSP high-quality resampling (SPEEX_RESAMPLER_QUALITY_DESKTOP)
 * - Opus in-band FEC for packet loss resilience
 * - S16_LE stereo, 20ms frames at 48kHz (hardware rate auto-negotiated)
 * - Direct hardware access with userspace resampling (no ALSA plugin layer)
 */

#include <alsa/asoundlib.h>
#include <opus.h>
#include <speex/speex_resampler.h>
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

// ARM NEON SIMD optimizations (Cortex-A7 accelerates buffer operations, with scalar fallback)
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
static bool capture_channels_swapped = false;  // True if hardware reports R,L instead of L,R

static OpusEncoder *encoder = NULL;
static OpusDecoder *decoder = NULL;
static SpeexResamplerState *capture_resampler = NULL;

// Audio format - Opus always uses 48kHz for WebRTC (RFC 7587)
static const uint32_t opus_sample_rate = 48000;  // Fixed: Opus RTP clock rate
static uint32_t hardware_sample_rate = 48000;    // Hardware-negotiated rate
static uint8_t capture_channels = 2;   // OUTPUT: Audio source (HDMI or USB) → client (stereo by default)
static uint8_t playback_channels = 1;  // INPUT: Client mono mic → device (always mono for USB audio gadget)
static const uint16_t opus_frame_size = 960;  // 20ms frames at 48kHz (fixed)
static uint16_t hardware_frame_size = 960;     // 20ms frames at hardware rate

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

// Mutexes to protect concurrent access to ALSA handles and codecs throughout their lifecycle
// These prevent race conditions when jetkvm_audio_*_close() is called while
// jetkvm_audio_read_encode() or jetkvm_audio_decode_write() are executing.
// The mutexes protect initialization, cleanup, ALSA I/O, codec operations, and handle validation
// to ensure handles remain valid from acquisition through release.
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
    // Validate and set bitrate (64-256 kbps range)
    opus_bitrate = (bitrate >= 64000 && bitrate <= 256000) ? bitrate : 192000;

    // Set complexity (0-10 range)
    opus_complexity = (complexity <= 10) ? complexity : 5;

    // Set channel count (mono or stereo)
    capture_channels = (ch == 1 || ch == 2) ? ch : 2;

    // Set packet and timing parameters
    max_packet_size = max_pkt > 0 ? max_pkt : 1500;
    sleep_microseconds = sleep_us > 0 ? sleep_us : 1000;
    sleep_milliseconds = sleep_microseconds / 1000;
    max_attempts_global = max_attempts > 0 ? max_attempts : 5;
    max_backoff_us_global = max_backoff > 0 ? max_backoff : 500000;

    // Set codec features
    opus_dtx_enabled = dtx_enabled ? 1 : 0;
    opus_fec_enabled = fec_enabled ? 1 : 0;

    // Set buffer configuration
    buffer_period_count = (buf_periods >= 2 && buf_periods <= 24) ? buf_periods : 12;
    opus_packet_loss_perc = (pkt_loss_perc <= 100) ? pkt_loss_perc : 20;

    // Note: sr and fs parameters ignored - Opus always uses 48kHz with 960 samples
}

void update_audio_decoder_constants(uint32_t sr, uint8_t ch, uint16_t fs, uint16_t max_pkt,
                                    uint32_t sleep_us, uint8_t max_attempts, uint32_t max_backoff,
                                    uint8_t buf_periods) {
    // Set playback channels (mono or stereo)
    playback_channels = (ch == 1 || ch == 2) ? ch : 2;

    // Set packet and timing parameters
    max_packet_size = max_pkt > 0 ? max_pkt : 1500;
    sleep_microseconds = sleep_us > 0 ? sleep_us : 1000;
    sleep_milliseconds = sleep_microseconds / 1000;
    max_attempts_global = max_attempts > 0 ? max_attempts : 5;
    max_backoff_us_global = max_backoff > 0 ? max_backoff : 500000;

    // Set buffer configuration
    buffer_period_count = (buf_periods >= 2 && buf_periods <= 24) ? buf_periods : 12;

    // Note: sr and fs parameters ignored - always 48kHz with 960 samples
}

/**
 * Initialize ALSA device names from environment variables
 * Must be called before jetkvm_audio_capture_init or jetkvm_audio_playback_init
 *
 * Device mapping (set via ALSA_CAPTURE_DEVICE/ALSA_PLAYBACK_DEVICE):
 *   hw:0,0 = TC358743 HDMI audio (direct hardware access, SpeexDSP resampling)
 *   hw:1,0 = USB Audio Gadget (direct hardware access, SpeexDSP resampling)
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

/**
 * Soft-clip audio samples to prevent digital clipping distortion
 * Uses smooth saturation curve for transients that exceed ±30720 (~0.94 of max)
 * Processes 8 samples per iteration using ARM NEON
 */
static inline void simd_soft_clip_s16(short * __restrict__ buffer, uint32_t samples) {
	const int16_t threshold = 30720;  // 0.9375 * 32768
	const int16x8_t thresh_pos = vdupq_n_s16(threshold);
	const int16x8_t thresh_neg = vdupq_n_s16(-threshold);
	const int16x8_t max_val = vdupq_n_s16(32767);
	const int16x8_t min_val = vdupq_n_s16(-32768);

	uint32_t i = 0;
	uint32_t simd_samples = samples & ~7U;

	for (; i < simd_samples; i += 8) {
		int16x8_t samples_vec = vld1q_s16(&buffer[i]);

		// Detect samples exceeding positive threshold
		uint16x8_t exceeds_pos = vcgtq_s16(samples_vec, thresh_pos);
		// Detect samples below negative threshold
		uint16x8_t exceeds_neg = vcltq_s16(samples_vec, thresh_neg);

		// Apply soft saturation to samples exceeding thresholds
		// For positive: scale down to range [threshold, max_val]
		// For negative: scale up to range [min_val, -threshold]
		int16x8_t clipped = samples_vec;
		clipped = vbslq_s16(exceeds_pos,
		                    vaddq_s16(thresh_pos, vshrq_n_s16(vsubq_s16(samples_vec, thresh_pos), 2)),
		                    clipped);
		clipped = vbslq_s16(exceeds_neg,
		                    vaddq_s16(thresh_neg, vshrq_n_s16(vsubq_s16(samples_vec, thresh_neg), 2)),
		                    clipped);

		// Clamp to int16 range
		clipped = vminq_s16(vmaxq_s16(clipped, min_val), max_val);

		vst1q_s16(&buffer[i], clipped);
	}

	// Scalar: remaining samples
	for (; i < samples; i++) {
		int32_t sample = buffer[i];
		if (sample > threshold) {
			sample = threshold + ((sample - threshold) >> 2);
		} else if (sample < -threshold) {
			sample = -threshold + ((sample + threshold) >> 2);
		}
		// Clamp to int16 range
		if (sample > 32767) sample = 32767;
		if (sample < -32768) sample = -32768;
		buffer[i] = (short)sample;
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
// High-precision sleep using nanosleep
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

		// Apply sleep strategy based on error type
		if (err == -EPERM || err == -EACCES) {
			precise_sleep_us(backoff_us >> 1);  // Shorter wait for permission errors
		} else {
			precise_sleep_us(backoff_us);
			// Exponential backoff for retry-worthy errors
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
 * Configure ALSA device (S16_LE @ hardware-negotiated rate with optimized buffering)
 * @param handle ALSA PCM handle
 * @param device_name Device name for logging
 * @param num_channels Number of channels (1=mono, 2=stereo)
 * @param actual_rate_out Pointer to store the actual hardware-negotiated rate
 * @param actual_frame_size_out Pointer to store the actual frame size at hardware rate
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
	if (err < 0) {
		fprintf(stderr, "ERROR: %s: Failed to set access mode: %s\n", device_name, snd_strerror(err));
		fflush(stderr);
		return err;
	}

	err = snd_pcm_hw_params_set_format(handle, params, SND_PCM_FORMAT_S16_LE);
	if (err < 0) {
		fprintf(stderr, "ERROR: %s: Failed to set format S16_LE: %s\n", device_name, snd_strerror(err));
		fflush(stderr);
		return err;
	}

	err = snd_pcm_hw_params_set_channels(handle, params, num_channels);
	if (err < 0) {
		fprintf(stderr, "ERROR: %s: Failed to set %u channels: %s\n", device_name, num_channels, snd_strerror(err));
		fflush(stderr);
		return err;
	}

	// Disable ALSA resampling - we handle it with SpeexDSP
	err = snd_pcm_hw_params_set_rate_resample(handle, params, 0);
	if (err < 0) {
		fprintf(stderr, "ERROR: %s: Failed to disable ALSA resampling: %s\n", device_name, snd_strerror(err));
		fflush(stderr);
		return err;
	}

	// Try to set 48kHz first (preferred), then let hardware negotiate
	unsigned int requested_rate = opus_sample_rate;
	err = snd_pcm_hw_params_set_rate_near(handle, params, &requested_rate, 0);
	if (err < 0) return err;

	// Calculate frame size for this hardware rate (20ms)
	uint16_t hw_frame_size = requested_rate / 50;

	snd_pcm_uframes_t period_size = hw_frame_size;
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
	if (err < 0) {
		fprintf(stderr, "ERROR: %s: Failed to get rate: %s\n",
		        device_name, snd_strerror(err));
		fflush(stderr);
		return err;
	}

	fprintf(stderr, "INFO: %s: Hardware negotiated %u Hz (Opus uses %u Hz with SpeexDSP resampling)\n",
	        device_name, verified_rate, opus_sample_rate);
	fflush(stderr);

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

	if (num_channels == 2) {
		snd_pcm_chmap_t *chmap = snd_pcm_get_chmap(handle);
		if (chmap != NULL) {
			if (chmap->channels == 2) {
				bool is_swapped = (chmap->pos[0] == SND_CHMAP_FR && chmap->pos[1] == SND_CHMAP_FL);
				if (is_swapped) {
					fprintf(stderr, "INFO: %s: Hardware reports swapped channel map (R,L instead of L,R)\n",
					        device_name);
					fflush(stderr);
				}
				if (actual_frame_size_out && is_swapped) {
					*actual_frame_size_out |= 0x8000;
				}
			}
			free(chmap);
		}
	}

	if (actual_rate_out) *actual_rate_out = verified_rate;
	if (actual_frame_size_out) *actual_frame_size_out &= 0x7FFF;

	return 0;
}

// AUDIO OUTPUT PATH FUNCTIONS (TC358743 HDMI Audio → Client Speakers)

/**
 * Initialize OUTPUT path (HDMI or USB Gadget audio capture → Opus encoder)
 * Opens ALSA capture device from ALSA_CAPTURE_DEVICE env (default: hw:1,0, set to hw:0,0 for HDMI)
 * and creates Opus encoder with optimized settings
 * @return 0 on success, -EBUSY if initializing, -1/-2/-3/-4 on errors
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
	uint16_t actual_frame_size_with_flag = 0;
	err = configure_alsa_device(pcm_capture_handle, "capture", capture_channels, &actual_rate, &actual_frame_size_with_flag);
	if (err < 0) {
		snd_pcm_t *handle = pcm_capture_handle;
		pcm_capture_handle = NULL;
		snd_pcm_close(handle);
		atomic_store(&capture_stop_requested, 0);
		capture_initializing = 0;
		return -2;
	}

	capture_channels_swapped = (actual_frame_size_with_flag & 0x8000) != 0;
	hardware_sample_rate = actual_rate;
	hardware_frame_size = actual_frame_size_with_flag & 0x7FFF;
	if (hardware_frame_size > 3840) {
		fprintf(stderr, "ERROR: capture: Hardware frame size %u exceeds buffer capacity 3840\n",
		        hardware_frame_size);
		fflush(stderr);
		snd_pcm_t *handle = pcm_capture_handle;
		pcm_capture_handle = NULL;
		snd_pcm_close(handle);
		atomic_store(&capture_stop_requested, 0);
		capture_initializing = 0;
		return -4;
	}

	// Clean up any existing resampler before creating new one (prevents memory leak on re-init)
	if (capture_resampler) {
		speex_resampler_destroy(capture_resampler);
		capture_resampler = NULL;
	}

	// Initialize Speex resampler if hardware rate != 48kHz
	if (hardware_sample_rate != opus_sample_rate) {
		int speex_err = 0;
		capture_resampler = speex_resampler_init(capture_channels, hardware_sample_rate,
		                                          opus_sample_rate, SPEEX_RESAMPLER_QUALITY_DESKTOP,
		                                          &speex_err);
		if (!capture_resampler || speex_err != 0) {
			fprintf(stderr, "ERROR: capture: Failed to create SpeexDSP resampler (%u Hz → %u Hz): %d\n",
			        hardware_sample_rate, opus_sample_rate, speex_err);
			fflush(stderr);
			snd_pcm_t *handle = pcm_capture_handle;
			pcm_capture_handle = NULL;
			snd_pcm_close(handle);
			atomic_store(&capture_stop_requested, 0);
			capture_initializing = 0;
			return -3;
		}
		fprintf(stderr, "INFO: capture: SpeexDSP resampler initialized (%u Hz → %u Hz)\n",
		        hardware_sample_rate, opus_sample_rate);
		fflush(stderr);
	} else {
		fprintf(stderr, "INFO: capture: No resampling needed (hardware = Opus = %u Hz)\n", opus_sample_rate);
		fflush(stderr);
	}

	fprintf(stderr, "INFO: capture: Initializing Opus encoder at %u Hz, %u channels, frame size %u\n",
	        opus_sample_rate, capture_channels, opus_frame_size);
	fflush(stderr);

	int opus_err = 0;
	encoder = opus_encoder_create(opus_sample_rate, capture_channels, OPUS_APPLICATION_AUDIO, &opus_err);
	if (!encoder || opus_err != OPUS_OK) {
		if (capture_resampler) {
			speex_resampler_destroy(capture_resampler);
			capture_resampler = NULL;
		}
		if (pcm_capture_handle) {
			snd_pcm_t *handle = pcm_capture_handle;
			pcm_capture_handle = NULL;
			snd_pcm_close(handle);
		}
		atomic_store(&capture_stop_requested, 0);
		capture_initializing = 0;
		return -4;
	}

	#define OPUS_CTL_WARN(call, desc) do { \
		int _err = call; \
		if (_err != OPUS_OK) { \
			fprintf(stderr, "WARN: capture: Failed to set " desc ": %s\n", opus_strerror(_err)); \
			fflush(stderr); \
		} \
	} while(0)

	OPUS_CTL_WARN(opus_encoder_ctl(encoder, OPUS_SET_BITRATE(opus_bitrate)), "bitrate");
	OPUS_CTL_WARN(opus_encoder_ctl(encoder, OPUS_SET_COMPLEXITY(opus_complexity)), "complexity");
	OPUS_CTL_WARN(opus_encoder_ctl(encoder, OPUS_SET_VBR(OPUS_VBR)), "VBR mode");
	OPUS_CTL_WARN(opus_encoder_ctl(encoder, OPUS_SET_VBR_CONSTRAINT(OPUS_VBR_CONSTRAINT)), "VBR constraint");
	OPUS_CTL_WARN(opus_encoder_ctl(encoder, OPUS_SET_SIGNAL(OPUS_SIGNAL_TYPE)), "signal type");
	OPUS_CTL_WARN(opus_encoder_ctl(encoder, OPUS_SET_BANDWIDTH(OPUS_BANDWIDTH)), "bandwidth");
	OPUS_CTL_WARN(opus_encoder_ctl(encoder, OPUS_SET_DTX(opus_dtx_enabled)), "DTX");
	OPUS_CTL_WARN(opus_encoder_ctl(encoder, OPUS_SET_LSB_DEPTH(OPUS_LSB_DEPTH)), "LSB depth");
	OPUS_CTL_WARN(opus_encoder_ctl(encoder, OPUS_SET_INBAND_FEC(opus_fec_enabled)), "FEC");
	OPUS_CTL_WARN(opus_encoder_ctl(encoder, OPUS_SET_PACKET_LOSS_PERC(opus_packet_loss_perc)), "packet loss percentage");

	#undef OPUS_CTL_WARN

	capture_initialized = 1;
	atomic_store(&capture_stop_requested, 0);
	capture_initializing = 0;
	return 0;
}

/**
 * Read HDMI audio, resample with SpeexDSP, encode to Opus (OUTPUT path hot function)
 * @param opus_buf Output buffer for encoded Opus packet
 * @return >0 = Opus packet size in bytes, -1 = error
 */
__attribute__((hot)) int jetkvm_audio_read_encode(void * __restrict__ opus_buf) {
	// Two buffers: hardware buffer + resampled buffer (at 48kHz)
	static short CACHE_ALIGN pcm_hw_buffer[3840 * 2];    // Max 192kHz @ 20ms * 2 channels
	static short CACHE_ALIGN pcm_opus_buffer[960 * 2];   // 48kHz @ 20ms * 2 channels
	unsigned char * __restrict__ out = (unsigned char*)opus_buf;
	int32_t pcm_rc, nb_bytes;
	int32_t err = 0;
	uint8_t recovery_attempts = 0;
	const uint8_t max_recovery_attempts = 3;

	if (__builtin_expect(atomic_load(&capture_stop_requested), 0)) {
		return -1;
	}

	SIMD_PREFETCH(out, 1, 0);
	SIMD_PREFETCH(pcm_hw_buffer, 0, 0);
	SIMD_PREFETCH(pcm_hw_buffer + 64, 0, 1);

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

	// Release mutex before blocking I/O to allow clean shutdown
	pthread_mutex_unlock(&capture_mutex);

	// Read from hardware at hardware sample rate (blocking call, no mutex held)
	pcm_rc = snd_pcm_readi(handle, pcm_hw_buffer, hardware_frame_size);

	// Reacquire mutex and verify device wasn't closed during read
	pthread_mutex_lock(&capture_mutex);

	if (handle != pcm_capture_handle || atomic_load(&capture_stop_requested)) {
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
	if (__builtin_expect(pcm_rc < hardware_frame_size, 0)) {
		uint32_t remaining_samples = (hardware_frame_size - pcm_rc) * capture_channels;
		simd_clear_samples_s16(&pcm_hw_buffer[pcm_rc * capture_channels], remaining_samples);
	}

	if (capture_channels_swapped && capture_channels == 2) {
		for (uint32_t i = 0; i < hardware_frame_size; i++) {
			short temp = pcm_hw_buffer[i * 2];
			pcm_hw_buffer[i * 2] = pcm_hw_buffer[i * 2 + 1];
			pcm_hw_buffer[i * 2 + 1] = temp;
		}
	}
	short *pcm_to_encode;
	if (capture_resampler) {
		spx_uint32_t in_len = hardware_frame_size;
		spx_uint32_t out_len = opus_frame_size;
		int res_err = speex_resampler_process_interleaved_int(capture_resampler,
		                                                        pcm_hw_buffer, &in_len,
		                                                        pcm_opus_buffer, &out_len);
		if (res_err != 0 || out_len != opus_frame_size) {
			fprintf(stderr, "ERROR: capture: Resampling failed (err=%d, out_len=%u, expected=%u)\n",
			        res_err, out_len, opus_frame_size);
			fflush(stderr);
			pthread_mutex_unlock(&capture_mutex);
			return -1;
		}
		pcm_to_encode = pcm_opus_buffer;
	} else {
		pcm_to_encode = pcm_hw_buffer;
	}

	OpusEncoder *enc = encoder;
	if (!enc || enc != encoder) {
		pthread_mutex_unlock(&capture_mutex);
		return -1;
	}

	// Apply soft-clipping to prevent digital clipping distortion on sharp transients
	simd_soft_clip_s16(pcm_to_encode, opus_frame_size * capture_channels);

	nb_bytes = opus_encode(enc, pcm_to_encode, opus_frame_size, out, max_packet_size);

	if (__builtin_expect(nb_bytes < 0, 0)) {
		fprintf(stderr, "ERROR: capture: Opus encoding failed: %s\n", opus_strerror(nb_bytes));
		fflush(stderr);
	}

	pthread_mutex_unlock(&capture_mutex);
	return nb_bytes;
}

// AUDIO INPUT PATH FUNCTIONS (Client Microphone → Device Speakers)

/**
 * Initialize INPUT path (Opus decoder → device speakers)
 * Opens ALSA playback device from ALSA_PLAYBACK_DEVICE env (default: hw:1,0)
 * and creates Opus decoder. Returns immediately on device open failure (no fallback).
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
		atomic_store(&playback_stop_requested, 0);
		playback_initializing = 0;
		return -1;
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
	pcm_frames = opus_decode(dec, in, opus_size, pcm_buffer, opus_frame_size, 0);

	if (__builtin_expect(pcm_frames < 0, 0)) {
		pcm_frames = opus_decode(dec, NULL, 0, pcm_buffer, opus_frame_size, 1);

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

	// Release mutex before blocking I/O to allow clean shutdown
	pthread_mutex_unlock(&playback_mutex);

	// Write to hardware (blocking call, no mutex held)
	pcm_rc = snd_pcm_writei(handle, pcm_buffer, pcm_frames);

	// Reacquire mutex and verify device wasn't closed during write
	pthread_mutex_lock(&playback_mutex);

	if (handle != pcm_playback_handle || atomic_load(&playback_stop_requested)) {
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

	// Clean up resampler inside mutex to prevent race with encoding thread
	if (mutex == &capture_mutex && capture_resampler) {
		SpeexResamplerState *res = capture_resampler;
		capture_resampler = NULL;
		speex_resampler_destroy(res);
	}

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
