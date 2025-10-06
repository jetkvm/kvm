/*
 * JetKVM Audio Common Utilities
 *
 * Shared functions used by both audio input and output servers
 */

#ifndef JETKVM_AUDIO_COMMON_H
#define JETKVM_AUDIO_COMMON_H

#include <signal.h>
#include <stdint.h>

// SHARED CONSTANTS

// Audio processing parameters
#define AUDIO_MAX_PACKET_SIZE     1500     // Maximum Opus packet size
#define AUDIO_SLEEP_MICROSECONDS  1000     // Default sleep time in microseconds
#define AUDIO_MAX_ATTEMPTS        5        // Maximum retry attempts
#define AUDIO_MAX_BACKOFF_US      500000   // Maximum backoff in microseconds

// Error handling
#define AUDIO_MAX_CONSECUTIVE_ERRORS  10   // Maximum consecutive errors before giving up

// Performance monitoring
#define AUDIO_TRACE_MASK          0x3FF    // Log every 1024th frame (bit mask for efficiency)

// SIGNAL HANDLERS

/**
 * Setup signal handlers for graceful shutdown.
 * Handles SIGTERM and SIGINT by setting the running flag to 0.
 * Ignores SIGPIPE to prevent crashes on broken pipe writes.
 *
 * @param running Pointer to the volatile running flag to set on shutdown
 */
void audio_common_setup_signal_handlers(volatile sig_atomic_t *running);


/**
 * Parse integer from environment variable.
 * Returns default_value if variable is not set or empty.
 *
 * @param name Environment variable name
 * @param default_value Default value if not set
 * @return Parsed integer value or default
 */
int32_t audio_common_parse_env_int(const char *name, int32_t default_value);

/**
 * Parse string from environment variable.
 * Returns default_value if variable is not set or empty.
 *
 * @param name Environment variable name
 * @param default_value Default value if not set
 * @return Environment variable value or default (not duplicated)
 */
const char* audio_common_parse_env_string(const char *name, const char *default_value);


// COMMON CONFIGURATION

/**
 * Common audio configuration structure
 */
typedef struct {
    const char *alsa_device;     // ALSA device path
    int opus_bitrate;            // Opus bitrate
    int opus_complexity;         // Opus complexity
    int sample_rate;             // Sample rate
    int channels;                // Number of channels
    int frame_size;              // Frame size in samples
} audio_config_t;

/**
 * Load common audio configuration from environment
 * @param config Output configuration
 * @param is_output true for output server, false for input
 */
void audio_common_load_config(audio_config_t *config, int is_output);

/**
 * Print server startup message
 * @param server_name Name of the server (e.g., "Audio Output Server")
 */
void audio_common_print_startup(const char *server_name);

/**
 * Print server shutdown message
 * @param server_name Name of the server
 */
void audio_common_print_shutdown(const char *server_name);

// ERROR TRACKING

/**
 * Error tracking state for audio processing loops
 */
typedef struct {
    uint8_t consecutive_errors;       // Current consecutive error count
    uint32_t frame_count;             // Total frames processed
} audio_error_tracker_t;

/**
 * Initialize error tracker
 */
static inline void audio_error_tracker_init(audio_error_tracker_t *tracker) {
    tracker->consecutive_errors = 0;
    tracker->frame_count = 0;
}

/**
 * Record an error and check if we should give up
 * Returns 1 if too many errors, 0 to continue
 */
static inline uint8_t audio_error_tracker_record_error(audio_error_tracker_t *tracker) {
    tracker->consecutive_errors++;
    return (tracker->consecutive_errors >= AUDIO_MAX_CONSECUTIVE_ERRORS) ? 1 : 0;
}

/**
 * Record success and increment frame count
 */
static inline void audio_error_tracker_record_success(audio_error_tracker_t *tracker) {
    tracker->consecutive_errors = 0;
    tracker->frame_count++;
}

/**
 * Check if we should log trace info for this frame
 */
static inline uint8_t audio_error_tracker_should_trace(audio_error_tracker_t *tracker) {
    return ((tracker->frame_count & AUDIO_TRACE_MASK) == 1) ? 1 : 0;
}


/**
 * Parse Opus config message and optionally apply to encoder.
 * @param data Raw message data
 * @param length Message length
 * @param is_encoder If true, apply config to encoder (output server)
 * @return 0 on success, -1 on error
 */
int audio_common_handle_opus_config(const uint8_t *data, uint32_t length, int is_encoder);

// IPC MAIN LOOP HELPERS

/**
 * Common server accept loop with signal handling.
 * Accepts clients and calls handler function for each connection.
 *
 * @param server_sock Server socket from ipc_create_server
 * @param running Pointer to running flag (set to 0 on shutdown)
 * @param handler Connection handler function
 * @return 0 on clean shutdown, -1 on error
 */
typedef int (*connection_handler_t)(int client_sock, volatile sig_atomic_t *running);
int audio_common_server_loop(int server_sock, volatile sig_atomic_t *running,
                             connection_handler_t handler);

#endif // JETKVM_AUDIO_COMMON_H
