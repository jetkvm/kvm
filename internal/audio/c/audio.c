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
 * - Capture: S16_LE stereo, 20ms frames at hardware-negotiated rate
 * - Playback: S16_LE mono (USB gadget), 20ms frames at 48kHz
 * - Direct hardware access with userspace resampling (no ALSA plugin layer)
 */

#include <alsa/asoundlib.h>
#include <opus.h>
#include <speex/speex_resampler.h>
#include <stdio.h>
#include <stdlib.h>
#include <stdbool.h>
#include <string.h>
#include <unistd.h>
#include <errno.h>
#include <sched.h>
#include <time.h>
#include <signal.h>
#include <pthread.h>
#include <stdatomic.h>
#include <fcntl.h>
#include <sys/ioctl.h>
#include <linux/videodev2.h>

// ARM NEON SIMD optimizations (Cortex-A7 accelerates buffer operations, with scalar fallback)
#include <arm_neon.h>

// TC358743 V4L2 control IDs for audio
#ifndef V4L2_CID_USER_TC35874X_BASE
#define V4L2_CID_USER_TC35874X_BASE (V4L2_CID_USER_BASE + 0x10a0)
#endif
#define TC35874X_CID_AUDIO_SAMPLING_RATE (V4L2_CID_USER_TC35874X_BASE + 0)

// RV1106 (Cortex-A7) has 64-byte cache lines
#define CACHE_LINE_SIZE 64
#define SIMD_ALIGN __attribute__((aligned(16)))
#define CACHE_ALIGN __attribute__((aligned(CACHE_LINE_SIZE)))
#define SIMD_PREFETCH(addr, rw, locality) __builtin_prefetch(addr, rw, locality)

static snd_pcm_t *pcm_capture_handle = NULL;  // OUTPUT: TC358743 HDMI audio → client
static snd_pcm_t *pcm_playback_handle = NULL; // INPUT: Client microphone → device speakers

static const char *alsa_capture_device = NULL;
static const char *alsa_playback_device = NULL;
static bool capture_channels_swapped = false;
static bool capture_is_hdmi = false;

static OpusEncoder *encoder = NULL;
static OpusDecoder *decoder = NULL;
static SpeexResamplerState *capture_resampler = NULL;

// Audio format - RFC 7587 requires Opus RTP clock rate (not sample rate) to be 48kHz
// The Opus codec itself supports multiple sample rates (8/12/16/24/48 kHz), but the
// RTP timestamp clock must always increment at 48kHz for WebRTC compatibility
static const uint32_t opus_sample_rate = 48000;  // RFC 7587: Opus RTP timestamp clock rate (not codec sample rate)
static uint32_t hardware_sample_rate = 48000;    // Hardware-negotiated rate (can be 44.1k, 48k, 96k, etc.)
static uint8_t capture_channels = 2;   // OUTPUT: Audio source (HDMI or USB) → client (stereo by default)
static uint8_t playback_channels = 1;  // INPUT: Client mono mic → device (always mono for USB audio gadget)
static const uint16_t opus_frame_size = 960;  // 20ms frames at 48kHz (fixed)
static uint16_t hardware_frame_size = 960;     // 20ms frames at hardware rate

// Maximum hardware frame size: 192kHz @ 20ms = 3840 samples/channel
// This is the upper bound for hardware buffer allocation (highest sample rate we support)
#define MAX_HARDWARE_FRAME_SIZE 3840

// Audio initialization error codes
#define ERR_ALSA_OPEN_FAILED      -1
#define ERR_ALSA_CONFIG_FAILED    -2
#define ERR_RESAMPLER_INIT_FAILED -3
#define ERR_CODEC_INIT_FAILED     -4

static uint32_t opus_bitrate = 192000;
static uint8_t opus_complexity = 8;
static uint16_t max_packet_size = 1500;

// Opus encoder configuration constants (see opus_defines.h for full enum values)
#define OPUS_VBR 1                    // Variable bitrate mode enabled
#define OPUS_VBR_CONSTRAINT 1         // Constrained VBR maintains bitrate ceiling
#define OPUS_SIGNAL_TYPE 3002         // OPUS_SIGNAL_MUSIC (optimized for music/audio content)
#define OPUS_BANDWIDTH 1105           // OPUS_BANDWIDTH_FULLBAND (20kHz passband)
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

// Log level control (matches zerolog levels: 0=panic, 1=fatal, 2=error, 3=warn, 4=info, 5=debug, 6=trace)
// Default to warn (3) - only errors and warnings print by default
// Using volatile instead of atomic: single-core RV1106 has no cache coherency issues,
// and we have single writer (Go) with multiple readers (C threads). Avoids memory barrier overhead.
static volatile int audio_log_level = 3;  // WARN level

// Log level setters (called from Go)
void jetkvm_audio_set_log_level(int level);

void jetkvm_audio_set_log_level(int level) {
    // Clamp to valid range (0-6)
    if (level < 0) level = 0;
    if (level > 6) level = 6;
    audio_log_level = level;  // Simple store - no barrier needed on single-core
}

// Log level constants
#define LOG_LEVEL_ERROR 2
#define LOG_LEVEL_WARN  3
#define LOG_LEVEL_INFO  4
#define LOG_LEVEL_DEBUG 5
#define LOG_LEVEL_TRACE 6

// Optimized logging macros that short-circuit argument evaluation.
// On single-core RV1106, simple volatile read is sufficient (no atomic needed).
// INFO/DEBUG skip fflush() to avoid syscall overhead - rely on line buffering.
// ERROR/WARN always fflush() since they indicate problems that need immediate visibility.

#define LOG_ERROR(fmt, ...) do { \
    fprintf(stderr, "ERROR: " fmt "\n", ##__VA_ARGS__); \
    fflush(stderr); \
} while(0)

#define LOG_WARN(fmt, ...) do { \
    if (__builtin_expect(audio_log_level >= LOG_LEVEL_WARN, 1)) { \
        fprintf(stderr, "WARN: " fmt "\n", ##__VA_ARGS__); \
        fflush(stderr); \
    } \
} while(0)

#define LOG_INFO(fmt, ...) do { \
    if (__builtin_expect(audio_log_level >= LOG_LEVEL_INFO, 0)) { \
        fprintf(stdout, "INFO: " fmt "\n", ##__VA_ARGS__); \
    } \
} while(0)

#define LOG_DEBUG(fmt, ...) do { \
    if (__builtin_expect(audio_log_level >= LOG_LEVEL_DEBUG, 0)) { \
        fprintf(stdout, "DEBUG: " fmt "\n", ##__VA_ARGS__); \
    } \
} while(0)

// Legacy macro for gradual migration - will be removed
#define SHOULD_LOG(level) (__builtin_expect(audio_log_level >= (level), 0))

// Last captured PCM buffer for RDP audio output (copied after resampling)
// Format: 16-bit signed PCM, stereo interleaved, 48kHz, 20ms = 960 frames * 2 channels = 1920 samples
static short CACHE_ALIGN last_pcm_buffer[960 * 2];
static atomic_int last_pcm_samples = 0;  // Number of samples (not frames) in last_pcm_buffer

// Mutexes protect handle lifecycle and codec operations, NOT the ALSA I/O itself.
// The mutex is temporarily released during snd_pcm_readi/writei to prevent blocking.
// Race conditions are detected via handle pointer comparison after reacquiring the lock.
static pthread_mutex_t capture_mutex = PTHREAD_MUTEX_INITIALIZER;
static pthread_mutex_t playback_mutex = PTHREAD_MUTEX_INITIALIZER;

// Reference counters to track active ALSA I/O operations.
// Incremented before releasing mutex for snd_pcm_readi/writei, decremented after.
// close_audio_stream waits for these to reach 0 before closing handles.
static atomic_int capture_in_io = 0;
static atomic_int playback_in_io = 0;

int jetkvm_audio_capture_init();
void jetkvm_audio_capture_close();
int jetkvm_audio_read_encode(void *opus_buf);
int jetkvm_audio_get_last_pcm(void *pcm_buf, int max_size);

int jetkvm_audio_playback_init();
void jetkvm_audio_playback_close();
int jetkvm_audio_playback_drop();
int jetkvm_audio_decode_write(void *opus_buf, int opus_size);
int jetkvm_audio_write_pcm(void *pcm_buf, int num_bytes);

void update_audio_constants(uint32_t bitrate, uint8_t complexity,
                           uint8_t ch, uint16_t max_pkt,
                           uint32_t sleep_us, uint8_t max_attempts, uint32_t max_backoff,
                           uint8_t dtx_enabled, uint8_t fec_enabled, uint8_t buf_periods, uint8_t pkt_loss_perc);
void update_audio_decoder_constants(uint8_t ch, uint16_t max_pkt,
                                    uint32_t sleep_us, uint8_t max_attempts, uint32_t max_backoff,
                                    uint8_t buf_periods);


void update_audio_constants(uint32_t bitrate, uint8_t complexity,
                           uint8_t ch, uint16_t max_pkt,
                           uint32_t sleep_us, uint8_t max_attempts, uint32_t max_backoff,
                           uint8_t dtx_enabled, uint8_t fec_enabled, uint8_t buf_periods, uint8_t pkt_loss_perc) {
    opus_bitrate = (bitrate >= 64000 && bitrate <= 256000) ? bitrate : 192000;
    opus_complexity = (complexity <= 10) ? complexity : 5;
    capture_channels = (ch == 1 || ch == 2) ? ch : 2;
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

void update_audio_decoder_constants(uint8_t ch, uint16_t max_pkt,
                                    uint32_t sleep_us, uint8_t max_attempts, uint32_t max_backoff,
                                    uint8_t buf_periods) {
    playback_channels = (ch == 1 || ch == 2) ? ch : 2;
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
 *   hw:0,0 = TC358743 HDMI audio (direct hardware access, SpeexDSP resampling)
 *   hw:1,0 = USB Audio Gadget (direct hardware access, SpeexDSP resampling)
 */
static void init_alsa_devices_from_env(void) {
    alsa_capture_device = getenv("ALSA_CAPTURE_DEVICE");
    if (alsa_capture_device == NULL || alsa_capture_device[0] == '\0') {
        alsa_capture_device = "hw:1,0";
    }

    alsa_playback_device = getenv("ALSA_PLAYBACK_DEVICE");
    if (alsa_playback_device == NULL || alsa_playback_device[0] == '\0') {
        alsa_playback_device = "hw:1,0";
    }
}

// SIMD-OPTIMIZED BUFFER OPERATIONS (ARM NEON)

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
 * Query TC358743 HDMI receiver for detected audio sample rate
 * Reads the hardware-detected sample rate from V4L2 control
 * @return detected sample rate (44100, 48000, etc.) or 0 if detection fails
 */
static unsigned int get_hdmi_audio_sample_rate(void) {
	// TC358743 is a V4L2 subdevice at /dev/v4l-subdev2
	int fd = open("/dev/v4l-subdev2", O_RDWR);
	if (fd < 0) {
		// Distinguish between different failure modes for better diagnostics
		if (errno == ENOENT) {
			LOG_INFO("TC358743 device not found (USB audio mode or device not present)");
		} else if (errno == EACCES || errno == EPERM) {
			LOG_ERROR("Permission denied accessing TC358743 (/dev/v4l-subdev2)");
			LOG_ERROR("Check device permissions or run with appropriate privileges");
		} else {
			LOG_WARN("Could not open /dev/v4l-subdev2: %s (errno=%d)", strerror(errno), errno);
			LOG_WARN("HDMI audio sample rate detection unavailable, will use 48kHz default");
		}
		return 0;
	}

	// Use extended controls API for custom V4L2 controls
	struct v4l2_ext_control ext_ctrl = {0};
	ext_ctrl.id = TC35874X_CID_AUDIO_SAMPLING_RATE;

	struct v4l2_ext_controls ext_ctrls = {0};
	ext_ctrls.ctrl_class = V4L2_CTRL_CLASS_USER;
	ext_ctrls.count = 1;
	ext_ctrls.controls = &ext_ctrl;

	if (ioctl(fd, VIDIOC_G_EXT_CTRLS, &ext_ctrls) == -1) {
		// Provide specific error messages based on errno
		if (errno == EINVAL) {
			LOG_ERROR("TC358743 sample rate control not supported (driver version mismatch?)");
			LOG_ERROR("Ensure kernel driver supports audio_sampling_rate control");
		} else {
			LOG_WARN("TC358743 ioctl failed: %s (errno=%d)", strerror(errno), errno);
			LOG_WARN("Will use 48kHz default sample rate");
		}
		close(fd);
		return 0;
	}

	close(fd);

	unsigned int detected_rate = (unsigned int)ext_ctrl.value;
	static unsigned int last_logged_rate = 0; // Track last logged rate to suppress duplicate messages

	if (detected_rate == 0) {
		if (last_logged_rate != 0) {
			LOG_INFO("TC358743 reports 0 Hz (no HDMI signal or audio not detected yet)");
			LOG_INFO("Will use 48kHz default and resample if needed when signal detected");
			last_logged_rate = 0;
		}
		return 0;
	}

	// Validate detected rate is reasonable (log warning only on rate changes)
	if (detected_rate < 8000 || detected_rate > 192000) {
		if (detected_rate != last_logged_rate) {
			LOG_WARN("TC358743 reported unusual sample rate: %u Hz (expected 32k-192k)", detected_rate);
			LOG_WARN("Using detected rate anyway, but audio may not work correctly");
			last_logged_rate = detected_rate;
		}
	}

	// Log rate changes and update tracking state to suppress duplicate logging
	if (detected_rate != last_logged_rate) {
		LOG_INFO("TC358743 detected HDMI audio sample rate: %u Hz", detected_rate);
		last_logged_rate = detected_rate;
	}

	return detected_rate;
}

static inline void precise_sleep_us(uint32_t microseconds) {
	struct timespec ts = {
		.tv_sec = microseconds / 1000000,
		.tv_nsec = (microseconds % 1000000) * 1000
	};
	nanosleep(&ts, NULL);
}

static int safe_alsa_open(snd_pcm_t **handle, const char *device, snd_pcm_stream_t stream, int nonblock) {
	uint8_t attempt = 0;
	int err;
	uint32_t backoff_us = sleep_microseconds;

	while (attempt < max_attempts_global) {
		err = snd_pcm_open(handle, device, stream, SND_PCM_NONBLOCK);
		if (err >= 0) {
			if (!nonblock) {
				// Switch to blocking mode for capture path
				err = snd_pcm_nonblock(*handle, 0);
				if (err < 0) {
					LOG_ERROR("Failed to set blocking mode on %s: %s", device, snd_strerror(err));
					snd_pcm_close(*handle);
					*handle = NULL;
					return err;
				}
			}
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
 * Filter TC358743 I2S glitches (isolated spikes during silence from clock-stop)
 *
 * The TC358743 HDMI receiver generates spurious spikes when I2S clocks stop/start
 * during silent periods. These manifest as:
 *   - Full glitches: ±32767 (0x7FFF / 0x8000)
 *   - Half glitches: ±16383 (0x3FFF / 0xC000)
 *
 * Detection: Sample with |value| > 8000 AND both neighbors have |value| < 4000
 * Correction: Replace with linear interpolation of neighbors
 *
 * SIMD fast-path: Processes 16 samples/iteration, skips chunks without extreme
 * values. Zero overhead for clean audio (common case).
 */
static inline void filter_hdmi_glitches(int16_t * __restrict__ buf, uint32_t n) {
	const int16x8_t thresh_pos = vdupq_n_s16(8000);
	const int16x8_t thresh_neg = vdupq_n_s16(-8000);
	uint32_t i = 0;

	for (; i + 15 < n; i += 16) {
		int16x8_t v0 = vld1q_s16(&buf[i]);
		int16x8_t v1 = vld1q_s16(&buf[i + 8]);

		// Check if any sample exceeds threshold (SIMD parallel comparison)
		uint16x8_t ext0 = vorrq_u16(vcgtq_s16(v0, thresh_pos), vcltq_s16(v0, thresh_neg));
		uint16x8_t ext1 = vorrq_u16(vcgtq_s16(v1, thresh_pos), vcltq_s16(v1, thresh_neg));
		uint16x8_t combined = vorrq_u16(ext0, ext1);

		// Fast-path: skip chunk if no extreme values (common case)
		uint64x2_t ext64 = vreinterpretq_u64_u16(combined);
		if ((vgetq_lane_u64(ext64, 0) | vgetq_lane_u64(ext64, 1)) == 0)
			continue;

		// Slow path: fix glitches in this 16-sample chunk
		for (uint32_t j = 0; j < 16; j++) {
			int16_t s = buf[i + j];
			// Single unsigned range check: |s| <= 8000 iff (s + 8000) in [0, 16000)
			if ((uint16_t)(s + 8000) < 16000U)
				continue;

			uint32_t idx = i + j;
			int16_t prev = (idx > 0) ? buf[idx - 1] : 0;
			int16_t next = (idx + 1 < n) ? buf[idx + 1] : 0;

			// Branchless abs using bitwise AND for both conditions
			int16_t abs_prev = (prev >= 0) ? prev : -prev;
			int16_t abs_next = (next >= 0) ? next : -next;
			if ((abs_prev < 4000) & (abs_next < 4000))
				buf[idx] = (int16_t)((prev + next) >> 1);
		}
	}

	// Scalar tail: remaining samples
	for (; i < n; i++) {
		int16_t s = buf[i];
		if ((uint16_t)(s + 8000) < 16000U)
			continue;

		int16_t prev = (i > 0) ? buf[i - 1] : 0;
		int16_t next = (i + 1 < n) ? buf[i + 1] : 0;

		int16_t abs_prev = (prev >= 0) ? prev : -prev;
		int16_t abs_next = (next >= 0) ? next : -next;
		if ((abs_prev < 4000) & (abs_next < 4000))
			buf[i] = (int16_t)((prev + next) >> 1);
	}
}

/**
 * Swap stereo channels (L<->R) using ARM NEON SIMD
 * Processes 4 frames (8 samples) at a time for optimal performance
 * @param buffer Interleaved stereo buffer (L,R,L,R,...)
 * @param num_frames Number of stereo frames to swap
 */
static inline void swap_stereo_channels(int16_t *buffer, uint16_t num_frames) {
	uint16_t i;
	// Process in chunks of 4 frames (8 samples, 128 bits)
	for (i = 0; i + 3 < num_frames; i += 4) {
		int16x8_t vec = vld1q_s16(&buffer[i * 2]);
		int16x8_t swapped = vrev32q_s16(vec);
		vst1q_s16(&buffer[i * 2], swapped);
	}

	// Handle remaining frames with scalar code
	for (; i < num_frames; i++) {
		int16_t temp = buffer[i * 2];
		buffer[i * 2] = buffer[i * 2 + 1];
		buffer[i * 2 + 1] = temp;
	}
}

/**
 * Handle ALSA I/O errors with recovery attempts
 * @param handle Pointer to PCM handle to use for recovery operations
 * @param valid_handle Pointer to the valid handle to check against (for race detection)
 * @param stop_flag Pointer to atomic stop flag
 * @param pcm_rc Error code from ALSA I/O operation
 * @param recovery_attempts Pointer to uint8_t recovery attempt counter
 * @param sleep_ms Milliseconds to sleep during recovery
 * @param max_attempts Maximum recovery attempts allowed
 * @return Return codes:
 *   1  = Retry operation (error was recovered)
 *   0  = Skip this frame and continue
 *  -1  = Fatal error, abort operation
 *
 * IMPORTANT: This function NEVER unlocks the mutex. The caller is always
 * responsible for unlocking after checking the return value. This ensures
 * consistent mutex ownership semantics.
 */
static int handle_alsa_error(snd_pcm_t *handle, snd_pcm_t **valid_handle,
                             atomic_int *stop_flag,
                             int pcm_rc, uint8_t *recovery_attempts,
                             uint32_t sleep_ms, uint8_t max_attempts) {
	int err;

	if (pcm_rc == -EPIPE) {
		// Buffer underrun/overrun
		(*recovery_attempts)++;
		if (*recovery_attempts > max_attempts || handle != *valid_handle) {
			return -1;
		}
		err = snd_pcm_prepare(handle);
		if (err < 0) {
			if (handle != *valid_handle) {
				return -1;
			}
			snd_pcm_drop(handle);
			err = snd_pcm_prepare(handle);
			if (err < 0 || handle != *valid_handle) {
				return -1;
			}
		}
		return 1;  // Retry
	} else if (pcm_rc == -EAGAIN) {
		// Resource temporarily unavailable (normal for non-blocking playback).
		// When the USB host isn't consuming audio data, the ALSA buffer stays full
		// and every write returns -EAGAIN. This is NOT a device failure — the device
		// is healthy, just the host side isn't draining. Treat exhaustion as "skip
		// this frame" (return 0) rather than fatal (return -1) to avoid triggering
		// unnecessary device recovery cycles.
		(*recovery_attempts)++;
		if (handle != *valid_handle) {
			return -1;
		}
		if (*recovery_attempts > max_attempts) {
			return 0;  // Skip frame — buffer full, not a device error
		}
		// Wait up to 20ms for device to become ready (covers 2 AUDIN packet periods)
		snd_pcm_wait(handle, 20);
		return 1;  // Retry
	} else if (pcm_rc == -ESTRPIPE) {
		// Suspended, need to resume
		(*recovery_attempts)++;
		if (*recovery_attempts > max_attempts || handle != *valid_handle) {
			return -1;
		}
		uint8_t resume_attempts = 0;
		while ((err = snd_pcm_resume(handle)) == -EAGAIN && resume_attempts < 10) {
			if (*stop_flag || handle != *valid_handle) {
				return -1;
			}
			snd_pcm_wait(handle, sleep_ms);
			resume_attempts++;
		}
		if (err < 0) {
			if (handle != *valid_handle) {
				return -1;
			}
			err = snd_pcm_prepare(handle);
			if (err < 0 || handle != *valid_handle) {
				return -1;
			}
		}
		return 0;  // Skip frame after suspend recovery
	} else if (pcm_rc == -ENODEV) {
		// Device was removed
		return -1;
	} else if (pcm_rc == -EIO) {
		// I/O error
		(*recovery_attempts)++;
		if (*recovery_attempts <= max_attempts && handle == *valid_handle) {
			snd_pcm_drop(handle);
			if (handle != *valid_handle) {
				return -1;
			}
			err = snd_pcm_prepare(handle);
			if (err >= 0 && handle == *valid_handle) {
				return 1;  // Retry
			}
		}
		return -1;
	} else {
		// Other errors
		(*recovery_attempts)++;
		if (*recovery_attempts <= 1 && pcm_rc == -EINTR) {
			return 1;  // Retry on first interrupt
		} else if (*recovery_attempts <= 1 && pcm_rc == -EBUSY && handle == *valid_handle) {
			snd_pcm_wait(handle, 1);
			return 1;  // Retry on first busy
		}
		return -1;
	}
}

/**
 * Configure ALSA device (S16_LE @ hardware-negotiated rate with optimized buffering)
 * @param handle ALSA PCM handle
 * @param device_name Device name for logging
 * @param num_channels Number of channels (1=mono, 2=stereo)
 * @param preferred_rate Preferred sample rate (0 = use default 48kHz)
 * @param actual_rate_out Pointer to store the actual hardware-negotiated rate
 * @param actual_frame_size_out Pointer to store the actual frame size at hardware rate
 * @param channels_swapped_out Pointer to store whether channels are swapped (NULL to ignore)
 * @return 0 on success, negative error code on failure
 */
static int configure_alsa_device(snd_pcm_t *handle, const char *device_name, uint8_t num_channels,
                                 unsigned int preferred_rate, unsigned int *actual_rate_out, uint16_t *actual_frame_size_out,
                                 bool *channels_swapped_out) {
	snd_pcm_hw_params_t *params;
	snd_pcm_sw_params_t *sw_params;
	int err;

	snd_pcm_hw_params_alloca(&params);
	snd_pcm_sw_params_alloca(&sw_params);

	err = snd_pcm_hw_params_any(handle, params);
	if (err < 0) return err;

	err = snd_pcm_hw_params_set_access(handle, params, SND_PCM_ACCESS_RW_INTERLEAVED);
	if (err < 0) {
		LOG_ERROR("%s: Failed to set access mode: %s", device_name, snd_strerror(err));
		return err;
	}

	err = snd_pcm_hw_params_set_format(handle, params, SND_PCM_FORMAT_S16_LE);
	if (err < 0) {
		LOG_ERROR("%s: Failed to set format S16_LE: %s", device_name, snd_strerror(err));
		return err;
	}

	err = snd_pcm_hw_params_set_channels(handle, params, num_channels);
	if (err < 0) {
		LOG_ERROR("%s: Failed to set %u channels: %s", device_name, num_channels, snd_strerror(err));
		return err;
	}

	// Disable ALSA resampling - we handle it with SpeexDSP
	err = snd_pcm_hw_params_set_rate_resample(handle, params, 0);
	if (err < 0) {
		LOG_ERROR("%s: Failed to disable ALSA resampling: %s", device_name, snd_strerror(err));
		return err;
	}

	// Use preferred rate if specified, otherwise default to 48kHz
	unsigned int requested_rate = (preferred_rate > 0) ? preferred_rate : opus_sample_rate;
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

	unsigned int negotiated_rate = 0;
	err = snd_pcm_hw_params_get_rate(params, &negotiated_rate, 0);
	if (err < 0) return err;

	LOG_INFO("%s: Hardware negotiated %u Hz (Opus uses %u Hz with SpeexDSP resampling)",
	         device_name, negotiated_rate, opus_sample_rate);

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

	if (num_channels == 2 && channels_swapped_out) {
		snd_pcm_chmap_t *chmap = snd_pcm_get_chmap(handle);
		if (chmap != NULL) {
			if (chmap->channels != 2) {
				LOG_WARN("%s: Expected 2 channels but channel map has %u", device_name, chmap->channels);
			} else if (chmap->pos[0] == SND_CHMAP_UNKNOWN || chmap->pos[1] == SND_CHMAP_UNKNOWN) {
				LOG_WARN("%s: Channel map positions are unknown, cannot detect swap", device_name);
			} else {
				bool is_swapped = (chmap->pos[0] == SND_CHMAP_FR && chmap->pos[1] == SND_CHMAP_FL);
				if (is_swapped) {
					LOG_INFO("%s: Hardware reports swapped channel map (R,L instead of L,R)", device_name);
				}
				*channels_swapped_out = is_swapped;
			}
			free(chmap);
		} else {
			LOG_INFO("%s: Channel map not available, assuming standard L/R order", device_name);
		}
	}

	*actual_rate_out = negotiated_rate;
	*actual_frame_size_out = negotiated_rate / 50;  // 20ms frames

	return 0;
}

// AUDIO OUTPUT PATH FUNCTIONS (TC358743 HDMI Audio → Client Speakers)

/**
 * Initialize OUTPUT path (HDMI or USB Gadget audio capture → Opus encoder)
 * Opens ALSA capture device from ALSA_CAPTURE_DEVICE env (default: hw:1,0, set to hw:0,0 for HDMI)
 * and creates Opus encoder with optimized settings
 * @return 0 on success, -EBUSY if initializing, or:
 *         ERR_ALSA_OPEN_FAILED (-1), ERR_ALSA_CONFIG_FAILED (-2),
 *         ERR_RESAMPLER_INIT_FAILED (-3), ERR_CODEC_INIT_FAILED (-4)
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
		__sync_synchronize();

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

		atomic_store(&capture_stop_requested, 0);
	}

	err = safe_alsa_open(&pcm_capture_handle, alsa_capture_device, SND_PCM_STREAM_CAPTURE, 0);
	if (err < 0) {
		LOG_ERROR("Failed to open ALSA capture device %s: %s", alsa_capture_device, snd_strerror(err));
		atomic_store(&capture_stop_requested, 0);
		capture_initializing = 0;
		return ERR_ALSA_OPEN_FAILED;
	}

	capture_is_hdmi = (alsa_capture_device != NULL && strstr(alsa_capture_device, "hw:0") != NULL);

	unsigned int preferred_rate = 0;
	if (capture_is_hdmi) {
		preferred_rate = get_hdmi_audio_sample_rate();
	}

	unsigned int actual_rate = 0;
	uint16_t actual_frame_size = 0;
	bool channels_swapped = false;
	err = configure_alsa_device(pcm_capture_handle, "capture", capture_channels, preferred_rate, &actual_rate, &actual_frame_size, &channels_swapped);
	if (err < 0) {
		snd_pcm_t *handle = pcm_capture_handle;
		pcm_capture_handle = NULL;
		if (handle) {
			snd_pcm_close(handle);
		}
		atomic_store(&capture_stop_requested, 0);
		capture_initializing = 0;
		return ERR_ALSA_CONFIG_FAILED;
	}

	capture_channels_swapped = channels_swapped;
	hardware_sample_rate = actual_rate;
	hardware_frame_size = actual_frame_size;
	if (hardware_frame_size > MAX_HARDWARE_FRAME_SIZE) {
		LOG_ERROR("capture: Hardware frame size %u exceeds buffer capacity %u",
		          hardware_frame_size, MAX_HARDWARE_FRAME_SIZE);
		snd_pcm_t *handle = pcm_capture_handle;
		pcm_capture_handle = NULL;
		if (handle) {
			snd_pcm_close(handle);
		}
		atomic_store(&capture_stop_requested, 0);
		capture_initializing = 0;
		return ERR_CODEC_INIT_FAILED;
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
			LOG_ERROR("capture: Failed to create SpeexDSP resampler (%u Hz → %u Hz): %d",
			          hardware_sample_rate, opus_sample_rate, speex_err);
			snd_pcm_t *handle = pcm_capture_handle;
			pcm_capture_handle = NULL;
			if (handle) {
				snd_pcm_close(handle);
			}
			atomic_store(&capture_stop_requested, 0);
			capture_initializing = 0;
			return ERR_RESAMPLER_INIT_FAILED;
		}
	}

	LOG_INFO("capture: Initializing Opus encoder %sat (%u Hz → %u Hz), %u channels, frame size %u",
	         hardware_sample_rate == opus_sample_rate ? "" : "SpeexDSP resampled ",
	         hardware_sample_rate, opus_sample_rate, capture_channels, opus_frame_size);

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
			if (handle) {
				snd_pcm_close(handle);
			}
		}
		atomic_store(&capture_stop_requested, 0);
		capture_initializing = 0;
		return ERR_CODEC_INIT_FAILED;
	}

	// Critical settings that must succeed for WebRTC compliance
	#define OPUS_CTL_CRITICAL(call, desc) do { \
		int _err = call; \
		if (_err != OPUS_OK) { \
			LOG_ERROR("capture: Failed to set " desc ": %s", opus_strerror(_err)); \
			opus_encoder_destroy(encoder); \
			encoder = NULL; \
			if (capture_resampler) { \
				speex_resampler_destroy(capture_resampler); \
				capture_resampler = NULL; \
			} \
			snd_pcm_t *handle = pcm_capture_handle; \
			pcm_capture_handle = NULL; \
			if (handle) { \
				snd_pcm_close(handle); \
			} \
			atomic_store(&capture_stop_requested, 0); \
			capture_initializing = 0; \
			return ERR_CODEC_INIT_FAILED; \
		} \
	} while(0)

	// Non-critical settings that can fail without breaking functionality
	#define OPUS_CTL_WARN(call, desc) do { \
		int _err = call; \
		if (_err != OPUS_OK) { \
			LOG_WARN("capture: Failed to set " desc ": %s (non-critical, continuing)", opus_strerror(_err)); \
		} \
	} while(0)

	// Critical: Bitrate, VBR mode, FEC are required for proper WebRTC operation
	OPUS_CTL_CRITICAL(opus_encoder_ctl(encoder, OPUS_SET_BITRATE(opus_bitrate)), "bitrate");
	OPUS_CTL_CRITICAL(opus_encoder_ctl(encoder, OPUS_SET_VBR(OPUS_VBR)), "VBR mode");
	OPUS_CTL_CRITICAL(opus_encoder_ctl(encoder, OPUS_SET_VBR_CONSTRAINT(OPUS_VBR_CONSTRAINT)), "VBR constraint");
	OPUS_CTL_CRITICAL(opus_encoder_ctl(encoder, OPUS_SET_INBAND_FEC(opus_fec_enabled)), "FEC");

	// Non-critical: These optimize quality/performance but aren't required
	OPUS_CTL_WARN(opus_encoder_ctl(encoder, OPUS_SET_COMPLEXITY(opus_complexity)), "complexity");
	OPUS_CTL_WARN(opus_encoder_ctl(encoder, OPUS_SET_SIGNAL(OPUS_SIGNAL_TYPE)), "signal type");
	OPUS_CTL_WARN(opus_encoder_ctl(encoder, OPUS_SET_BANDWIDTH(OPUS_BANDWIDTH)), "bandwidth");
	OPUS_CTL_WARN(opus_encoder_ctl(encoder, OPUS_SET_DTX(opus_dtx_enabled)), "DTX");
	OPUS_CTL_WARN(opus_encoder_ctl(encoder, OPUS_SET_LSB_DEPTH(OPUS_LSB_DEPTH)), "LSB depth");
	OPUS_CTL_WARN(opus_encoder_ctl(encoder, OPUS_SET_PACKET_LOSS_PERC(opus_packet_loss_perc)), "packet loss percentage");

	#undef OPUS_CTL_CRITICAL
	#undef OPUS_CTL_WARN

	capture_initialized = 1;
	atomic_store(&capture_stop_requested, 0);
	capture_initializing = 0;
	return 0;
}

__attribute__((hot)) int jetkvm_audio_read_encode(void * __restrict__ opus_buf) {
	// Two buffers: hardware buffer + resampled buffer (at 48kHz)
	static short CACHE_ALIGN pcm_hw_buffer[MAX_HARDWARE_FRAME_SIZE * 2];    // Max hardware rate * stereo
	static short CACHE_ALIGN pcm_opus_buffer[960 * 2];   // 48kHz @ 20ms * 2 channels
	static uint16_t sample_rate_check_counter = 0;
	// Hysteresis for sample rate change detection - require 3 consecutive detections
	// of the same new rate before triggering reconnection (filters transient glitches)
	static unsigned int pending_new_rate = 0;
	static uint8_t rate_change_confirm_count = 0;
	#define RATE_CHANGE_CONFIRM_THRESHOLD 3
	// Cooldown after rate-change reconnection to prevent oscillation.
	// Some HDMI sources (e.g., macOS) rapidly switch audio sample rates between
	// applications (48kHz, 88.2kHz, 44.1kHz, etc.), causing a reconnection loop.
	// After triggering a reconnection, skip rate checks for this many cycles.
	static uint8_t rate_check_cooldown_cycles = 0;
	#define RATE_CHECK_COOLDOWN_AFTER_RECONNECT 60  // ~60 seconds (1 cycle = ~1s at 50-frame intervals)
	unsigned char * __restrict__ out = (unsigned char*)opus_buf;
	int32_t pcm_rc, nb_bytes;
	uint8_t recovery_attempts = 0;
	const uint8_t max_recovery_attempts = 3;

	if (__builtin_expect(atomic_load(&capture_stop_requested), 0)) {
		return -1;
	}

	SIMD_PREFETCH(out, 1, 0);
	SIMD_PREFETCH(pcm_hw_buffer, 0, 0);
	SIMD_PREFETCH(pcm_hw_buffer + 64, 0, 1);

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

	// Increment I/O counter before releasing mutex - prevents close from proceeding
	atomic_fetch_add(&capture_in_io, 1);
	pthread_mutex_unlock(&capture_mutex);
	pcm_rc = snd_pcm_readi(handle, pcm_hw_buffer, hardware_frame_size);
	pthread_mutex_lock(&capture_mutex);
	atomic_fetch_sub(&capture_in_io, 1);

	if (handle != pcm_capture_handle || atomic_load(&capture_stop_requested)) {
		pthread_mutex_unlock(&capture_mutex);
		return -1;
	}

	if (__builtin_expect(pcm_rc < 0, 0)) {
		int err_result = handle_alsa_error(handle, &pcm_capture_handle, &capture_stop_requested,
		                                    pcm_rc, &recovery_attempts,
		                                    sleep_milliseconds, max_recovery_attempts);
		if (err_result == 1) {
			// Recovery successful, retry (mutex still held)
			goto retry_read;
		} else {
			// Fatal error or skip frame (err_result == -1 or 0)
			pthread_mutex_unlock(&capture_mutex);
			return (err_result == 0) ? 0 : -1;
		}
	}

	if (capture_is_hdmi && __builtin_expect(++sample_rate_check_counter >= 50, 0)) {
		sample_rate_check_counter = 0;

		// After a rate-change reconnection, skip rate checks during cooldown
		// to let the audio pipeline stabilize and prevent oscillation loops
		if (rate_check_cooldown_cycles > 0) {
			rate_check_cooldown_cycles--;
		} else {
			unsigned int current_rate = get_hdmi_audio_sample_rate();
			if (current_rate != 0 && current_rate != hardware_sample_rate) {
				// Hysteresis: require multiple consecutive detections of the same new rate
				// to filter transient glitches from the TC358743 HDMI chip
				if (current_rate == pending_new_rate) {
					rate_change_confirm_count++;
					if (rate_change_confirm_count >= RATE_CHANGE_CONFIRM_THRESHOLD) {
						LOG_INFO("capture: HDMI sample rate changed from %u to %u Hz, reconfiguring",
						         hardware_sample_rate, current_rate);
						// Reset hysteresis and set cooldown to prevent oscillation
						pending_new_rate = 0;
						rate_change_confirm_count = 0;
						sample_rate_check_counter = 0;
						rate_check_cooldown_cycles = RATE_CHECK_COOLDOWN_AFTER_RECONNECT;
						pthread_mutex_unlock(&capture_mutex);
						return -2;  // -2 = sample rate changed (distinct from -1 = error)
					}
				} else {
					// Different rate detected, start new confirmation cycle
					pending_new_rate = current_rate;
					rate_change_confirm_count = 1;
				}
			} else {
				// Rate is stable or detection failed, reset hysteresis state
				if (rate_change_confirm_count > 0) {
					pending_new_rate = 0;
					rate_change_confirm_count = 0;
				}
			}
		}
	}

	// Zero-fill any missing samples if we got a short read
	// Guard: pcm_rc must be non-negative and less than frame size
	if (__builtin_expect(pcm_rc >= 0 && pcm_rc < hardware_frame_size, 0)) {
		uint32_t remaining_samples = (hardware_frame_size - pcm_rc) * capture_channels;
		simd_clear_samples_s16(&pcm_hw_buffer[pcm_rc * capture_channels], remaining_samples);
	}

	if (capture_channels_swapped) {
		swap_stereo_channels(pcm_hw_buffer, hardware_frame_size);
	}

	// Filter TC358743 I2S clock-stop glitches (only for HDMI capture)
	// Zero overhead for clean audio: SIMD fast-path skips chunks without max values
	if (capture_is_hdmi) {
		filter_hdmi_glitches(pcm_hw_buffer, hardware_frame_size * capture_channels);
	}

	short *pcm_to_encode;
	if (capture_resampler) {
		spx_uint32_t in_len = hardware_frame_size;
		spx_uint32_t out_len = opus_frame_size;
		int res_err = speex_resampler_process_interleaved_int(capture_resampler,
		                                                        pcm_hw_buffer, &in_len,
		                                                        pcm_opus_buffer, &out_len);
		if (res_err != 0 || out_len != opus_frame_size) {
			LOG_ERROR("capture: Resampling failed (err=%d, out_len=%u, expected=%u)",
			          res_err, out_len, opus_frame_size);
			pthread_mutex_unlock(&capture_mutex);
			return -1;
		}
		pcm_to_encode = pcm_opus_buffer;
	} else {
		pcm_to_encode = pcm_hw_buffer;
	}

	// Copy PCM to global buffer for RDP audio output (non-blocking, atomic size update)
	// This is always stereo 48kHz - opus_frame_size * capture_channels samples
	uint32_t pcm_samples = opus_frame_size * capture_channels;
	memcpy(last_pcm_buffer, pcm_to_encode, pcm_samples * sizeof(short));
	atomic_store(&last_pcm_samples, pcm_samples);

	OpusEncoder *enc = encoder;
	if (!enc) {
		pthread_mutex_unlock(&capture_mutex);
		return -1;
	}

	nb_bytes = opus_encode(enc, pcm_to_encode, opus_frame_size, out, max_packet_size);

	if (__builtin_expect(nb_bytes < 0, 0)) {
		LOG_ERROR("capture: Opus encoding failed: %s", opus_strerror(nb_bytes));
	}

	pthread_mutex_unlock(&capture_mutex);
	return nb_bytes;
}

/**
 * Get the last captured PCM audio data (for RDP audio output)
 * This function retrieves the raw PCM data that was captured and resampled
 * in the last call to jetkvm_audio_read_encode().
 * Format: 16-bit signed PCM, stereo interleaved, 48kHz, 20ms frames
 *
 * @param pcm_buf Output buffer for PCM data (must be at least max_size bytes)
 * @param max_size Maximum number of bytes to copy
 * @return Number of bytes copied, or 0 if no data available, or -1 on error
 */
int jetkvm_audio_get_last_pcm(void * __restrict__ pcm_buf, int max_size) {
	if (!pcm_buf || max_size <= 0) {
		return -1;
	}

	int samples = atomic_load(&last_pcm_samples);
	if (samples <= 0) {
		return 0;
	}

	int bytes_needed = samples * sizeof(short);
	if (bytes_needed > max_size) {
		bytes_needed = max_size;
		samples = bytes_needed / sizeof(short);
	}

	memcpy(pcm_buf, last_pcm_buffer, bytes_needed);
	return bytes_needed;
}

// AUDIO INPUT PATH FUNCTIONS (Client Microphone → Device Speakers)

/**
 * Initialize INPUT path (Opus decoder → device speakers)
 * Opens ALSA playback device from ALSA_PLAYBACK_DEVICE env (default: hw:1,0)
 * and creates Opus decoder. Returns immediately on device open failure (no fallback).
 * @return 0 on success, -EBUSY if initializing, or:
 *         ERR_ALSA_OPEN_FAILED (-1), ERR_ALSA_CONFIG_FAILED (-2), ERR_CODEC_INIT_FAILED (-4)
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
			snd_pcm_nonblock(pcm_playback_handle, 1);
			snd_pcm_drop(pcm_playback_handle);
		}

		// Wait briefly for any in-flight I/O to return after the drop
		int pb_wait = 0;
		while (atomic_load(&playback_in_io) > 0 && pb_wait < 50) {
			struct timespec w = { .tv_sec = 0, .tv_nsec = 1000000 };
			nanosleep(&w, NULL);
			pb_wait++;
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

		atomic_store(&playback_stop_requested, 0);
	}

	err = safe_alsa_open(&pcm_playback_handle, alsa_playback_device, SND_PCM_STREAM_PLAYBACK, 1);
	if (err < 0) {
		LOG_ERROR("Failed to open ALSA playback device %s: %s", alsa_playback_device, snd_strerror(err));
		atomic_store(&playback_stop_requested, 0);
		playback_initializing = 0;
		return ERR_ALSA_OPEN_FAILED;
	}

	unsigned int actual_rate = 0;
	uint16_t actual_frame_size = 0;
	err = configure_alsa_device(pcm_playback_handle, "playback", playback_channels, 0, &actual_rate, &actual_frame_size, NULL);
	if (err < 0) {
		snd_pcm_t *handle = pcm_playback_handle;
		pcm_playback_handle = NULL;
		if (handle) {
			snd_pcm_close(handle);
		}
		atomic_store(&playback_stop_requested, 0);
		playback_initializing = 0;
		return ERR_ALSA_CONFIG_FAILED;
	}

	LOG_INFO("playback: Initializing Opus decoder at %u Hz, %u channels, frame size %u",
	         actual_rate, playback_channels, actual_frame_size);

	int opus_err = 0;
	decoder = opus_decoder_create(actual_rate, playback_channels, &opus_err);
	if (!decoder || opus_err != OPUS_OK) {
		snd_pcm_t *handle = pcm_playback_handle;
		pcm_playback_handle = NULL;
		if (handle) {
			snd_pcm_close(handle);
		}
		atomic_store(&playback_stop_requested, 0);
		playback_initializing = 0;
		return ERR_CODEC_INIT_FAILED;
	}

	playback_initialized = 1;
	atomic_store(&playback_stop_requested, 0);
	playback_initializing = 0;
	return 0;
}

__attribute__((hot)) int jetkvm_audio_decode_write(void * __restrict__ opus_buf, int32_t opus_size) {
	static short CACHE_ALIGN pcm_buffer[960 * 2];  // Cache-aligned
	unsigned char * __restrict__ in = (unsigned char*)opus_buf;
	int32_t pcm_frames, pcm_rc;
	uint8_t recovery_attempts = 0;
	// Higher limit for non-blocking mode: EAGAIN retries with 20ms waits (200ms max)
	const uint8_t max_recovery_attempts = 10;

	// Validate inputs before acquiring mutex to reduce lock contention
	if (__builtin_expect(!opus_buf || opus_size <= 0 || opus_size > max_packet_size, 0)) {
		return -1;
	}

	if (__builtin_expect(atomic_load(&playback_stop_requested), 0)) {
		return -1;
	}

	SIMD_PREFETCH(in, 0, 0);

	pthread_mutex_lock(&playback_mutex);

	if (__builtin_expect(!playback_initialized || !pcm_playback_handle || !decoder, 0)) {
		pthread_mutex_unlock(&playback_mutex);
		return -1;
	}

	OpusDecoder *dec = decoder;
	if (!dec) {
		pthread_mutex_unlock(&playback_mutex);
		return -1;
	}

	pcm_frames = opus_decode(dec, in, opus_size, pcm_buffer, opus_frame_size, 0);

	if (__builtin_expect(pcm_frames < 0, 0)) {
		// Initial decode failed, try Forward Error Correction from previous packets
		LOG_WARN("playback: Opus decode failed (%d), attempting FEC recovery", pcm_frames);

		pcm_frames = opus_decode(dec, NULL, 0, pcm_buffer, opus_frame_size, 1);

		if (pcm_frames < 0) {
			LOG_ERROR("playback: FEC recovery also failed (%d), dropping frame", pcm_frames);
			pthread_mutex_unlock(&playback_mutex);
			return -1;
		}

		if (pcm_frames > 0) {
			LOG_INFO("playback: FEC recovered %d frames", pcm_frames);
		} else {
			pthread_mutex_unlock(&playback_mutex);
			return 0;  // FEC returned no frames, nothing to write
		}
	}

	if (__builtin_expect(pcm_frames <= 0, 0)) {
		pthread_mutex_unlock(&playback_mutex);
		return 0;  // Nothing to write
	}

retry_write:
	if (__builtin_expect(atomic_load(&playback_stop_requested), 0)) {
		pthread_mutex_unlock(&playback_mutex);
		return -1;
	}

	snd_pcm_t *handle = pcm_playback_handle;

	// Increment I/O counter before releasing mutex - prevents close from proceeding
	atomic_fetch_add(&playback_in_io, 1);
	pthread_mutex_unlock(&playback_mutex);
	pcm_rc = snd_pcm_writei(handle, pcm_buffer, pcm_frames);
	pthread_mutex_lock(&playback_mutex);
	atomic_fetch_sub(&playback_in_io, 1);

	if (handle != pcm_playback_handle || atomic_load(&playback_stop_requested)) {
		pthread_mutex_unlock(&playback_mutex);
		return -1;
	}
	if (__builtin_expect(pcm_rc < 0, 0)) {
		int err_result = handle_alsa_error(handle, &pcm_playback_handle, &playback_stop_requested,
		                                    pcm_rc, &recovery_attempts,
		                                    sleep_milliseconds, max_recovery_attempts);
		if (err_result == 1) {
			// Recovery successful, retry (mutex still held)
			goto retry_write;
		} else {
			// Fatal error or skip frame (err_result == -1 or 0)
			pthread_mutex_unlock(&playback_mutex);
			return (err_result == 0) ? 0 : -2;
		}
	}
	pthread_mutex_unlock(&playback_mutex);
	return pcm_frames;
}

/**
 * Write raw PCM audio data to playback device (for RDP audio input)
 * This function writes raw PCM directly without Opus decoding.
 * Format: 16-bit signed PCM, mono or stereo (matches playback_channels), 48kHz
 *
 * @param pcm_buf Input buffer containing PCM samples
 * @param num_bytes Number of bytes in the buffer
 * @return Number of frames written, 0 if skipped, or negative on error
 */
__attribute__((hot)) int jetkvm_audio_write_pcm(void * __restrict__ pcm_buf, int num_bytes) {
	int32_t pcm_rc;
	uint8_t recovery_attempts = 0;
	// Higher limit for non-blocking mode: EAGAIN retries with 20ms waits (200ms max)
	const uint8_t max_recovery_attempts = 10;

	// Validate inputs before acquiring mutex
	if (__builtin_expect(!pcm_buf || num_bytes <= 0, 0)) {
		return -1;
	}

	if (__builtin_expect(atomic_load(&playback_stop_requested), 0)) {
		return -1;
	}

	pthread_mutex_lock(&playback_mutex);

	if (__builtin_expect(!playback_initialized || !pcm_playback_handle, 0)) {
		pthread_mutex_unlock(&playback_mutex);
		return -1;
	}

	// Calculate number of frames from bytes
	// Frame = channels * bytes_per_sample = playback_channels * 2
	int bytes_per_frame = playback_channels * 2;
	int32_t pcm_frames = num_bytes / bytes_per_frame;

	if (pcm_frames <= 0) {
		pthread_mutex_unlock(&playback_mutex);
		return 0;
	}

retry_write_pcm:
	if (__builtin_expect(atomic_load(&playback_stop_requested), 0)) {
		pthread_mutex_unlock(&playback_mutex);
		return -1;
	}

	snd_pcm_t *handle = pcm_playback_handle;

	// Increment I/O counter before releasing mutex - prevents close from proceeding
	atomic_fetch_add(&playback_in_io, 1);
	pthread_mutex_unlock(&playback_mutex);
	pcm_rc = snd_pcm_writei(handle, pcm_buf, pcm_frames);
	pthread_mutex_lock(&playback_mutex);
	atomic_fetch_sub(&playback_in_io, 1);

	if (handle != pcm_playback_handle || atomic_load(&playback_stop_requested)) {
		pthread_mutex_unlock(&playback_mutex);
		return -1;
	}

	if (__builtin_expect(pcm_rc < 0, 0)) {
		int err_result = handle_alsa_error(handle, &pcm_playback_handle, &playback_stop_requested,
		                                    pcm_rc, &recovery_attempts,
		                                    sleep_milliseconds, max_recovery_attempts);
		if (err_result == 1) {
			goto retry_write_pcm;
		} else {
			pthread_mutex_unlock(&playback_mutex);
			return (err_result == 0) ? 0 : -2;
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
                                codec_destroy_fn destroy_codec, atomic_int *in_io_counter) {
	atomic_store(stop_requested, 1);

	while (*initializing) {
		sched_yield();
	}

	if (__sync_bool_compare_and_swap(initialized, 1, 0) == 0) {
		atomic_store(stop_requested, 0);
		return;
	}

	// Abort any in-flight ALSA I/O BEFORE waiting for the counter.
	// snd_pcm_drop() stops the PCM stream, causing blocked/pending writes to fail.
	// snd_pcm_nonblock() ensures non-blocking mode so no new waits can start.
	// Without this, a stuck snd_pcm_writei in the kernel (e.g., dead USB device)
	// would block forever, and the I/O counter would never reach 0.
	if (*pcm_handle) {
		snd_pcm_nonblock(*pcm_handle, 1);
		snd_pcm_drop(*pcm_handle);
	}

	// Wait for any active ALSA I/O operations to complete
	int wait_count = 0;
	while (atomic_load(in_io_counter) > 0 && wait_count < 100) {
		struct timespec io_wait = { .tv_sec = 0, .tv_nsec = 1000000 }; // 1ms
		nanosleep(&io_wait, NULL);
		wait_count++;
	}
	if (wait_count >= 100) {
		LOG_WARN("audio: close timed out waiting for I/O to complete (counter=%d)", atomic_load(in_io_counter));
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
		// Validate codec pointer before destroy - must be in heap range
		// This prevents crashes from double-free or corrupted pointers
		uintptr_t ptr = (uintptr_t)codec_to_destroy;
		if (ptr > 0x1000 && ptr < 0xFFFFFFFF) {
			destroy_codec(codec_to_destroy);
		} else {
			LOG_WARN("audio: skipping destroy of invalid codec pointer %p", codec_to_destroy);
		}
	}

	atomic_store(stop_requested, 0);
}

void jetkvm_audio_playback_close() {
	close_audio_stream(&playback_stop_requested, &playback_initializing,
	                   &playback_initialized, &playback_mutex,
	                   &pcm_playback_handle, (void**)&decoder,
	                   (codec_destroy_fn)opus_decoder_destroy, &playback_in_io);
}

/**
 * Drop any pending audio frames in the playback buffer.
 * This clears stale audio data that may have accumulated while
 * the host wasn't consuming from the USB audio gadget.
 *
 * Call this when audio input is first enabled to prevent
 * accumulated audio from playing back when recording starts.
 *
 * @return 0 on success, negative error code on failure
 */
int jetkvm_audio_playback_drop() {
	if (!playback_initialized || !pcm_playback_handle) {
		return 0;  // Not initialized, nothing to drop
	}

	pthread_mutex_lock(&playback_mutex);

	if (!pcm_playback_handle) {
		pthread_mutex_unlock(&playback_mutex);
		return 0;
	}

	// Drop all pending frames and stop the PCM
	int rc = snd_pcm_drop(pcm_playback_handle);
	if (rc < 0) {
		LOG_ERROR("audio: snd_pcm_drop failed: %s", snd_strerror(rc));
		pthread_mutex_unlock(&playback_mutex);
		return rc;
	}

	// Prepare the PCM for playback again
	rc = snd_pcm_prepare(pcm_playback_handle);
	if (rc < 0) {
		LOG_ERROR("audio: snd_pcm_prepare failed: %s", snd_strerror(rc));
		pthread_mutex_unlock(&playback_mutex);
		return rc;
	}

	pthread_mutex_unlock(&playback_mutex);

	LOG_INFO("audio: playback buffers dropped");

	return 0;
}

void jetkvm_audio_capture_close() {
	close_audio_stream(&capture_stop_requested, &capture_initializing,
	                   &capture_initialized, &capture_mutex,
	                   &pcm_capture_handle, (void**)&encoder,
	                   (codec_destroy_fn)opus_encoder_destroy, &capture_in_io);
}
