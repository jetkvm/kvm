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
static const int max_attempts_global = 5;     // Maximum retry attempts
static const int max_backoff_us_global = 500000; // Maximum backoff time

// Performance optimization flags
static const int optimized_buffer_size = 1;   // Use optimized buffer sizing
static int trace_logging_enabled = 0;   // Enable detailed trace logging

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

	// Optimize buffer sizes for constrained hardware
	snd_pcm_uframes_t period_size = frame_size;
	if (optimized_buffer_size) {
		// Use smaller periods for lower latency on constrained hardware
		period_size = frame_size / 2;
		if (period_size < 64) period_size = 64; // Minimum safe period size
	}
	err = snd_pcm_hw_params_set_period_size_near(handle, params, &period_size, 0);
	if (err < 0) return err;

	// Optimize buffer size based on hardware constraints
	snd_pcm_uframes_t buffer_size;
	if (optimized_buffer_size) {
		// Use 2 periods for ultra-low latency on constrained hardware
		buffer_size = period_size * 2;
	} else {
		// Standard 4 periods for good latency/stability balance
		buffer_size = period_size * 4;
	}
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
		if (pcm_capture_handle) { snd_pcm_close(pcm_capture_handle); pcm_capture_handle = NULL; }
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
	static short __attribute__((aligned(16))) pcm_buffer[1920]; // max 2ch*960, aligned for SIMD
	unsigned char * __restrict__ out = (unsigned char*)opus_buf;
	
	// Prefetch output buffer for better cache performance
	__builtin_prefetch(out, 1, 3);
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

	// If we got fewer frames than expected, pad with silence
	if (__builtin_expect(pcm_rc < frame_size, 0)) {
		__builtin_memset(&pcm_buffer[pcm_rc * channels], 0, (frame_size - pcm_rc) * channels * sizeof(short));
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
	__builtin_prefetch(in, 0, 3);
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
