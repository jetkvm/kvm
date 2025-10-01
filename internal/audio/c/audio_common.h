/*
 * JetKVM Audio Common Utilities
 *
 * Shared functions used by both audio input and output servers
 */

#ifndef JETKVM_AUDIO_COMMON_H
#define JETKVM_AUDIO_COMMON_H

#include <signal.h>

// ============================================================================
// SIGNAL HANDLERS
// ============================================================================

/**
 * Setup signal handlers for graceful shutdown.
 * Handles SIGTERM and SIGINT by setting the running flag to 0.
 * Ignores SIGPIPE to prevent crashes on broken pipe writes.
 *
 * @param running Pointer to the volatile running flag to set on shutdown
 */
void audio_common_setup_signal_handlers(volatile sig_atomic_t *running);

// ============================================================================
// CONFIGURATION PARSING
// ============================================================================

/**
 * Parse integer from environment variable.
 * Returns default_value if variable is not set or empty.
 *
 * @param name Environment variable name
 * @param default_value Default value if not set
 * @return Parsed integer value or default
 */
int audio_common_parse_env_int(const char *name, int default_value);

/**
 * Parse string from environment variable.
 * Returns default_value if variable is not set or empty.
 *
 * @param name Environment variable name
 * @param default_value Default value if not set
 * @return Environment variable value or default (not duplicated)
 */
const char* audio_common_parse_env_string(const char *name, const char *default_value);

/**
 * Check if trace logging is enabled for audio subsystem.
 * Looks for "audio" in PION_LOG_TRACE comma-separated list.
 *
 * @return 1 if enabled, 0 otherwise
 */
int audio_common_is_trace_enabled(void);

#endif // JETKVM_AUDIO_COMMON_H
