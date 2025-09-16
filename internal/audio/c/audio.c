/*
 * JetKVM Audio Processing Module
 * 
 * This module handles bidirectional audio processing for JetKVM:
 * - Audio INPUT: Client microphone → Device speakers (decode Opus → ALSA playback)
 * - Audio OUTPUT: Device microphone → Client speakers (ALSA capture → encode Opus)
 */

#include <alsa/asoundlib.h>
#include <opus.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <errno.h>

// ARM NEON SIMD support for Cortex-A7
#ifdef __ARM_NEON
#include <arm_neon.h>
#define SIMD_ENABLED 1
#else
#define SIMD_ENABLED 0
#endif

// Performance optimization flags
static int trace_logging_enabled = 0;   // Enable detailed trace logging

// SIMD feature detection and optimization macros
#if SIMD_ENABLED
#define SIMD_ALIGN __attribute__((aligned(16)))
#define SIMD_PREFETCH(addr, rw, locality) __builtin_prefetch(addr, rw, locality)
#else
#define SIMD_ALIGN
#define SIMD_PREFETCH(addr, rw, locality)
#endif

// SIMD initialization and feature detection
static int simd_initialized = 0;

static void simd_init_once(void) {
    if (simd_initialized) return;
    simd_initialized = 1;
}

// ============================================================================
// GLOBAL STATE VARIABLES
// ============================================================================

// ALSA device handles
static snd_pcm_t *pcm_capture_handle = NULL;  // Device microphone (OUTPUT path)
static snd_pcm_t *pcm_playback_handle = NULL; // Device speakers (INPUT path)

// Opus codec instances
static OpusEncoder *encoder = NULL; // For OUTPUT path (device mic → client)
static OpusDecoder *decoder = NULL; // For INPUT path (client → device speakers)
// Audio format configuration
static int sample_rate = 48000;    // Sample rate in Hz
static int channels = 2;           // Number of audio channels (stereo)
static int frame_size = 960;       // Frames per Opus packet

// Opus encoder configuration
static int opus_bitrate = 96000;        // Bitrate in bits/second
static int opus_complexity = 3;         // Encoder complexity (0-10)
static int opus_vbr = 1;                // Variable bitrate enabled
static int opus_vbr_constraint = 1;     // Constrained VBR
static int opus_signal_type = 3;        // Audio signal type
static int opus_bandwidth = 1105;       // Bandwidth setting
static int opus_dtx = 0;                // Discontinuous transmission
static int opus_lsb_depth = 16;         // LSB depth for bit allocation

// Network and buffer configuration
static int max_packet_size = 1500;      // Maximum Opus packet size

// Error handling and retry configuration
static int sleep_microseconds = 1000;   // Base sleep time for retries
static int max_attempts_global = 5;     // Maximum retry attempts
static int max_backoff_us_global = 500000; // Maximum backoff time

// Performance optimization flags
static const int optimized_buffer_size = 1;   // Use optimized buffer sizing


// ============================================================================
// FUNCTION DECLARATIONS
// ============================================================================

// Audio OUTPUT path functions (device microphone → client speakers)
int jetkvm_audio_capture_init();           // Initialize capture device and Opus encoder
void jetkvm_audio_capture_close();         // Cleanup capture resources
int jetkvm_audio_read_encode(void *opus_buf); // Read PCM, encode to Opus

// Audio INPUT path functions (client microphone → device speakers)  
int jetkvm_audio_playback_init();          // Initialize playback device and Opus decoder
void jetkvm_audio_playback_close();        // Cleanup playback resources
int jetkvm_audio_decode_write(void *opus_buf, int opus_size); // Decode Opus, write PCM

// Configuration and utility functions
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
 * Update audio configuration constants from Go
 * Called during initialization to sync C variables with Go config
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
 * Enable or disable trace logging
 * When enabled, detailed debug information is printed to stdout
 * Zero overhead when disabled - no function calls or string formatting occur
 */
void set_trace_logging(int enabled) {
    trace_logging_enabled = enabled;
}

// ============================================================================
// SIMD-OPTIMIZED BUFFER OPERATIONS
// ============================================================================

#if SIMD_ENABLED
/**
 * SIMD-optimized buffer clearing for 16-bit audio samples
 * Uses ARM NEON to clear 8 samples (16 bytes) per iteration
 * 
 * @param buffer Pointer to 16-bit sample buffer (must be 16-byte aligned)
 * @param samples Number of samples to clear
 */
static inline void simd_clear_samples_s16(short *buffer, int samples) {
    simd_init_once();
    
    const int16x8_t zero = vdupq_n_s16(0);
    int simd_samples = samples & ~7; // Round down to multiple of 8
    
    // Process 8 samples at a time with NEON
    for (int i = 0; i < simd_samples; i += 8) {
        vst1q_s16(&buffer[i], zero);
    }
    
    // Handle remaining samples with scalar operations
    for (int i = simd_samples; i < samples; i++) {
        buffer[i] = 0;
    }
}

/**
 * SIMD-optimized stereo sample interleaving
 * Combines left and right channel data using NEON zip operations
 * 
 * @param left Left channel samples
 * @param right Right channel samples  
 * @param output Interleaved stereo output
 * @param frames Number of frames to process
 */
static inline void simd_interleave_stereo_s16(const short *left, const short *right, 
                                             short *output, int frames) {
    simd_init_once();
    
    int simd_frames = frames & ~7; // Process 8 frames at a time
    
    for (int i = 0; i < simd_frames; i += 8) {
        int16x8_t left_vec = vld1q_s16(&left[i]);
        int16x8_t right_vec = vld1q_s16(&right[i]);
        
        // Interleave using zip operations
        int16x8x2_t interleaved = vzipq_s16(left_vec, right_vec);
        
        // Store interleaved data
        vst1q_s16(&output[i * 2], interleaved.val[0]);
        vst1q_s16(&output[i * 2 + 8], interleaved.val[1]);
    }
    
    // Handle remaining frames
    for (int i = simd_frames; i < frames; i++) {
        output[i * 2] = left[i];
        output[i * 2 + 1] = right[i];
    }
}

/**
 * SIMD-optimized volume scaling for 16-bit samples
 * Applies volume scaling using NEON multiply operations
 * 
 * @param samples Input/output sample buffer
 * @param count Number of samples to scale
 * @param volume Volume factor (0.0 to 1.0, converted to fixed-point)
 */
static inline void simd_scale_volume_s16(short *samples, int count, float volume) {
    simd_init_once();
    
    // Convert volume to fixed-point (Q15 format)
    int16_t vol_fixed = (int16_t)(volume * 32767.0f);
    int16x8_t vol_vec = vdupq_n_s16(vol_fixed);
    
    int simd_count = count & ~7;
    
    for (int i = 0; i < simd_count; i += 8) {
        int16x8_t samples_vec = vld1q_s16(&samples[i]);
        
        // Multiply and shift right by 15 to maintain Q15 format
        int32x4_t low_result = vmull_s16(vget_low_s16(samples_vec), vget_low_s16(vol_vec));
        int32x4_t high_result = vmull_s16(vget_high_s16(samples_vec), vget_high_s16(vol_vec));
        
        // Shift right by 15 and narrow back to 16-bit
        int16x4_t low_narrow = vshrn_n_s32(low_result, 15);
        int16x4_t high_narrow = vshrn_n_s32(high_result, 15);
        
        int16x8_t result = vcombine_s16(low_narrow, high_narrow);
        vst1q_s16(&samples[i], result);
    }
    
    // Handle remaining samples
    for (int i = simd_count; i < count; i++) {
        samples[i] = (short)((samples[i] * vol_fixed) >> 15);
    }
}

/**
 * SIMD-optimized endianness conversion for 16-bit samples
 * Swaps byte order using NEON reverse operations
 */
static inline void simd_swap_endian_s16(short *samples, int count) {
    int simd_count = count & ~7;
    
    for (int i = 0; i < simd_count; i += 8) {
        uint16x8_t samples_vec = vld1q_u16((uint16_t*)&samples[i]);
        
        // Reverse bytes within each 16-bit element
        uint8x16_t samples_u8 = vreinterpretq_u8_u16(samples_vec);
        uint8x16_t swapped_u8 = vrev16q_u8(samples_u8);
        uint16x8_t swapped = vreinterpretq_u16_u8(swapped_u8);
        
        vst1q_u16((uint16_t*)&samples[i], swapped);
    }
    
    // Handle remaining samples
    for (int i = simd_count; i < count; i++) {
        samples[i] = __builtin_bswap16(samples[i]);
    }
}

/**
 * Convert 16-bit signed samples to 32-bit float samples using NEON
 */
static inline void simd_s16_to_float(const short *input, float *output, int count) {
    const float scale = 1.0f / 32768.0f;
    float32x4_t scale_vec = vdupq_n_f32(scale);
    
    // Process 4 samples at a time
    int simd_count = count & ~3;
    for (int i = 0; i < simd_count; i += 4) {
        int16x4_t s16_data = vld1_s16(input + i);
        int32x4_t s32_data = vmovl_s16(s16_data);
        float32x4_t float_data = vcvtq_f32_s32(s32_data);
        float32x4_t scaled = vmulq_f32(float_data, scale_vec);
        vst1q_f32(output + i, scaled);
    }
    
    // Handle remaining samples
    for (int i = simd_count; i < count; i++) {
        output[i] = (float)input[i] * scale;
    }
}

/**
 * Convert 32-bit float samples to 16-bit signed samples using NEON
 */
static inline void simd_float_to_s16(const float *input, short *output, int count) {
    const float scale = 32767.0f;
    float32x4_t scale_vec = vdupq_n_f32(scale);
    
    // Process 4 samples at a time
    int simd_count = count & ~3;
    for (int i = 0; i < simd_count; i += 4) {
        float32x4_t float_data = vld1q_f32(input + i);
        float32x4_t scaled = vmulq_f32(float_data, scale_vec);
        int32x4_t s32_data = vcvtq_s32_f32(scaled);
        int16x4_t s16_data = vqmovn_s32(s32_data);
        vst1_s16(output + i, s16_data);
    }
    
    // Handle remaining samples
    for (int i = simd_count; i < count; i++) {
        float scaled = input[i] * scale;
        output[i] = (short)__builtin_fmaxf(__builtin_fminf(scaled, 32767.0f), -32768.0f);
    }
}

/**
 * Convert mono to stereo by duplicating samples using NEON
 */
static inline void simd_mono_to_stereo_s16(const short *mono, short *stereo, int frames) {
	// Process 4 frames at a time
	int simd_frames = frames & ~3;
	for (int i = 0; i < simd_frames; i += 4) {
		int16x4_t mono_data = vld1_s16(mono + i);
		int16x4x2_t stereo_data = {mono_data, mono_data};
		vst2_s16(stereo + i * 2, stereo_data);
	}
	
	// Handle remaining frames
	for (int i = simd_frames; i < frames; i++) {
		stereo[i * 2] = mono[i];
		stereo[i * 2 + 1] = mono[i];
	}
}

/**
 * Convert stereo to mono by averaging channels using NEON
 */
static inline void simd_stereo_to_mono_s16(const short *stereo, short *mono, int frames) {
	// Process 4 frames at a time
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
	
	// Handle remaining frames
	for (int i = simd_frames; i < frames; i++) {
		mono[i] = (stereo[i * 2] + stereo[i * 2 + 1]) / 2;
	}
}

/**
 * Apply stereo balance adjustment using NEON
 */
static inline void simd_apply_stereo_balance_s16(short *stereo, int frames, float balance) {
	// Balance: -1.0 = full left, 0.0 = center, 1.0 = full right
	float left_gain = balance <= 0.0f ? 1.0f : 1.0f - balance;
	float right_gain = balance >= 0.0f ? 1.0f : 1.0f + balance;
	
	float32x4_t left_gain_vec = vdupq_n_f32(left_gain);
	float32x4_t right_gain_vec = vdupq_n_f32(right_gain);
	
	// Process 4 frames at a time
	int simd_frames = frames & ~3;
	for (int i = 0; i < simd_frames; i += 4) {
		int16x4x2_t stereo_data = vld2_s16(stereo + i * 2);
		
		// Convert to float for processing
		int32x4_t left_wide = vmovl_s16(stereo_data.val[0]);
		int32x4_t right_wide = vmovl_s16(stereo_data.val[1]);
		float32x4_t left_float = vcvtq_f32_s32(left_wide);
		float32x4_t right_float = vcvtq_f32_s32(right_wide);
		
		// Apply balance
		left_float = vmulq_f32(left_float, left_gain_vec);
		right_float = vmulq_f32(right_float, right_gain_vec);
		
		// Convert back to int16
		int32x4_t left_result = vcvtq_s32_f32(left_float);
		int32x4_t right_result = vcvtq_s32_f32(right_float);
		stereo_data.val[0] = vqmovn_s32(left_result);
		stereo_data.val[1] = vqmovn_s32(right_result);
		
		vst2_s16(stereo + i * 2, stereo_data);
	}
	
	// Handle remaining frames
	for (int i = simd_frames; i < frames; i++) {
		stereo[i * 2] = (short)(stereo[i * 2] * left_gain);
		stereo[i * 2 + 1] = (short)(stereo[i * 2 + 1] * right_gain);
	}
}

/**
 * Deinterleave stereo samples into separate left/right channels using NEON
 */
static inline void simd_deinterleave_stereo_s16(const short *interleaved, short *left, 
                                               short *right, int frames) {
	// Process 4 frames at a time
	int simd_frames = frames & ~3;
	for (int i = 0; i < simd_frames; i += 4) {
		int16x4x2_t stereo_data = vld2_s16(interleaved + i * 2);
		vst1_s16(left + i, stereo_data.val[0]);
		vst1_s16(right + i, stereo_data.val[1]);
	}
	
	// Handle remaining frames
	for (int i = simd_frames; i < frames; i++) {
		left[i] = interleaved[i * 2];
		right[i] = interleaved[i * 2 + 1];
	}
}

#else
// Fallback implementations for non-SIMD builds
static inline void simd_clear_samples_s16(short *buffer, int samples) {
    simd_init_once();
    
    memset(buffer, 0, samples * sizeof(short));
}

static inline void simd_interleave_stereo_s16(const short *left, const short *right, 
                                             short *output, int frames) {
    simd_init_once();
    
    for (int i = 0; i < frames; i++) {
        output[i * 2] = left[i];
        output[i * 2 + 1] = right[i];
    }
}

static inline void simd_scale_volume_s16(short *samples, int count, float volume) {
    simd_init_once();
    
    for (int i = 0; i < count; i++) {
        samples[i] = (short)(samples[i] * volume);
    }
}

static inline void simd_swap_endian_s16(short *samples, int count) {
	for (int i = 0; i < count; i++) {
		samples[i] = __builtin_bswap16(samples[i]);
	}
}

static inline void simd_s16_to_float(const short *input, float *output, int count) {
	const float scale = 1.0f / 32768.0f;
	for (int i = 0; i < count; i++) {
		output[i] = (float)input[i] * scale;
	}
}

static inline void simd_float_to_s16(const float *input, short *output, int count) {
	const float scale = 32767.0f;
	for (int i = 0; i < count; i++) {
		float scaled = input[i] * scale;
		output[i] = (short)__builtin_fmaxf(__builtin_fminf(scaled, 32767.0f), -32768.0f);
	}
}

static inline void simd_mono_to_stereo_s16(const short *mono, short *stereo, int frames) {
	for (int i = 0; i < frames; i++) {
		stereo[i * 2] = mono[i];
		stereo[i * 2 + 1] = mono[i];
	}
}

static inline void simd_stereo_to_mono_s16(const short *stereo, short *mono, int frames) {
	for (int i = 0; i < frames; i++) {
		mono[i] = (stereo[i * 2] + stereo[i * 2 + 1]) / 2;
	}
}

static inline void simd_apply_stereo_balance_s16(short *stereo, int frames, float balance) {
	float left_gain = balance <= 0.0f ? 1.0f : 1.0f - balance;
	float right_gain = balance >= 0.0f ? 1.0f : 1.0f + balance;
	
	for (int i = 0; i < frames; i++) {
		stereo[i * 2] = (short)(stereo[i * 2] * left_gain);
		stereo[i * 2 + 1] = (short)(stereo[i * 2 + 1] * right_gain);
	}
}

static inline void simd_deinterleave_stereo_s16(const short *interleaved, short *left, 
                                               short *right, int frames) {
	for (int i = 0; i < frames; i++) {
		left[i] = interleaved[i * 2];
		right[i] = interleaved[i * 2 + 1];
	}
}
#endif

// ============================================================================
// INITIALIZATION STATE TRACKING
// ============================================================================

// Thread-safe initialization state tracking to prevent race conditions
static volatile int capture_initializing = 0;  // OUTPUT path init in progress
static volatile int capture_initialized = 0;   // OUTPUT path ready
static volatile int playback_initializing = 0; // INPUT path init in progress  
static volatile int playback_initialized = 0;  // INPUT path ready

/**
 * Update Opus encoder parameters dynamically
 * Used for OUTPUT path (device microphone → client speakers)
 * 
 * @return 0 on success, -1 if encoder not initialized, >0 if some settings failed
 */
int update_opus_encoder_params(int bitrate, int complexity, int vbr, int vbr_constraint,
                              int signal_type, int bandwidth, int dtx) {
    if (!encoder || !capture_initialized) {
        return -1;
    }

    // Update local configuration
    opus_bitrate = bitrate;
    opus_complexity = complexity;
    opus_vbr = vbr;
    opus_vbr_constraint = vbr_constraint;
    opus_signal_type = signal_type;
    opus_bandwidth = bandwidth;
    opus_dtx = dtx;

    // Apply settings to Opus encoder
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
 * Safely open ALSA device with exponential backoff retry logic
 * Handles common device busy/unavailable scenarios with appropriate retry strategies
 * 
 * @param handle Pointer to PCM handle to be set
 * @param device ALSA device name (e.g., "hw:1,0")
 * @param stream Stream direction (capture or playback)
 * @return 0 on success, negative error code on failure
 */
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

/**
 * Configure ALSA device with optimized parameters
 * Sets up hardware and software parameters for optimal performance on constrained hardware
 * 
 * @param handle ALSA PCM handle
 * @param device_name Device name for debugging (not used in current implementation)
 * @return 0 on success, negative error code on failure
 */
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

	// Use RW access for compatibility
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

	// Optimize buffer sizes for constrained hardware, using smaller periods for lower latency on
	// constrained hardware
	snd_pcm_uframes_t period_size = optimized_buffer_size ? frame_size : frame_size / 2;
	if (period_size < 64) period_size = 64; // Minimum safe period size

	err = snd_pcm_hw_params_set_period_size_near(handle, params, &period_size, 0);
	if (err < 0) return err;

	// Optimize buffer size based on hardware constraints, using 2 periods for ultra-low latency on 
	// constrained hardware or 4 periods for good latency/stability balance
	snd_pcm_uframes_t buffer_size = optimized_buffer_size ? buffer_size = period_size * 2 : period_size * 4;
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

// ============================================================================
// AUDIO OUTPUT PATH FUNCTIONS (Device Microphone → Client Speakers)
// ============================================================================

/**
 * Initialize audio OUTPUT path: device microphone capture and Opus encoder
 * This enables sending device audio to the client
 * 
 * Thread-safe with atomic operations to prevent concurrent initialization
 * 
 * @return 0 on success, negative error codes on failure:
 *         -EBUSY: Already initializing
 *         -1: ALSA device open failed
 *         -2: ALSA device configuration failed  
 *         -3: Opus encoder creation failed
 */
int jetkvm_audio_capture_init() {
	int err;

	// Initialize SIMD capabilities early
	simd_init_once();

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
	if (pcm_capture_handle) {
		snd_pcm_close(pcm_capture_handle);
		pcm_capture_handle = NULL;
	}

	// Try to open ALSA capture device
	err = safe_alsa_open(&pcm_capture_handle, "hw:1,0", SND_PCM_STREAM_CAPTURE);
	if (err < 0) {
		capture_initializing = 0;
		return -1;
	}

	// Configure the device
	err = configure_alsa_device(pcm_capture_handle, "capture");
	if (err < 0) {
		snd_pcm_close(pcm_capture_handle);
		pcm_capture_handle = NULL;
		capture_initializing = 0;
		return -2;
	}

	// Initialize Opus encoder with optimized settings
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

	// Apply optimized Opus encoder settings for constrained hardware
	opus_encoder_ctl(encoder, OPUS_SET_BITRATE(opus_bitrate));
	opus_encoder_ctl(encoder, OPUS_SET_COMPLEXITY(opus_complexity));
	opus_encoder_ctl(encoder, OPUS_SET_VBR(opus_vbr));
	opus_encoder_ctl(encoder, OPUS_SET_VBR_CONSTRAINT(opus_vbr_constraint));
	opus_encoder_ctl(encoder, OPUS_SET_SIGNAL(opus_signal_type));
	opus_encoder_ctl(encoder, OPUS_SET_BANDWIDTH(opus_bandwidth)); // WIDEBAND for compatibility
	opus_encoder_ctl(encoder, OPUS_SET_DTX(opus_dtx));
	// Set LSB depth for improved bit allocation on constrained hardware
	opus_encoder_ctl(encoder, OPUS_SET_LSB_DEPTH(opus_lsb_depth));
	// Enable packet loss concealment for better resilience
	opus_encoder_ctl(encoder, OPUS_SET_PACKET_LOSS_PERC(5));
	// Set prediction disabled for lower latency
	opus_encoder_ctl(encoder, OPUS_SET_PREDICTION_DISABLED(1));

	capture_initialized = 1;
	capture_initializing = 0;
	return 0;
}

/**
 * Capture audio from device microphone and encode to Opus (OUTPUT path)
 * 
 * This function:
 * 1. Reads PCM audio from device microphone via ALSA
 * 2. Handles ALSA errors with robust recovery strategies
 * 3. Encodes PCM to Opus format for network transmission
 * 4. Provides zero-overhead trace logging when enabled
 * 
 * Error recovery includes handling:
 * - Buffer underruns (-EPIPE)
 * - Device suspension (-ESTRPIPE) 
 * - I/O errors (-EIO)
 * - Device busy conditions (-EBUSY, -EAGAIN)
 * 
 * @param opus_buf Buffer to store encoded Opus data (must be at least max_packet_size)
 * @return >0: Number of Opus bytes written
 *         0: No audio data available (not an error)
 *         -1: Initialization error or unrecoverable failure
 */
__attribute__((hot)) int jetkvm_audio_read_encode(void * __restrict__ opus_buf) {
	static short SIMD_ALIGN pcm_buffer[1920]; // max 2ch*960, aligned for SIMD
	unsigned char * __restrict__ out = (unsigned char*)opus_buf;
	
	// Prefetch output buffer and PCM buffer for better cache performance
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

	// Handle ALSA errors with robust recovery strategies
	if (__builtin_expect(pcm_rc < 0, 0)) {
		if (pcm_rc == -EPIPE) {
			// Buffer underrun - implement progressive recovery
			recovery_attempts++;
			if (recovery_attempts > max_recovery_attempts) {
				return -1; // Give up after max attempts
			}

			// Try to recover with prepare
			err = snd_pcm_prepare(pcm_capture_handle);
			if (err < 0) {
				// If prepare fails, try drop and prepare
				snd_pcm_drop(pcm_capture_handle);
				err = snd_pcm_prepare(pcm_capture_handle);
				if (err < 0) return -1;
			}
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
			while ((err = snd_pcm_resume(pcm_capture_handle)) == -EAGAIN && resume_attempts < 10) {
				usleep(sleep_microseconds);
				resume_attempts++;
			}
			if (err < 0) {
				// Resume failed, try prepare as fallback
				err = snd_pcm_prepare(pcm_capture_handle);
				if (err < 0) return -1;
			}
			return 0;
		} else if (pcm_rc == -ENODEV) {
			// Device disconnected - critical error
			return -1;
		} else if (pcm_rc == -EIO) {
			// I/O error - try recovery once
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
			// Other errors - limited retry for transient issues
			recovery_attempts++;
			if (recovery_attempts <= 1 && pcm_rc == -EINTR) {
				goto retry_read;
			} else if (recovery_attempts <= 1 && pcm_rc == -EBUSY) {
				// Device busy - simple sleep to allow other operations to complete
				usleep(sleep_microseconds / 2);
				goto retry_read;
			}
			return -1;
		}
	}

	// If we got fewer frames than expected, pad with silence using SIMD
	if (__builtin_expect(pcm_rc < frame_size, 0)) {
		int remaining_samples = (frame_size - pcm_rc) * channels;
		simd_clear_samples_s16(&pcm_buffer[pcm_rc * channels], remaining_samples);
	}

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
 * Initialize audio INPUT path: ALSA playback device and Opus decoder
 * This enables playing client audio through device speakers
 * 
 * Thread-safe with atomic operations to prevent concurrent initialization
 * 
 * @return 0 on success, negative error codes on failure:
 *         -EBUSY: Already initializing
 *         -1: ALSA device open failed or configuration failed
 *         -2: Opus decoder creation failed
 */
int jetkvm_audio_playback_init() {
	int err;

	// Initialize SIMD capabilities early
	simd_init_once();

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

/**
 * Decode Opus audio and play through device speakers (INPUT path)
 * 
 * This function:
 * 1. Validates input parameters and Opus packet size
 * 2. Decodes Opus data to PCM format
 * 3. Implements packet loss concealment for network issues
 * 4. Writes PCM to device speakers via ALSA
 * 5. Handles ALSA playback errors with recovery strategies
 * 6. Provides zero-overhead trace logging when enabled
 * 
 * Error recovery includes handling:
 * - Buffer underruns (-EPIPE) with progressive recovery
 * - Device suspension (-ESTRPIPE) with resume logic
 * - I/O errors (-EIO) with device reset
 * - Device not ready (-EAGAIN) with retry logic
 * 
 * @param opus_buf Buffer containing Opus-encoded audio data
 * @param opus_size Size of Opus data in bytes
 * @return >0: Number of PCM frames written to speakers
 *         0: Frame skipped (not an error)
 *         -1: Invalid input or decode failure
 *         -2: Unrecoverable ALSA error
 */
__attribute__((hot)) int jetkvm_audio_decode_write(void * __restrict__ opus_buf, int opus_size) {
	static short __attribute__((aligned(16))) pcm_buffer[1920]; // max 2ch*960, aligned for SIMD
	unsigned char * __restrict__ in = (unsigned char*)opus_buf;
	
	// Prefetch input buffer for better cache performance
	SIMD_PREFETCH(in, 0, 3);
	int err = 0;
	int recovery_attempts = 0;
	const int max_recovery_attempts = 3;

	// Safety checks
	if (__builtin_expect(!playback_initialized || !pcm_playback_handle || !decoder || !opus_buf || opus_size <= 0, 0)) {
		if (trace_logging_enabled) {
			printf("[AUDIO_INPUT] jetkvm_audio_decode_write: Failed safety checks - playback_initialized=%d, pcm_playback_handle=%p, decoder=%p, opus_buf=%p, opus_size=%d\n", 
				playback_initialized, pcm_playback_handle, decoder, opus_buf, opus_size);
		}
		return -1;
	}

	// Additional bounds checking
	if (opus_size > max_packet_size) {
		if (trace_logging_enabled) {
			printf("[AUDIO_INPUT] jetkvm_audio_decode_write: Opus packet too large - size=%d, max=%d\n", opus_size, max_packet_size);
		}
		return -1;
	}

	if (trace_logging_enabled) {
		printf("[AUDIO_INPUT] jetkvm_audio_decode_write: Processing Opus packet - size=%d bytes\n", opus_size);
	}

	// Decode Opus to PCM with error handling
	int pcm_frames = opus_decode(decoder, in, opus_size, pcm_buffer, frame_size, 0);
	if (__builtin_expect(pcm_frames < 0, 0)) {
		if (trace_logging_enabled) {
			printf("[AUDIO_INPUT] jetkvm_audio_decode_write: Opus decode failed with error %d, attempting packet loss concealment\n", pcm_frames);
		}
		// Try packet loss concealment on decode error
		pcm_frames = opus_decode(decoder, NULL, 0, pcm_buffer, frame_size, 0);
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
	// Write PCM to playback device with robust recovery
	int pcm_rc = snd_pcm_writei(pcm_playback_handle, pcm_buffer, pcm_frames);
	if (__builtin_expect(pcm_rc < 0, 0)) {
		if (trace_logging_enabled) {
			printf("[AUDIO_INPUT] jetkvm_audio_decode_write: ALSA write failed with error %d (%s), attempt %d/%d\n", 
				pcm_rc, snd_strerror(pcm_rc), recovery_attempts + 1, max_recovery_attempts);
		}
		
		if (pcm_rc == -EPIPE) {
			// Buffer underrun - implement progressive recovery
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
			// Try to recover with prepare
			err = snd_pcm_prepare(pcm_playback_handle);
			if (err < 0) {
				if (trace_logging_enabled) {
					printf("[AUDIO_INPUT] jetkvm_audio_decode_write: snd_pcm_prepare failed (%s), trying drop+prepare\n", snd_strerror(err));
				}
				// If prepare fails, try drop and prepare
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
			// Device suspended, implement robust resume logic
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
			// Try to resume with timeout
			int resume_attempts = 0;
			while ((err = snd_pcm_resume(pcm_playback_handle)) == -EAGAIN && resume_attempts < 10) {
				usleep(sleep_microseconds);
				resume_attempts++;
			}
			if (err < 0) {
				if (trace_logging_enabled) {
					printf("[AUDIO_INPUT] jetkvm_audio_decode_write: Device resume failed (%s), trying prepare fallback\n", snd_strerror(err));
				}
				// Resume failed, try prepare as fallback
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
			return 0; // Skip this frame but don't fail
		} else if (pcm_rc == -ENODEV) {
			// Device disconnected - critical error
			if (trace_logging_enabled) {
				printf("[AUDIO_INPUT] jetkvm_audio_decode_write: Device disconnected (ENODEV) - critical error\n");
			}
			return -2;
		} else if (pcm_rc == -EIO) {
			// I/O error - try recovery once
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
			// Device not ready - brief wait and retry
			recovery_attempts++;
			if (recovery_attempts <= max_recovery_attempts) {
				if (trace_logging_enabled) {
					printf("[AUDIO_INPUT] jetkvm_audio_decode_write: Device not ready (EAGAIN), waiting and retrying\n");
				}
				snd_pcm_wait(pcm_playback_handle, sleep_microseconds / 4000); // Convert to milliseconds
				goto retry_write;
			}
			if (trace_logging_enabled) {
				printf("[AUDIO_INPUT] jetkvm_audio_decode_write: Device not ready recovery failed after %d attempts\n", max_recovery_attempts);
			}
			return -2;
		} else {
			// Other errors - limited retry for transient issues
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
		printf("[AUDIO_INPUT] jetkvm_audio_decode_write: Successfully wrote %d PCM frames to USB Gadget audio device\n", pcm_frames);
	}
	return pcm_frames;
}

// ============================================================================
// CLEANUP FUNCTIONS
// ============================================================================

/**
 * Cleanup audio INPUT path resources (client microphone → device speakers)
 * 
 * Thread-safe cleanup with atomic operations to prevent double-cleanup
 * Properly drains ALSA buffers before closing to avoid audio artifacts
 */
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

/**
 * Cleanup audio OUTPUT path resources (device microphone → client speakers)
 * 
 * Thread-safe cleanup with atomic operations to prevent double-cleanup
 * Properly drains ALSA buffers before closing to avoid audio artifacts
 */
void jetkvm_audio_capture_close() {
	// Wait for any ongoing operations to complete
	while (capture_initializing) {
		usleep(sleep_microseconds);
	}

	// Atomic check and set to prevent double cleanup
	if (__sync_bool_compare_and_swap(&capture_initialized, 1, 0) == 0) {
		return; // Already cleaned up
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
