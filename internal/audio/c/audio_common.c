/*
 * JetKVM Audio Common Utilities
 *
 * Shared functions used by both audio input and output servers
 */

#include "audio_common.h"
#include "ipc_protocol.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <signal.h>
#include <unistd.h>
#include <sys/socket.h>
#include <time.h>

// Forward declarations for encoder update (only in output server)
extern int update_opus_encoder_params(uint32_t bitrate, uint8_t complexity);

// GLOBAL STATE FOR SIGNAL HANDLER

// Pointer to the running flag that will be set to 0 on shutdown
static volatile sig_atomic_t *g_running_ptr = NULL;

// SIGNAL HANDLERS

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


int32_t audio_common_parse_env_int(const char *name, int32_t default_value) {
    const char *str = getenv(name);
    if (str == NULL || str[0] == '\0') {
        return default_value;
    }
    return (int32_t)atoi(str);
}

const char* audio_common_parse_env_string(const char *name, const char *default_value) {
    const char *str = getenv(name);
    if (str == NULL || str[0] == '\0') {
        return default_value;
    }
    return str;
}

// COMMON CONFIGURATION

void audio_common_load_config(audio_config_t *config, int is_output) {
    // ALSA device configuration
    if (is_output) {
        config->alsa_device = audio_common_parse_env_string("ALSA_CAPTURE_DEVICE", "hw:0,0");
    } else {
        config->alsa_device = audio_common_parse_env_string("ALSA_PLAYBACK_DEVICE", "hw:1,0");
    }

    // Common Opus configuration
    config->opus_bitrate = audio_common_parse_env_int("OPUS_BITRATE", 128000);
    config->opus_complexity = audio_common_parse_env_int("OPUS_COMPLEXITY", 2);

    // Audio format
    config->sample_rate = audio_common_parse_env_int("AUDIO_SAMPLE_RATE", 48000);
    config->channels = audio_common_parse_env_int("AUDIO_CHANNELS", 2);
    config->frame_size = audio_common_parse_env_int("AUDIO_FRAME_SIZE", 960);

    // Log configuration
    printf("Audio %s Server Configuration:\n", is_output ? "Output" : "Input");
    printf("  ALSA Device:    %s\n", config->alsa_device);
    printf("  Sample Rate:    %d Hz\n", config->sample_rate);
    printf("  Channels:       %d\n", config->channels);
    printf("  Frame Size:     %d samples\n", config->frame_size);
    if (is_output) {
        printf("  Opus Bitrate:   %d bps\n", config->opus_bitrate);
        printf("  Opus Complexity: %d\n", config->opus_complexity);
    }
}

void audio_common_print_startup(const char *server_name) {
    printf("JetKVM %s Starting...\n", server_name);
}

void audio_common_print_shutdown(const char *server_name) {
    printf("Shutting down %s...\n", server_name);
}


int audio_common_handle_opus_config(const uint8_t *data, uint32_t length, int is_encoder) {
    ipc_opus_config_t config;

    if (ipc_parse_opus_config(data, length, &config) != 0) {
        fprintf(stderr, "Failed to parse Opus config\n");
        return -1;
    }

    if (is_encoder) {
        printf("Received Opus config: bitrate=%u, complexity=%u\n",
               config.bitrate, config.complexity);

        int result = update_opus_encoder_params(
            config.bitrate,
            config.complexity
        );

        if (result != 0) {
            fprintf(stderr, "Warning: Failed to apply Opus encoder parameters\n");
        }
    } else {
        printf("Received Opus config (informational): bitrate=%u, complexity=%u\n",
               config.bitrate, config.complexity);
    }

    return 0;
}

// IPC MAIN LOOP HELPERS

int audio_common_server_loop(int server_sock, volatile sig_atomic_t *running,
                             connection_handler_t handler) {
    while (*running) {
        printf("Waiting for client connection...\n");

        int client_sock = accept(server_sock, NULL, NULL);
        if (client_sock < 0) {
            if (*running) {
                fprintf(stderr, "Failed to accept client, retrying...\n");
                struct timespec ts = {1, 0};  // 1 second
                nanosleep(&ts, NULL);
                continue;
            }
            break;
        }

        printf("Client connected (fd=%d)\n", client_sock);

        // Run handler with this client
        handler(client_sock, running);

        // Close client connection
        close(client_sock);

        if (*running) {
            printf("Client disconnected, waiting for next client...\n");
        }
    }

    return 0;
}
