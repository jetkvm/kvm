/*
 * JetKVM Audio Processing Module
 * 
 * This module handles bidirectional audio processing for JetKVM:
 * - Audio INPUT: Client microphone → Device speakers (decode Opus → ALSA playback)
 * - Audio OUTPUT: TC358743 HDMI audio → Client speakers (ALSA capture → encode Opus)
 */

#include <alsa/asoundlib.h>
#include <opus.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <errno.h>

// ARM NEON SIMD support (always available on JetKVM's ARM Cortex-A7)
#include <arm_neon.h>

#define SIMD_ALIGN __attribute__((aligned(16)))
#define SIMD_PREFETCH(addr, rw, locality) __builtin_prefetch(addr, rw, locality)

static int trace_logging_enabled = 0;
static int simd_initialized = 0;

static void simd_init_once(void) {
    if (simd_initialized) return;
    simd_initialized = 1;
}

// ============================================================================
// GLOBAL STATE VARIABLES
// ============================================================================

// ALSA device handles
static snd_pcm_t *pcm_capture_handle = NULL;  // OUTPUT: TC358743 HDMI audio → client
static snd_pcm_t *pcm_playback_handle = NULL; // INPUT: Client microphone → device speakers

// Opus codec instances
static OpusEncoder *encoder = NULL;
static OpusDecoder *decoder = NULL;

// Audio format (S16_LE @ 48kHz stereo)
static int sample_rate = 48000;
static int channels = 2;
static int frame_size = 960;  // 20ms frames at 48kHz

// Opus encoder settings (optimized for minimal CPU ~0.5% on RV1106)
static int opus_bitrate = 96000;        // 96 kbps
static int opus_complexity = 1;         // Complexity 1 (minimal CPU)
static int opus_vbr = 1;                // Variable bitrate enabled
static int opus_vbr_constraint = 1;     // Constrained VBR for predictable bandwidth
static int opus_signal_type = -1000;    // OPUS_AUTO (-1000)
static int opus_bandwidth = 1103;       // OPUS_BANDWIDTH_WIDEBAND (1103)
static int opus_dtx = 0;                // DTX disabled
static int opus_lsb_depth = 16;         // 16-bit depth matches S16_LE

// Network configuration
static int max_packet_size = 1500;

// ALSA retry configuration
static int sleep_microseconds = 1000;
static int max_attempts_global = 5;
static int max_backoff_us_global = 500000;

// Buffer optimization (1 = use 2-period ultra-low latency, 0 = use 4-period balanced)
static const int optimized_buffer_size = 1;


// ============================================================================
// FUNCTION DECLARATIONS
// ============================================================================

int jetkvm_audio_capture_init();
void jetkvm_audio_capture_close();
int jetkvm_audio_read_encode(void *opus_buf);

int jetkvm_audio_playback_init();
void jetkvm_audio_playback_close();
int jetkvm_audio_decode_write(void *opus_buf, int opus_size);

void update_audio_constants(int bitrate, int complexity, int vbr, int vbr_constraint,
                           int signal_type, int bandwidth, int dtx, int lsb_depth, int sr, int ch,
                           int fs, int max_pkt, int sleep_us, int max_attempts, int max_backoff);
void set_trace_logging(int enabled);
int update_opus_encoder_params(int bitrate, int complexity, int vbr, int vbr_constraint,
                              int signal_type, int bandwidth, int dtx);

// ============================================================================
// CONFIGURATION FUNCTIONS
// ============================================================================

/**
 * Sync configuration from Go to C
 */
void update_audio_constants(int bitrate, int complexity, int vbr, int vbr_constraint,
                           int signal_type, int bandwidth, int dtx, int lsb_depth, int sr, int ch,
                           int fs, int max_pkt, int sleep_us, int max_attempts, int max_backoff) {
    opus_bitrate = bitrate;
    opus_complexity = complexity;
    opus_vbr = vbr;
    opus_vbr_constraint = vbr_constraint;
    opus_signal_type = signal_type;
    opus_bandwidth = bandwidth;
    opus_dtx = dtx;
    opus_lsb_depth = lsb_depth;
    sample_rate = sr;
    channels = ch;
    frame_size = fs;
    max_packet_size = max_pkt;
    sleep_microseconds = sleep_us;
    max_attempts_global = max_attempts;
    max_backoff_us_global = max_backoff;
}

/**
 * Enable/disable trace logging (zero overhead when disabled)
 */
void set_trace_logging(int enabled) {
    trace_logging_enabled = enabled;
}

// ============================================================================
// SIMD-OPTIMIZED BUFFER OPERATIONS (ARM NEON)
// ============================================================================

/**
 * Clear audio buffer using NEON (8 samples/iteration)
 */
static inline void simd_clear_samples_s16(short *buffer, int samples) {
    simd_init_once();

    const int16x8_t zero = vdupq_n_s16(0);
    int simd_samples = samples & ~7;

    for (int i = 0; i < simd_samples; i += 8) {
        vst1q_s16(&buffer[i], zero);
    }

    for (int i = simd_samples; i < samples; i++) {
        buffer[i] = 0;
    }
}

/**
 * Interleave L/R channels using NEON (8 frames/iteration)
 */
static inline void simd_interleave_stereo_s16(const short *left, const short *right,
                                             short *output, int frames) {
    simd_init_once();
    int simd_frames = frames & ~7;

    for (int i = 0; i < simd_frames; i += 8) {
        int16x8_t left_vec = vld1q_s16(&left[i]);
        int16x8_t right_vec = vld1q_s16(&right[i]);
        int16x8x2_t interleaved = vzipq_s16(left_vec, right_vec);
        vst1q_s16(&output[i * 2], interleaved.val[0]);
        vst1q_s16(&output[i * 2 + 8], interleaved.val[1]);
    }

    for (int i = simd_frames; i < frames; i++) {
        output[i * 2] = left[i];
        output[i * 2 + 1] = right[i];
    }
}

/**
 * Apply gain using NEON Q15 fixed-point math (8 samples/iteration)
 */
static inline void simd_scale_volume_s16(short *samples, int count, float volume) {
    simd_init_once();
    int16_t vol_fixed = (int16_t)(volume * 32767.0f);
    int16x8_t vol_vec = vdupq_n_s16(vol_fixed);
    int simd_count = count & ~7;

    for (int i = 0; i < simd_count; i += 8) {
        int16x8_t samples_vec = vld1q_s16(&samples[i]);
        int32x4_t low_result = vmull_s16(vget_low_s16(samples_vec), vget_low_s16(vol_vec));
        int32x4_t high_result = vmull_s16(vget_high_s16(samples_vec), vget_high_s16(vol_vec));
        int16x4_t low_narrow = vshrn_n_s32(low_result, 15);
        int16x4_t high_narrow = vshrn_n_s32(high_result, 15);
        int16x8_t result = vcombine_s16(low_narrow, high_narrow);
        vst1q_s16(&samples[i], result);
    }

    for (int i = simd_count; i < count; i++) {
        samples[i] = (short)((samples[i] * vol_fixed) >> 15);
    }
}

/**
 * Byte-swap 16-bit samples using NEON (8 samples/iteration)
 */
static inline void simd_swap_endian_s16(short *samples, int count) {
    int simd_count = count & ~7;

    for (int i = 0; i < simd_count; i += 8) {
        uint16x8_t samples_vec = vld1q_u16((uint16_t*)&samples[i]);
        uint8x16_t samples_u8 = vreinterpretq_u8_u16(samples_vec);
        uint8x16_t swapped_u8 = vrev16q_u8(samples_u8);
        uint16x8_t swapped = vreinterpretq_u16_u8(swapped_u8);
        vst1q_u16((uint16_t*)&samples[i], swapped);
    }

    for (int i = simd_count; i < count; i++) {
        samples[i] = __builtin_bswap16(samples[i]);
    }
}

/**
 * Convert S16 to float using NEON (4 samples/iteration)
 */
static inline void simd_s16_to_float(const short *input, float *output, int count) {
    const float scale = 1.0f / 32768.0f;
    float32x4_t scale_vec = vdupq_n_f32(scale);
    int simd_count = count & ~3;

    for (int i = 0; i < simd_count; i += 4) {
        int16x4_t s16_data = vld1_s16(input + i);
        int32x4_t s32_data = vmovl_s16(s16_data);
        float32x4_t float_data = vcvtq_f32_s32(s32_data);
        float32x4_t scaled = vmulq_f32(float_data, scale_vec);
        vst1q_f32(output + i, scaled);
    }

    for (int i = simd_count; i < count; i++) {
        output[i] = (float)input[i] * scale;
    }
}

/**
 * Convert float to S16 using NEON (4 samples/iteration)
 */
static inline void simd_float_to_s16(const float *input, short *output, int count) {
    const float scale = 32767.0f;
    float32x4_t scale_vec = vdupq_n_f32(scale);
    int simd_count = count & ~3;

    for (int i = 0; i < simd_count; i += 4) {
        float32x4_t float_data = vld1q_f32(input + i);
        float32x4_t scaled = vmulq_f32(float_data, scale_vec);
        int32x4_t s32_data = vcvtq_s32_f32(scaled);
        int16x4_t s16_data = vqmovn_s32(s32_data);
        vst1_s16(output + i, s16_data);
    }

    for (int i = simd_count; i < count; i++) {
        float scaled = input[i] * scale;
        output[i] = (short)__builtin_fmaxf(__builtin_fminf(scaled, 32767.0f), -32768.0f);
    }
}

/**
 * Mono → stereo (duplicate samples) using NEON (4 frames/iteration)
 */
static inline void simd_mono_to_stereo_s16(const short *mono, short *stereo, int frames) {
	int simd_frames = frames & ~3;
	for (int i = 0; i < simd_frames; i += 4) {
		int16x4_t mono_data = vld1_s16(mono + i);
		int16x4x2_t stereo_data = {mono_data, mono_data};
		vst2_s16(stereo + i * 2, stereo_data);
	}

	for (int i = simd_frames; i < frames; i++) {
		stereo[i * 2] = mono[i];
		stereo[i * 2 + 1] = mono[i];
	}
}

/**
 * Stereo → mono (average L+R) using NEON (4 frames/iteration)
 */
static inline void simd_stereo_to_mono_s16(const short *stereo, short *mono, int frames) {
	int simd_frames = frames & ~3;
	for (int i = 0; i < simd_frames; i += 4) {
		int16x4x2_t stereo_data = vld2_s16(stereo + i * 2);
		int32x4_t left_wide = vmovl_s16(stereo_data.val[0]);
		int32x4_t right_wide = vmovl_s16(stereo_data.val[1]);
		int32x4_t sum = vaddq_s32(left_wide, right_wide);
		int32x4_t avg = vshrq_n_s32(sum, 1);
		int16x4_t mono_data = vqmovn_s32(avg);
		vst1_s16(mono + i, mono_data);
	}

	for (int i = simd_frames; i < frames; i++) {
		mono[i] = (stereo[i * 2] + stereo[i * 2 + 1]) / 2;
	}
}

/**
 * Apply L/R balance using NEON (4 frames/iteration)
 */
static inline void simd_apply_stereo_balance_s16(short *stereo, int frames, float balance) {
	float left_gain = balance <= 0.0f ? 1.0f : 1.0f - balance;
	float right_gain = balance >= 0.0f ? 1.0f : 1.0f + balance;
	float32x4_t left_gain_vec = vdupq_n_f32(left_gain);
	float32x4_t right_gain_vec = vdupq_n_f32(right_gain);
	int simd_frames = frames & ~3;

	for (int i = 0; i < simd_frames; i += 4) {
		int16x4x2_t stereo_data = vld2_s16(stereo + i * 2);
		int32x4_t left_wide = vmovl_s16(stereo_data.val[0]);
		int32x4_t right_wide = vmovl_s16(stereo_data.val[1]);
		float32x4_t left_float = vcvtq_f32_s32(left_wide);
		float32x4_t right_float = vcvtq_f32_s32(right_wide);
		left_float = vmulq_f32(left_float, left_gain_vec);
		right_float = vmulq_f32(right_float, right_gain_vec);
		int32x4_t left_result = vcvtq_s32_f32(left_float);
		int32x4_t right_result = vcvtq_s32_f32(right_float);
		stereo_data.val[0] = vqmovn_s32(left_result);
		stereo_data.val[1] = vqmovn_s32(right_result);
		vst2_s16(stereo + i * 2, stereo_data);
	}

	for (int i = simd_frames; i < frames; i++) {
		stereo[i * 2] = (short)(stereo[i * 2] * left_gain);
		stereo[i * 2 + 1] = (short)(stereo[i * 2 + 1] * right_gain);
	}
}

/**
 * Deinterleave stereo → L/R channels using NEON (4 frames/iteration)
 */
static inline void simd_deinterleave_stereo_s16(const short *interleaved, short *left,
                                               short *right, int frames) {
	int simd_frames = frames & ~3;
	for (int i = 0; i < simd_frames; i += 4) {
		int16x4x2_t stereo_data = vld2_s16(interleaved + i * 2);
		vst1_s16(left + i, stereo_data.val[0]);
		vst1_s16(right + i, stereo_data.val[1]);
	}

	for (int i = simd_frames; i < frames; i++) {
		left[i] = interleaved[i * 2];
		right[i] = interleaved[i * 2 + 1];
	}
}

/**
 * Find max absolute sample value for silence detection using NEON (8 samples/iteration)
 * Used to detect silence (threshold < 50 = ~0.15% max volume)
 */
static inline short simd_find_max_abs_s16(const short *samples, int count) {
	int16x8_t max_vec = vdupq_n_s16(0);
	int simd_count = count & ~7;

	for (int i = 0; i < simd_count; i += 8) {
		int16x8_t samples_vec = vld1q_s16(&samples[i]);
		int16x8_t abs_vec = vabsq_s16(samples_vec);
		max_vec = vmaxq_s16(max_vec, abs_vec);
	}

	int16x4_t max_half = vmax_s16(vget_low_s16(max_vec), vget_high_s16(max_vec));
	int16x4_t max_folded = vpmax_s16(max_half, max_half);
	max_folded = vpmax_s16(max_folded, max_folded);
	short max_sample = vget_lane_s16(max_folded, 0);

	for (int i = simd_count; i < count; i++) {
		short abs_sample = samples[i] < 0 ? -samples[i] : samples[i];
		if (abs_sample > max_sample) {
			max_sample = abs_sample;
		}
	}

	return max_sample;
}

// ============================================================================
// INITIALIZATION STATE TRACKING
// ============================================================================

static volatile int capture_initializing = 0;
static volatile int capture_initialized = 0;
static volatile int playback_initializing = 0;
static volatile int playback_initialized = 0;

/**
 * Update Opus encoder settings at runtime
 * @return 0 on success, -1 if not initialized, >0 if some settings failed
 */
int update_opus_encoder_params(int bitrate, int complexity, int vbr, int vbr_constraint,
                              int signal_type, int bandwidth, int dtx) {
    if (!encoder || !capture_initialized) {
        return -1;
    }

    opus_bitrate = bitrate;
    opus_complexity = complexity;
    opus_vbr = vbr;
    opus_vbr_constraint = vbr_constraint;
    opus_signal_type = signal_type;
    opus_bandwidth = bandwidth;
    opus_dtx = dtx;

    int result = 0;
    result |= opus_encoder_ctl(encoder, OPUS_SET_BITRATE(opus_bitrate));
    result |= opus_encoder_ctl(encoder, OPUS_SET_COMPLEXITY(opus_complexity));
    result |= opus_encoder_ctl(encoder, OPUS_SET_VBR(opus_vbr));
    result |= opus_encoder_ctl(encoder, OPUS_SET_VBR_CONSTRAINT(opus_vbr_constraint));
    result |= opus_encoder_ctl(encoder, OPUS_SET_SIGNAL(opus_signal_type));
    result |= opus_encoder_ctl(encoder, OPUS_SET_BANDWIDTH(opus_bandwidth));
    result |= opus_encoder_ctl(encoder, OPUS_SET_DTX(opus_dtx));

    return result;
}

// ============================================================================
// ALSA UTILITY FUNCTIONS
// ============================================================================

/**
 * Open ALSA device with exponential backoff retry
 * @return 0 on success, negative error code on failure
 */
static int safe_alsa_open(snd_pcm_t **handle, const char *device, snd_pcm_stream_t stream) {
	int attempt = 0;
	int err;
	int backoff_us = sleep_microseconds;

	while (attempt < max_attempts_global) {
		err = snd_pcm_open(handle, device, stream, SND_PCM_NONBLOCK);
		if (err >= 0) {
			snd_pcm_nonblock(*handle, 0);
			return 0;
		}

		attempt++;

		if (err == -EBUSY || err == -EAGAIN) {
			usleep(backoff_us);
			backoff_us = (backoff_us * 2 < max_backoff_us_global) ? backoff_us * 2 : max_backoff_us_global;
		} else if (err == -ENODEV || err == -ENOENT) {
			usleep(backoff_us * 2);
			backoff_us = (backoff_us * 2 < max_backoff_us_global) ? backoff_us * 2 : max_backoff_us_global;
		} else if (err == -EPERM || err == -EACCES) {
			usleep(backoff_us / 2);
		} else {
			usleep(backoff_us);
			backoff_us = (backoff_us * 2 < max_backoff_us_global) ? backoff_us * 2 : max_backoff_us_global;
		}
	}
	return err;
}

/**
 * Configure ALSA device (S16_LE @ 48kHz stereo with optimized buffering)
 * @param handle ALSA PCM handle
 * @param device_name Unused (for debugging only)
 * @return 0 on success, negative error code on failure
 */
static int configure_alsa_device(snd_pcm_t *handle, const char *device_name) {
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

	err = snd_pcm_hw_params_set_rate(handle, params, sample_rate, 0);
	if (err < 0) {
		unsigned int rate = sample_rate;
		err = snd_pcm_hw_params_set_rate_near(handle, params, &rate, 0);
		if (err < 0) return err;
	}

	snd_pcm_uframes_t period_size = optimized_buffer_size ? frame_size : frame_size / 2;
	if (period_size < 64) period_size = 64;

	err = snd_pcm_hw_params_set_period_size_near(handle, params, &period_size, 0);
	if (err < 0) return err;

	snd_pcm_uframes_t buffer_size = optimized_buffer_size ? period_size * 2 : period_size * 4;
	err = snd_pcm_hw_params_set_buffer_size_near(handle, params, &buffer_size);
	if (err < 0) return err;

	err = snd_pcm_hw_params(handle, params);
	if (err < 0) return err;

	err = snd_pcm_sw_params_current(handle, sw_params);
	if (err < 0) return err;

	err = snd_pcm_sw_params_set_start_threshold(handle, sw_params, period_size);
	if (err < 0) return err;

	err = snd_pcm_sw_params_set_avail_min(handle, sw_params, period_size);
	if (err < 0) return err;

	err = snd_pcm_sw_params(handle, sw_params);
	if (err < 0) return err;

	return snd_pcm_prepare(handle);
}

// ============================================================================
// AUDIO OUTPUT PATH FUNCTIONS (TC358743 HDMI Audio → Client Speakers)
// ============================================================================

/**
 * Initialize OUTPUT path (TC358743 HDMI capture → Opus encoder)
 * Opens hw:0,0 (TC358743) and creates Opus encoder with optimized settings
 * @return 0 on success, -EBUSY if initializing, -1/-2/-3 on errors
 */
int jetkvm_audio_capture_init() {
	int err;

	simd_init_once();

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

	err = safe_alsa_open(&pcm_capture_handle, "hw:0,0", SND_PCM_STREAM_CAPTURE);
	if (err < 0) {
		capture_initializing = 0;
		return -1;
	}

	err = configure_alsa_device(pcm_capture_handle, "capture");
	if (err < 0) {
		snd_pcm_close(pcm_capture_handle);
		pcm_capture_handle = NULL;
		capture_initializing = 0;
		return -2;
	}

	int opus_err = 0;
	encoder = opus_encoder_create(sample_rate, channels, OPUS_APPLICATION_AUDIO, &opus_err);
	if (!encoder || opus_err != OPUS_OK) {
		if (pcm_capture_handle) {
			snd_pcm_close(pcm_capture_handle);
			pcm_capture_handle = NULL;
		}
		capture_initializing = 0;
		return -3;
	}

	opus_encoder_ctl(encoder, OPUS_SET_BITRATE(opus_bitrate));
	opus_encoder_ctl(encoder, OPUS_SET_COMPLEXITY(opus_complexity));
	opus_encoder_ctl(encoder, OPUS_SET_VBR(opus_vbr));
	opus_encoder_ctl(encoder, OPUS_SET_VBR_CONSTRAINT(opus_vbr_constraint));
	opus_encoder_ctl(encoder, OPUS_SET_SIGNAL(opus_signal_type));
	opus_encoder_ctl(encoder, OPUS_SET_BANDWIDTH(opus_bandwidth));
	opus_encoder_ctl(encoder, OPUS_SET_DTX(opus_dtx));
	opus_encoder_ctl(encoder, OPUS_SET_LSB_DEPTH(opus_lsb_depth));

	// Enable in-band FEC for packet loss resilience (adds ~2-5% bitrate)
	opus_encoder_ctl(encoder, OPUS_SET_INBAND_FEC(1));
	opus_encoder_ctl(encoder, OPUS_SET_PACKET_LOSS_PERC(10));

	capture_initialized = 1;
	capture_initializing = 0;
	return 0;
}

/**
 * Read HDMI audio, encode to Opus (OUTPUT path hot function)
 * Process: ALSA capture → silence detection → 2.5x gain → Opus encode
 * @return >0 = Opus bytes, 0 = silence/no data, -1 = error
 */
__attribute__((hot)) int jetkvm_audio_read_encode(void * __restrict__ opus_buf) {
	static short SIMD_ALIGN pcm_buffer[1920];
	static short prev_max_sample = 0;  // Track previous frame's peak for discontinuity detection
	static int silence_count = 0;       // Count consecutive silent frames
	unsigned char * __restrict__ out = (unsigned char*)opus_buf;

	SIMD_PREFETCH(out, 1, 3);
	SIMD_PREFETCH(pcm_buffer, 0, 3);
	int err = 0;
	int recovery_attempts = 0;
	const int max_recovery_attempts = 3;

	if (__builtin_expect(!capture_initialized || !pcm_capture_handle || !encoder || !opus_buf, 0)) {
		if (trace_logging_enabled) {
			printf("[AUDIO_OUTPUT] jetkvm_audio_read_encode: Failed safety checks - capture_initialized=%d, pcm_capture_handle=%p, encoder=%p, opus_buf=%p\n",
				capture_initialized, pcm_capture_handle, encoder, opus_buf);
		}
		return -1;
	}

retry_read:
	;
	int pcm_rc = snd_pcm_readi(pcm_capture_handle, pcm_buffer, frame_size);

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
			return 0;
		} else if (pcm_rc == -ESTRPIPE) {
			recovery_attempts++;
			if (recovery_attempts > max_recovery_attempts) {
				return -1;
			}
			int resume_attempts = 0;
			while ((err = snd_pcm_resume(pcm_capture_handle)) == -EAGAIN && resume_attempts < 10) {
				usleep(sleep_microseconds);
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
				usleep(sleep_microseconds / 2);
				goto retry_read;
			}
			return -1;
		}
	}

	if (__builtin_expect(pcm_rc < frame_size, 0)) {
		int remaining_samples = (frame_size - pcm_rc) * channels;
		simd_clear_samples_s16(&pcm_buffer[pcm_rc * channels], remaining_samples);
	}

	// Silence detection: only skip true silence (< 50 = ~0.15% of max volume)
	int total_samples = frame_size * channels;
	short max_sample = simd_find_max_abs_s16(pcm_buffer, total_samples);

	if (max_sample < 50) {
		silence_count++;
		if (silence_count > 2) {
			prev_max_sample = 0;  // Reset after extended silence
		}
		if (trace_logging_enabled) {
			printf("[AUDIO_OUTPUT] jetkvm_audio_read_encode: Silence detected (max=%d), skipping frame\n", max_sample);
		}
		return 0;
	}

	// Detect audio discontinuity (video seek): sudden level change after silence
	// If audio level jumps >4x after 2+ silent frames, likely a seek occurred
	if (silence_count >= 2 && prev_max_sample > 0) {
		int level_ratio = (max_sample > prev_max_sample) ?
			(max_sample / (prev_max_sample + 1)) :
			(prev_max_sample / (max_sample + 1));

		if (level_ratio > 4) {
			if (trace_logging_enabled) {
				printf("[AUDIO_OUTPUT] Discontinuity detected (level jump %dx: %d→%d), resetting encoder\n",
					level_ratio, prev_max_sample, max_sample);
			}
			// Reset Opus encoder state to prevent mixing old/new audio context
			opus_encoder_ctl(encoder, OPUS_RESET_STATE);
			// Drop and reprepare ALSA to flush any buffered old audio
			snd_pcm_drop(pcm_capture_handle);
			snd_pcm_prepare(pcm_capture_handle);
		}
	}

	silence_count = 0;
	prev_max_sample = max_sample;

	// Apply moderate 2.5x gain to prevent quantization noise on transients
	// Balances between being audible at low volumes and not overdriving at high volumes
	simd_scale_volume_s16(pcm_buffer, frame_size * channels, 2.5f);

	int nb_bytes = opus_encode(encoder, pcm_buffer, frame_size, out, max_packet_size);

	if (trace_logging_enabled && nb_bytes > 0) {
		printf("[AUDIO_OUTPUT] jetkvm_audio_read_encode: Successfully encoded %d PCM frames to %d Opus bytes\n", pcm_rc, nb_bytes);
	}

	return nb_bytes;
}

// ============================================================================
// AUDIO INPUT PATH FUNCTIONS (Client Microphone → Device Speakers)
// ============================================================================

/**
 * Initialize INPUT path (Opus decoder → device speakers)
 * Opens hw:1,0 (USB gadget) or "default" and creates Opus decoder
 * @return 0 on success, -EBUSY if initializing, -1/-2 on errors
 */
int jetkvm_audio_playback_init() {
	int err;

	simd_init_once();

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

	err = safe_alsa_open(&pcm_playback_handle, "hw:1,0", SND_PCM_STREAM_PLAYBACK);
	if (err < 0) {
		err = safe_alsa_open(&pcm_playback_handle, "default", SND_PCM_STREAM_PLAYBACK);
		if (err < 0) {
			playback_initializing = 0;
			return -1;
		}
	}

	err = configure_alsa_device(pcm_playback_handle, "playback");
	if (err < 0) {
		snd_pcm_close(pcm_playback_handle);
		pcm_playback_handle = NULL;
		playback_initializing = 0;
		return -1;
	}

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

/**
 * Decode Opus, write to device speakers (INPUT path hot function)
 * Process: Opus decode → ALSA write with packet loss concealment
 * @return >0 = PCM frames written, 0 = frame skipped, -1/-2 = error
 */
__attribute__((hot)) int jetkvm_audio_decode_write(void * __restrict__ opus_buf, int opus_size) {
	static short __attribute__((aligned(16))) pcm_buffer[1920];
	unsigned char * __restrict__ in = (unsigned char*)opus_buf;

	SIMD_PREFETCH(in, 0, 3);
	int err = 0;
	int recovery_attempts = 0;
	const int max_recovery_attempts = 3;

	if (__builtin_expect(!playback_initialized || !pcm_playback_handle || !decoder || !opus_buf || opus_size <= 0, 0)) {
		if (trace_logging_enabled) {
			printf("[AUDIO_INPUT] jetkvm_audio_decode_write: Failed safety checks - playback_initialized=%d, pcm_playback_handle=%p, decoder=%p, opus_buf=%p, opus_size=%d\n",
				playback_initialized, pcm_playback_handle, decoder, opus_buf, opus_size);
		}
		return -1;
	}

	if (opus_size > max_packet_size) {
		if (trace_logging_enabled) {
			printf("[AUDIO_INPUT] jetkvm_audio_decode_write: Opus packet too large - size=%d, max=%d\n", opus_size, max_packet_size);
		}
		return -1;
	}

	if (trace_logging_enabled) {
		printf("[AUDIO_INPUT] jetkvm_audio_decode_write: Processing Opus packet - size=%d bytes\n", opus_size);
	}

	// Decode normally (FEC is automatically used if available in the packet)
	int pcm_frames = opus_decode(decoder, in, opus_size, pcm_buffer, frame_size, 0);
	if (__builtin_expect(pcm_frames < 0, 0)) {
		if (trace_logging_enabled) {
			printf("[AUDIO_INPUT] jetkvm_audio_decode_write: Opus decode failed with error %d, attempting packet loss concealment\n", pcm_frames);
		}
		// Packet loss concealment: decode using FEC from next packet (if available)
		pcm_frames = opus_decode(decoder, NULL, 0, pcm_buffer, frame_size, 1);
		if (pcm_frames < 0) {
			if (trace_logging_enabled) {
				printf("[AUDIO_INPUT] jetkvm_audio_decode_write: Packet loss concealment also failed with error %d\n", pcm_frames);
			}
			return -1;
		}
		if (trace_logging_enabled) {
			printf("[AUDIO_INPUT] jetkvm_audio_decode_write: Packet loss concealment succeeded, recovered %d frames\n", pcm_frames);
		}
	} else if (trace_logging_enabled) {
		printf("[AUDIO_INPUT] jetkvm_audio_decode_write: Opus decode successful - decoded %d PCM frames\n", pcm_frames);
	}

retry_write:
	;
	int pcm_rc = snd_pcm_writei(pcm_playback_handle, pcm_buffer, pcm_frames);
	if (__builtin_expect(pcm_rc < 0, 0)) {
		if (trace_logging_enabled) {
			printf("[AUDIO_INPUT] jetkvm_audio_decode_write: ALSA write failed with error %d (%s), attempt %d/%d\n",
				pcm_rc, snd_strerror(pcm_rc), recovery_attempts + 1, max_recovery_attempts);
		}

		if (pcm_rc == -EPIPE) {
			recovery_attempts++;
			if (recovery_attempts > max_recovery_attempts) {
				if (trace_logging_enabled) {
					printf("[AUDIO_INPUT] jetkvm_audio_decode_write: Buffer underrun recovery failed after %d attempts\n", max_recovery_attempts);
				}
				return -2;
			}

			if (trace_logging_enabled) {
				printf("[AUDIO_INPUT] jetkvm_audio_decode_write: Buffer underrun detected, attempting recovery (attempt %d)\n", recovery_attempts);
			}
			err = snd_pcm_prepare(pcm_playback_handle);
			if (err < 0) {
				if (trace_logging_enabled) {
					printf("[AUDIO_INPUT] jetkvm_audio_decode_write: snd_pcm_prepare failed (%s), trying drop+prepare\n", snd_strerror(err));
				}
				snd_pcm_drop(pcm_playback_handle);
				err = snd_pcm_prepare(pcm_playback_handle);
				if (err < 0) {
					if (trace_logging_enabled) {
						printf("[AUDIO_INPUT] jetkvm_audio_decode_write: drop+prepare recovery failed (%s)\n", snd_strerror(err));
					}
					return -2;
				}
			}

			if (trace_logging_enabled) {
				printf("[AUDIO_INPUT] jetkvm_audio_decode_write: Buffer underrun recovery successful, retrying write\n");
			}
			goto retry_write;
		} else if (pcm_rc == -ESTRPIPE) {
			recovery_attempts++;
			if (recovery_attempts > max_recovery_attempts) {
				if (trace_logging_enabled) {
					printf("[AUDIO_INPUT] jetkvm_audio_decode_write: Device suspend recovery failed after %d attempts\n", max_recovery_attempts);
				}
				return -2;
			}

			if (trace_logging_enabled) {
				printf("[AUDIO_INPUT] jetkvm_audio_decode_write: Device suspended, attempting resume (attempt %d)\n", recovery_attempts);
			}
			int resume_attempts = 0;
			while ((err = snd_pcm_resume(pcm_playback_handle)) == -EAGAIN && resume_attempts < 10) {
				usleep(sleep_microseconds);
				resume_attempts++;
			}
			if (err < 0) {
				if (trace_logging_enabled) {
					printf("[AUDIO_INPUT] jetkvm_audio_decode_write: Device resume failed (%s), trying prepare fallback\n", snd_strerror(err));
				}
				err = snd_pcm_prepare(pcm_playback_handle);
				if (err < 0) {
					if (trace_logging_enabled) {
						printf("[AUDIO_INPUT] jetkvm_audio_decode_write: Prepare fallback failed (%s)\n", snd_strerror(err));
					}
					return -2;
				}
			}
			if (trace_logging_enabled) {
				printf("[AUDIO_INPUT] jetkvm_audio_decode_write: Device suspend recovery successful, skipping frame\n");
			}
			return 0;
		} else if (pcm_rc == -ENODEV) {
			if (trace_logging_enabled) {
				printf("[AUDIO_INPUT] jetkvm_audio_decode_write: Device disconnected (ENODEV) - critical error\n");
			}
			return -2;
		} else if (pcm_rc == -EIO) {
			recovery_attempts++;
			if (recovery_attempts <= max_recovery_attempts) {
				if (trace_logging_enabled) {
					printf("[AUDIO_INPUT] jetkvm_audio_decode_write: I/O error detected, attempting recovery\n");
				}
				snd_pcm_drop(pcm_playback_handle);
				err = snd_pcm_prepare(pcm_playback_handle);
				if (err >= 0) {
					if (trace_logging_enabled) {
						printf("[AUDIO_INPUT] jetkvm_audio_decode_write: I/O error recovery successful, retrying write\n");
					}
					goto retry_write;
				}
				if (trace_logging_enabled) {
					printf("[AUDIO_INPUT] jetkvm_audio_decode_write: I/O error recovery failed (%s)\n", snd_strerror(err));
				}
			}
			return -2;
		} else if (pcm_rc == -EAGAIN) {
			recovery_attempts++;
			if (recovery_attempts <= max_recovery_attempts) {
				if (trace_logging_enabled) {
					printf("[AUDIO_INPUT] jetkvm_audio_decode_write: Device not ready (EAGAIN), waiting and retrying\n");
				}
				snd_pcm_wait(pcm_playback_handle, sleep_microseconds / 4000);
				goto retry_write;
			}
			if (trace_logging_enabled) {
				printf("[AUDIO_INPUT] jetkvm_audio_decode_write: Device not ready recovery failed after %d attempts\n", max_recovery_attempts);
			}
			return -2;
		} else {
			recovery_attempts++;
			if (recovery_attempts <= 1 && (pcm_rc == -EINTR || pcm_rc == -EBUSY)) {
				if (trace_logging_enabled) {
					printf("[AUDIO_INPUT] jetkvm_audio_decode_write: Transient error %d (%s), retrying once\n", pcm_rc, snd_strerror(pcm_rc));
				}
				usleep(sleep_microseconds / 2);
				goto retry_write;
			}
			if (trace_logging_enabled) {
				printf("[AUDIO_INPUT] jetkvm_audio_decode_write: Unrecoverable error %d (%s)\n", pcm_rc, snd_strerror(pcm_rc));
			}
			return -2;
		}
	}

	if (trace_logging_enabled) {
		printf("[AUDIO_INPUT] jetkvm_audio_decode_write: Successfully wrote %d PCM frames to device\n", pcm_frames);
	}
	return pcm_frames;
}

// ============================================================================
// CLEANUP FUNCTIONS
// ============================================================================

/**
 * Close INPUT path (thread-safe with drain)
 */
void jetkvm_audio_playback_close() {
	while (playback_initializing) {
		usleep(sleep_microseconds);
	}

	if (__sync_bool_compare_and_swap(&playback_initialized, 1, 0) == 0) {
		return;
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

/**
 * Close OUTPUT path (thread-safe with drain)
 */
void jetkvm_audio_capture_close() {
	while (capture_initializing) {
		usleep(sleep_microseconds);
	}

	if (__sync_bool_compare_and_swap(&capture_initialized, 1, 0) == 0) {
		return;
	}

	if (encoder) {
		opus_encoder_destroy(encoder);
		encoder = NULL;
	}
	if (pcm_capture_handle) {
		snd_pcm_drain(pcm_capture_handle);
		snd_pcm_close(pcm_capture_handle);
		pcm_capture_handle = NULL;
	}
}
