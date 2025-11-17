/*
 * JetKVM Audio Processing Module
 *
 * Bidirectional audio processing optimized for ARM NEON SIMD:
 * - OUTPUT PATH: TC358743 HDMI or USB Gadget audio → Client speakers
 *   Pipeline: ALSA hw:0,0 or hw:1,0 capture → Opus encode (128kbps, FEC enabled)
 *
 * - INPUT PATH: Client microphone → Device speakers
 *   Pipeline: Opus decode (with FEC) → ALSA hw:1,0 playback
 *
 * Key features:
 * - ARM NEON SIMD optimization for all audio operations
 * - Opus in-band FEC for packet loss resilience
 * - S16_LE @ 48kHz stereo, 20ms frames (960 samples)
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

// ARM NEON SIMD support (always available on JetKVM's ARM Cortex-A7)
#include <arm_neon.h>

// RV1106 (Cortex-A7) has 64-byte cache lines
#define CACHE_LINE_SIZE 64
#define SIMD_ALIGN __attribute__((aligned(16)))
#define CACHE_ALIGN __attribute__((aligned(CACHE_LINE_SIZE)))
#define SIMD_PREFETCH(addr, rw, locality) __builtin_prefetch(addr, rw, locality)

// Compile-time trace logging - disabled for production (zero overhead)
#define TRACE_LOG(...) ((void)0)

// ALSA device handles
static snd_pcm_t *pcm_capture_handle = NULL;  // OUTPUT: TC358743 HDMI audio → client
static snd_pcm_t *pcm_playback_handle = NULL; // INPUT: Client microphone → device speakers

// ALSA device names
static const char *alsa_capture_device = NULL;
static const char *alsa_playback_device = NULL;

// Opus codec instances
static OpusEncoder *encoder = NULL;
static OpusDecoder *decoder = NULL;

// Audio format (S16_LE @ 48kHz stereo)
static uint32_t sample_rate = 48000;
static uint8_t channels = 2;
static uint16_t frame_size = 960;  // 20ms frames at 48kHz

static uint32_t opus_bitrate = 128000;
static uint8_t opus_complexity = 5;
static uint16_t max_packet_size = 1500;

#define OPUS_VBR 1
#define OPUS_VBR_CONSTRAINT 1
#define OPUS_SIGNAL_TYPE 3002
#define OPUS_BANDWIDTH 1104
#define OPUS_LSB_DEPTH 16

static uint8_t opus_dtx_enabled = 1;
static uint8_t opus_fec_enabled = 1;
static uint8_t opus_packet_loss_perc = 20;
static uint8_t buffer_period_count = 12;

static uint32_t sleep_microseconds = 1000;
static uint32_t sleep_milliseconds = 1;
static uint8_t max_attempts_global = 5;
static uint32_t max_backoff_us_global = 500000;

static volatile int capture_stop_requested = 0;
static volatile int playback_stop_requested = 0;

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
int update_opus_encoder_params(uint32_t bitrate, uint8_t complexity);


void update_audio_constants(uint32_t bitrate, uint8_t complexity,
                           uint32_t sr, uint8_t ch, uint16_t fs, uint16_t max_pkt,
                           uint32_t sleep_us, uint8_t max_attempts, uint32_t max_backoff,
                           uint8_t dtx_enabled, uint8_t fec_enabled, uint8_t buf_periods, uint8_t pkt_loss_perc) {
    opus_bitrate = (bitrate >= 64000 && bitrate <= 256000) ? bitrate : 128000;
    opus_complexity = (complexity <= 10) ? complexity : 5;
    sample_rate = sr > 0 ? sr : 48000;
    channels = (ch == 1 || ch == 2) ? ch : 2;
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
    channels = (ch == 1 || ch == 2) ? ch : 2;
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
 */
static void init_alsa_devices_from_env(void) {
    // Always read from environment to support device switching
    alsa_capture_device = getenv("ALSA_CAPTURE_DEVICE");
    if (alsa_capture_device == NULL || alsa_capture_device[0] == '\0') {
        alsa_capture_device = "hw:1,0"; // Default to USB gadget
    }

    alsa_playback_device = getenv("ALSA_PLAYBACK_DEVICE");
    if (alsa_playback_device == NULL || alsa_playback_device[0] == '\0') {
        alsa_playback_device = "hw:1,0"; // Default to USB gadget
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

/**
 * Update Opus encoder settings at runtime (does NOT modify FEC or hardcoded settings)
 *
 * NOTE: Currently unused but kept for potential future runtime configuration updates.
 * In the current CGO implementation, encoder params are set once via update_audio_constants()
 * before initialization. This function would be useful if we add runtime bitrate/complexity
 * adjustment without restarting the encoder.
 *
 * @return 0 on success, -1 if not initialized, >0 if some settings failed
 */
int update_opus_encoder_params(uint32_t bitrate, uint8_t complexity) {
    if (!encoder || !capture_initialized) {
        return -1;
    }

    // Update runtime-configurable parameters
    opus_bitrate = bitrate;
    opus_complexity = complexity;

    // Apply settings to encoder
    int result = 0;
    result |= opus_encoder_ctl(encoder, OPUS_SET_BITRATE(opus_bitrate));
    result |= opus_encoder_ctl(encoder, OPUS_SET_COMPLEXITY(opus_complexity));

    return result;
}

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

		if (err == -EBUSY || err == -EAGAIN) {
			precise_sleep_us(backoff_us);
			backoff_us = (backoff_us < 50000) ? (backoff_us << 1) : 50000;
		} else if (err == -ENODEV || err == -ENOENT) {
			precise_sleep_us(backoff_us);
			backoff_us = (backoff_us < 50000) ? (backoff_us << 1) : 50000;
		} else if (err == -EPERM || err == -EACCES) {
			precise_sleep_us(backoff_us >> 1);
		} else {
			precise_sleep_us(backoff_us);
			backoff_us = (backoff_us < 50000) ? (backoff_us << 1) : 50000;
		}
	}
	return err;
}

/**
 * Configure ALSA device (S16_LE @ variable rate stereo with optimized buffering)
 * @param handle ALSA PCM handle
 * @param device_name Device name for logging
 * @param actual_rate_out Pointer to store the actual rate the device was configured to use
 * @param actual_frame_size_out Pointer to store the actual frame size (samples per channel)
 * @return 0 on success, negative error code on failure
 */
static int configure_alsa_device(snd_pcm_t *handle, const char *device_name,
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

	err = snd_pcm_hw_params_set_channels(handle, params, channels);
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
		if (channels == 2) {
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
 * Initialize OUTPUT path (TC358743 HDMI capture → Opus encoder)
 * Opens hw:0,0 (TC358743) and creates Opus encoder with optimized settings
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

	if (encoder) {
		opus_encoder_destroy(encoder);
		encoder = NULL;
	}
	if (pcm_capture_handle) {
		snd_pcm_close(pcm_capture_handle);
		pcm_capture_handle = NULL;
	}

	err = safe_alsa_open(&pcm_capture_handle, alsa_capture_device, SND_PCM_STREAM_CAPTURE);
	if (err < 0) {
		fprintf(stderr, "Failed to open ALSA capture device %s: %s\n",
		        alsa_capture_device, snd_strerror(err));
		fflush(stderr);
		capture_initializing = 0;
		return -1;
	}

	unsigned int actual_rate = 0;
	uint16_t actual_frame_size = 0;
	err = configure_alsa_device(pcm_capture_handle, "capture", &actual_rate, &actual_frame_size);
	if (err < 0) {
		snd_pcm_close(pcm_capture_handle);
		pcm_capture_handle = NULL;
		capture_initializing = 0;
		return -2;
	}

	fprintf(stderr, "INFO: capture: Initializing Opus encoder at %u Hz, %u channels, frame size %u\n",
	        actual_rate, channels, actual_frame_size);
	fflush(stderr);

	int opus_err = 0;
	encoder = opus_encoder_create(actual_rate, channels, OPUS_APPLICATION_AUDIO, &opus_err);
	if (!encoder || opus_err != OPUS_OK) {
		if (pcm_capture_handle) {
			snd_pcm_close(pcm_capture_handle);
			pcm_capture_handle = NULL;
		}
		capture_initializing = 0;
		return -3;
	}

	// Configure encoder with optimized settings
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

	if (__builtin_expect(capture_stop_requested, 0)) {
		return -1;
	}

	SIMD_PREFETCH(out, 1, 0);
	SIMD_PREFETCH(pcm_buffer, 0, 0);
	SIMD_PREFETCH(pcm_buffer + 64, 0, 1);

	if (__builtin_expect(!capture_initialized || !pcm_capture_handle || !encoder || !opus_buf, 0)) {
		TRACE_LOG("[AUDIO_OUTPUT] jetkvm_audio_read_encode: Failed safety checks - capture_initialized=%d, pcm_capture_handle=%p, encoder=%p, opus_buf=%p\n",
				capture_initialized, pcm_capture_handle, encoder, opus_buf);
		return -1;
	}

retry_read:
	if (__builtin_expect(capture_stop_requested, 0)) {
		return -1;
	}

	pcm_rc = snd_pcm_readi(pcm_capture_handle, pcm_buffer, frame_size);

	if (__builtin_expect(pcm_rc < 0, 0)) {
		if (pcm_rc == -EPIPE) {
			recovery_attempts++;
			if (recovery_attempts > max_recovery_attempts) {
				return -1;
			}
			err = snd_pcm_prepare(pcm_capture_handle);
			if (err < 0) {
				snd_pcm_drop(pcm_capture_handle);
				err = snd_pcm_prepare(pcm_capture_handle);
				if (err < 0) return -1;
			}
			goto retry_read;
		} else if (pcm_rc == -EAGAIN) {
			snd_pcm_wait(pcm_capture_handle, sleep_milliseconds);
			goto retry_read;
		} else if (pcm_rc == -ESTRPIPE) {
			recovery_attempts++;
			if (recovery_attempts > max_recovery_attempts) {
				return -1;
			}
			uint8_t resume_attempts = 0;
			while ((err = snd_pcm_resume(pcm_capture_handle)) == -EAGAIN && resume_attempts < 10) {
				if (capture_stop_requested) return -1;
				snd_pcm_wait(pcm_capture_handle, sleep_milliseconds);
				resume_attempts++;
			}
			if (err < 0) {
				err = snd_pcm_prepare(pcm_capture_handle);
				if (err < 0) return -1;
			}
			return 0;
		} else if (pcm_rc == -ENODEV) {
			return -1;
		} else if (pcm_rc == -EIO) {
			recovery_attempts++;
			if (recovery_attempts <= max_recovery_attempts) {
				snd_pcm_drop(pcm_capture_handle);
				err = snd_pcm_prepare(pcm_capture_handle);
				if (err >= 0) {
					goto retry_read;
				}
			}
			return -1;
		} else {
			recovery_attempts++;
			if (recovery_attempts <= 1 && pcm_rc == -EINTR) {
				goto retry_read;
			} else if (recovery_attempts <= 1 && pcm_rc == -EBUSY) {
				snd_pcm_wait(pcm_capture_handle, 1); // Wait 1ms for device
				goto retry_read;
			}
			return -1;
		}
	}

	// Zero-pad if we got a short read
	if (__builtin_expect(pcm_rc < frame_size, 0)) {
		uint32_t remaining_samples = (frame_size - pcm_rc) * channels;
		simd_clear_samples_s16(&pcm_buffer[pcm_rc * channels], remaining_samples);
	}

	nb_bytes = opus_encode(encoder, pcm_buffer, frame_size, out, max_packet_size);
	return nb_bytes;
}

// AUDIO INPUT PATH FUNCTIONS (Client Microphone → Device Speakers)

/**
 * Initialize INPUT path (Opus decoder → device speakers)
 * Opens hw:1,0 (USB gadget) or "default" and creates Opus decoder
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

	if (decoder) {
		opus_decoder_destroy(decoder);
		decoder = NULL;
	}
	if (pcm_playback_handle) {
		snd_pcm_close(pcm_playback_handle);
		pcm_playback_handle = NULL;
	}

	err = safe_alsa_open(&pcm_playback_handle, alsa_playback_device, SND_PCM_STREAM_PLAYBACK);
	if (err < 0) {
		fprintf(stderr, "Failed to open ALSA playback device %s: %s\n",
		        alsa_playback_device, snd_strerror(err));
		fflush(stderr);
		err = safe_alsa_open(&pcm_playback_handle, "default", SND_PCM_STREAM_PLAYBACK);
		if (err < 0) {
			playback_initializing = 0;
			return -1;
		}
	}

	unsigned int actual_rate = 0;
	uint16_t actual_frame_size = 0;
	err = configure_alsa_device(pcm_playback_handle, "playback", &actual_rate, &actual_frame_size);
	if (err < 0) {
		snd_pcm_close(pcm_playback_handle);
		pcm_playback_handle = NULL;
		playback_initializing = 0;
		return -1;
	}

	fprintf(stderr, "INFO: playback: Initializing Opus decoder at %u Hz, %u channels, frame size %u\n",
	        actual_rate, channels, actual_frame_size);
	fflush(stderr);

	int opus_err = 0;
	decoder = opus_decoder_create(actual_rate, channels, &opus_err);
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

	if (__builtin_expect(playback_stop_requested, 0)) {
		return -1;
	}

	SIMD_PREFETCH(in, 0, 0);

	if (__builtin_expect(!playback_initialized || !pcm_playback_handle || !decoder || !opus_buf || opus_size <= 0, 0)) {
		TRACE_LOG("[AUDIO_INPUT] jetkvm_audio_decode_write: Failed safety checks - playback_initialized=%d, pcm_playback_handle=%p, decoder=%p, opus_buf=%p, opus_size=%d\n",
				playback_initialized, pcm_playback_handle, decoder, opus_buf, opus_size);
		return -1;
	}

	if (opus_size > max_packet_size) {
		TRACE_LOG("[AUDIO_INPUT] jetkvm_audio_decode_write: Opus packet too large - size=%d, max=%d\n", opus_size, max_packet_size);
		return -1;
	}
		TRACE_LOG("[AUDIO_INPUT] jetkvm_audio_decode_write: Processing Opus packet - size=%d bytes\n", opus_size);

	// Decode Opus packet to PCM (FEC automatically applied if embedded in packet)
	// decode_fec=0 means normal decode (FEC data is used automatically when present)
	pcm_frames = opus_decode(decoder, in, opus_size, pcm_buffer, frame_size, 0);

	if (__builtin_expect(pcm_frames < 0, 0)) {
		// Decode failed - attempt packet loss concealment using FEC from previous packet
		TRACE_LOG("[AUDIO_INPUT] jetkvm_audio_decode_write: Opus decode failed with error %d, attempting packet loss concealment\n", pcm_frames);

		// decode_fec=1 means use FEC data from the NEXT packet to reconstruct THIS lost packet
		pcm_frames = opus_decode(decoder, NULL, 0, pcm_buffer, frame_size, 1);
		if (pcm_frames < 0) {
		TRACE_LOG("[AUDIO_INPUT] jetkvm_audio_decode_write: Packet loss concealment also failed with error %d\n", pcm_frames);
			return -1;
		}
		TRACE_LOG("[AUDIO_INPUT] jetkvm_audio_decode_write: Packet loss concealment succeeded, recovered %d frames\n", pcm_frames);
	} else
		TRACE_LOG("[AUDIO_INPUT] jetkvm_audio_decode_write: Opus decode successful - decoded %d PCM frames\n", pcm_frames);

retry_write:
	if (__builtin_expect(playback_stop_requested, 0)) {
		return -1;
	}

	pcm_rc = snd_pcm_writei(pcm_playback_handle, pcm_buffer, pcm_frames);
	if (__builtin_expect(pcm_rc < 0, 0)) {
		TRACE_LOG("[AUDIO_INPUT] jetkvm_audio_decode_write: ALSA write failed with error %d (%s), attempt %d/%d\n",
				pcm_rc, snd_strerror(pcm_rc), recovery_attempts + 1, max_recovery_attempts);

		if (pcm_rc == -EPIPE) {
			recovery_attempts++;
			if (recovery_attempts > max_recovery_attempts) {
		TRACE_LOG("[AUDIO_INPUT] jetkvm_audio_decode_write: Buffer underrun recovery failed after %d attempts\n", max_recovery_attempts);
				return -2;
			}
		TRACE_LOG("[AUDIO_INPUT] jetkvm_audio_decode_write: Buffer underrun detected, attempting recovery (attempt %d)\n", recovery_attempts);
			err = snd_pcm_prepare(pcm_playback_handle);
			if (err < 0) {
		TRACE_LOG("[AUDIO_INPUT] jetkvm_audio_decode_write: snd_pcm_prepare failed (%s), trying drop+prepare\n", snd_strerror(err));
				snd_pcm_drop(pcm_playback_handle);
				err = snd_pcm_prepare(pcm_playback_handle);
				if (err < 0) {
		TRACE_LOG("[AUDIO_INPUT] jetkvm_audio_decode_write: drop+prepare recovery failed (%s)\n", snd_strerror(err));
					return -2;
				}
			}
		TRACE_LOG("[AUDIO_INPUT] jetkvm_audio_decode_write: Buffer underrun recovery successful, retrying write\n");
			goto retry_write;
		} else if (pcm_rc == -ESTRPIPE) {
			recovery_attempts++;
			if (recovery_attempts > max_recovery_attempts) {
		TRACE_LOG("[AUDIO_INPUT] jetkvm_audio_decode_write: Device suspend recovery failed after %d attempts\n", max_recovery_attempts);
				return -2;
			}
		TRACE_LOG("[AUDIO_INPUT] jetkvm_audio_decode_write: Device suspended, attempting resume (attempt %d)\n", recovery_attempts);
			uint8_t resume_attempts = 0;
			while ((err = snd_pcm_resume(pcm_playback_handle)) == -EAGAIN && resume_attempts < 10) {
				if (playback_stop_requested) return -1;
				snd_pcm_wait(pcm_playback_handle, sleep_milliseconds);
				resume_attempts++;
			}
			if (err < 0) {
		TRACE_LOG("[AUDIO_INPUT] jetkvm_audio_decode_write: Device resume failed (%s), trying prepare fallback\n", snd_strerror(err));
				err = snd_pcm_prepare(pcm_playback_handle);
				if (err < 0) {
		TRACE_LOG("[AUDIO_INPUT] jetkvm_audio_decode_write: Prepare fallback failed (%s)\n", snd_strerror(err));
					return -2;
				}
			}
		TRACE_LOG("[AUDIO_INPUT] jetkvm_audio_decode_write: Device suspend recovery successful, skipping frame\n");
			return 0;
		} else if (pcm_rc == -ENODEV) {
		TRACE_LOG("[AUDIO_INPUT] jetkvm_audio_decode_write: Device disconnected (ENODEV) - critical error\n");
			return -2;
		} else if (pcm_rc == -EIO) {
			recovery_attempts++;
			if (recovery_attempts <= max_recovery_attempts) {
		TRACE_LOG("[AUDIO_INPUT] jetkvm_audio_decode_write: I/O error detected, attempting recovery\n");
				snd_pcm_drop(pcm_playback_handle);
				err = snd_pcm_prepare(pcm_playback_handle);
				if (err >= 0) {
		TRACE_LOG("[AUDIO_INPUT] jetkvm_audio_decode_write: I/O error recovery successful, retrying write\n");
					goto retry_write;
				}
		TRACE_LOG("[AUDIO_INPUT] jetkvm_audio_decode_write: I/O error recovery failed (%s)\n", snd_strerror(err));
			}
			return -2;
		} else if (pcm_rc == -EAGAIN) {
			recovery_attempts++;
			if (recovery_attempts <= max_recovery_attempts) {
		TRACE_LOG("[AUDIO_INPUT] jetkvm_audio_decode_write: Device not ready (EAGAIN), waiting and retrying\n");
				snd_pcm_wait(pcm_playback_handle, 1);  // Wait 1ms
				goto retry_write;
			}
		TRACE_LOG("[AUDIO_INPUT] jetkvm_audio_decode_write: Device not ready recovery failed after %d attempts\n", max_recovery_attempts);
			return -2;
		} else {
			recovery_attempts++;
			if (recovery_attempts <= 1 && (pcm_rc == -EINTR || pcm_rc == -EBUSY)) {
		TRACE_LOG("[AUDIO_INPUT] jetkvm_audio_decode_write: Transient error %d (%s), retrying once\n", pcm_rc, snd_strerror(pcm_rc));
				snd_pcm_wait(pcm_playback_handle, 1);  // Wait 1ms
				goto retry_write;
			}
		TRACE_LOG("[AUDIO_INPUT] jetkvm_audio_decode_write: Unrecoverable error %d (%s)\n", pcm_rc, snd_strerror(pcm_rc));
			return -2;
		}
	}
		TRACE_LOG("[AUDIO_INPUT] jetkvm_audio_decode_write: Successfully wrote %d PCM frames to device\n", pcm_frames);
	return pcm_frames;
}

// CLEANUP FUNCTIONS

void jetkvm_audio_playback_close() {
	playback_stop_requested = 1;
	__sync_synchronize();

	while (playback_initializing) {
		sched_yield();
	}

	if (__sync_bool_compare_and_swap(&playback_initialized, 1, 0) == 0) {
		playback_stop_requested = 0;
		return;
	}

	if (decoder) {
		opus_decoder_destroy(decoder);
		decoder = NULL;
	}
	if (pcm_playback_handle) {
		snd_pcm_drop(pcm_playback_handle);
		snd_pcm_close(pcm_playback_handle);
		pcm_playback_handle = NULL;
	}

	playback_stop_requested = 0;
}

void jetkvm_audio_capture_close() {
	capture_stop_requested = 1;
	__sync_synchronize();

	while (capture_initializing) {
		sched_yield();
	}

	if (__sync_bool_compare_and_swap(&capture_initialized, 1, 0) == 0) {
		capture_stop_requested = 0;
		return;
	}

	if (pcm_capture_handle) {
		snd_pcm_drop(pcm_capture_handle);
		snd_pcm_close(pcm_capture_handle);
		pcm_capture_handle = NULL;
	}
	if (encoder) {
		opus_encoder_destroy(encoder);
		encoder = NULL;
	}

	capture_stop_requested = 0;
}
