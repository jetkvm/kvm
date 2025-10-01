/*
 * JetKVM Audio Common Utilities
 *
 * Shared functions used by both audio input and output servers
 */

#include "audio_common.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <signal.h>

// ============================================================================
// GLOBAL STATE FOR SIGNAL HANDLER
// ============================================================================

// Pointer to the running flag that will be set to 0 on shutdown
static volatile sig_atomic_t *g_running_ptr = NULL;

// ============================================================================
// SIGNAL HANDLERS
// ============================================================================

static void signal_handler(int signo) {
    if (signo == SIGTERM || signo == SIGINT) {
        printf("Audio server: Received signal %d, shutting down...\n", signo);
        if (g_running_ptr != NULL) {
            *g_running_ptr = 0;
        }
    }
}

void audio_common_setup_signal_handlers(volatile sig_atomic_t *running) {
    g_running_ptr = running;

    struct sigaction sa;
    memset(&sa, 0, sizeof(sa));
    sa.sa_handler = signal_handler;
    sigemptyset(&sa.sa_mask);
    sa.sa_flags = 0;

    sigaction(SIGTERM, &sa, NULL);
    sigaction(SIGINT, &sa, NULL);

    // Ignore SIGPIPE (write to closed socket should return error, not crash)
    signal(SIGPIPE, SIG_IGN);
}

// ============================================================================
// CONFIGURATION PARSING
// ============================================================================

int audio_common_parse_env_int(const char *name, int default_value) {
    const char *str = getenv(name);
    if (str == NULL || str[0] == '\0') {
        return default_value;
    }
    return atoi(str);
}

const char* audio_common_parse_env_string(const char *name, const char *default_value) {
    const char *str = getenv(name);
    if (str == NULL || str[0] == '\0') {
        return default_value;
    }
    return str;
}

int audio_common_is_trace_enabled(void) {
    const char *pion_trace = getenv("PION_LOG_TRACE");
    if (pion_trace == NULL) {
        return 0;
    }

    // Check if "audio" is in comma-separated list
    if (strstr(pion_trace, "audio") != NULL) {
        return 1;
    }

    return 0;
}
