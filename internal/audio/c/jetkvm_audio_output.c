/*
 * JetKVM Audio Output Server
 *
 * Standalone C binary for audio output path:
 * ALSA Capture (TC358743 HDMI) → Opus Encode → IPC Send → Go Process → WebRTC → Browser
 *
 * This replaces the Go subprocess that was running with --audio-output-server flag.
 */

#include "ipc_protocol.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <signal.h>
#include <errno.h>
#include <fcntl.h>

// Forward declarations from audio.c
extern int jetkvm_audio_capture_init(void);
extern void jetkvm_audio_capture_close(void);
extern int jetkvm_audio_read_encode(void *opus_buf);
extern void update_audio_constants(int bitrate, int complexity, int vbr, int vbr_constraint,
                                   int signal_type, int bandwidth, int dtx, int lsb_depth,
                                   int sr, int ch, int fs, int max_pkt,
                                   int sleep_us, int max_attempts, int max_backoff);
extern void set_trace_logging(int enabled);
extern int update_opus_encoder_params(int bitrate, int complexity, int vbr, int vbr_constraint,
                                      int signal_type, int bandwidth, int dtx);

// ============================================================================
// GLOBAL STATE
// ============================================================================

static volatile sig_atomic_t g_running = 1;  // Shutdown flag

// Audio configuration (from environment variables)
typedef struct {
    const char *alsa_device;     // ALSA capture device (default: "hw:0,0")
    int opus_bitrate;            // Opus bitrate (default: 96000)
    int opus_complexity;         // Opus complexity 0-10 (default: 1)
    int opus_vbr;                // VBR enabled (default: 1)
    int opus_vbr_constraint;     // VBR constraint (default: 1)
    int opus_signal_type;        // Signal type (default: -1000 = auto)
    int opus_bandwidth;          // Bandwidth (default: 1103 = wideband)
    int opus_dtx;                // DTX enabled (default: 0)
    int opus_lsb_depth;          // LSB depth (default: 16)
    int sample_rate;             // Sample rate (default: 48000)
    int channels;                // Channels (default: 2)
    int frame_size;              // Frame size in samples (default: 960)
    int trace_logging;           // Enable trace logging (default: 0)
} audio_config_t;

// ============================================================================
// SIGNAL HANDLERS
// ============================================================================

static void signal_handler(int signo) {
    if (signo == SIGTERM || signo == SIGINT) {
        printf("Audio output server: Received signal %d, shutting down...\n", signo);
        g_running = 0;
    }
}

static void setup_signal_handlers(void) {
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

static int parse_env_int(const char *name, int default_value) {
    const char *str = getenv(name);
    if (str == NULL || str[0] == '\0') {
        return default_value;
    }
    return atoi(str);
}

static const char* parse_env_string(const char *name, const char *default_value) {
    const char *str = getenv(name);
    if (str == NULL || str[0] == '\0') {
        return default_value;
    }
    return str;
}

static int is_trace_enabled(void) {
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

static void load_audio_config(audio_config_t *config) {
    // ALSA device configuration
    config->alsa_device = parse_env_string("ALSA_CAPTURE_DEVICE", "hw:0,0");

    // Opus encoder configuration
    config->opus_bitrate = parse_env_int("OPUS_BITRATE", 96000);
    config->opus_complexity = parse_env_int("OPUS_COMPLEXITY", 1);
    config->opus_vbr = parse_env_int("OPUS_VBR", 1);
    config->opus_vbr_constraint = parse_env_int("OPUS_VBR_CONSTRAINT", 1);
    config->opus_signal_type = parse_env_int("OPUS_SIGNAL_TYPE", -1000);
    config->opus_bandwidth = parse_env_int("OPUS_BANDWIDTH", 1103);
    config->opus_dtx = parse_env_int("OPUS_DTX", 0);
    config->opus_lsb_depth = parse_env_int("OPUS_LSB_DEPTH", 16);

    // Audio format
    config->sample_rate = parse_env_int("AUDIO_SAMPLE_RATE", 48000);
    config->channels = parse_env_int("AUDIO_CHANNELS", 2);
    config->frame_size = parse_env_int("AUDIO_FRAME_SIZE", 960);

    // Logging
    config->trace_logging = is_trace_enabled();

    // Log configuration
    printf("Audio Output Server Configuration:\n");
    printf("  ALSA Device:    %s\n", config->alsa_device);
    printf("  Sample Rate:    %d Hz\n", config->sample_rate);
    printf("  Channels:       %d\n", config->channels);
    printf("  Frame Size:     %d samples\n", config->frame_size);
    printf("  Opus Bitrate:   %d bps\n", config->opus_bitrate);
    printf("  Opus Complexity: %d\n", config->opus_complexity);
    printf("  Trace Logging:  %s\n", config->trace_logging ? "enabled" : "disabled");
}

// ============================================================================
// MESSAGE HANDLING
// ============================================================================

/**
 * Handle OpusConfig message: update encoder parameters dynamically.
 * Returns 0 on success, -1 on error.
 */
static int handle_opus_config(const uint8_t *data, uint32_t length) {
    ipc_opus_config_t config;

    if (ipc_parse_opus_config(data, length, &config) != 0) {
        fprintf(stderr, "Failed to parse Opus config\n");
        return -1;
    }

    printf("Received Opus config: bitrate=%u, complexity=%u, vbr=%u\n",
           config.bitrate, config.complexity, config.vbr);

    // Apply configuration to encoder
    // Note: Signal type needs special handling for negative values
    int signal_type = (int)(int32_t)config.signal_type;  // Treat as signed

    int result = update_opus_encoder_params(
        config.bitrate,
        config.complexity,
        config.vbr,
        config.vbr,  // Use VBR value for constraint (simplified)
        signal_type,
        config.bandwidth,
        config.dtx
    );

    if (result != 0) {
        fprintf(stderr, "Warning: Failed to apply some Opus encoder parameters\n");
        // Continue anyway - encoder may not be initialized yet
    }

    return 0;
}

/**
 * Handle incoming IPC messages from client (non-blocking).
 * Returns 0 on success, -1 on error.
 */
static int handle_incoming_messages(int client_sock) {
    // Set non-blocking mode for client socket
    int flags = fcntl(client_sock, F_GETFL, 0);
    fcntl(client_sock, F_SETFL, flags | O_NONBLOCK);

    ipc_message_t msg;

    // Try to read message (non-blocking)
    int result = ipc_read_message(client_sock, &msg, IPC_MAGIC_OUTPUT);

    // Restore blocking mode
    fcntl(client_sock, F_SETFL, flags);

    if (result != 0) {
        if (errno == EAGAIN || errno == EWOULDBLOCK) {
            return 0;  // No message available, not an error
        }
        return -1;  // Connection error
    }

    // Process message based on type
    switch (msg.header.type) {
        case IPC_MSG_TYPE_OPUS_CONFIG:
            handle_opus_config(msg.data, msg.header.length);
            break;

        case IPC_MSG_TYPE_STOP:
            printf("Received stop message\n");
            g_running = 0;
            break;

        case IPC_MSG_TYPE_HEARTBEAT:
            // Informational only, no response needed
            break;

        default:
            printf("Warning: Unknown message type: %u\n", msg.header.type);
            break;
    }

    ipc_free_message(&msg);
    return 0;
}

// ============================================================================
// MAIN LOOP
// ============================================================================

/**
 * Main audio capture and encode loop.
 * Continuously reads from ALSA, encodes to Opus, sends via IPC.
 */
static int run_audio_loop(int client_sock) {
    uint8_t opus_buffer[IPC_MAX_FRAME_SIZE];
    int consecutive_errors = 0;
    const int max_consecutive_errors = 10;
    int frame_count = 0;

    printf("Starting audio output loop...\n");

    while (g_running) {
        // Handle any incoming configuration messages (non-blocking)
        if (handle_incoming_messages(client_sock) < 0) {
            fprintf(stderr, "Client disconnected, waiting for reconnection...\n");
            break;  // Client disconnected
        }

        // Capture audio and encode to Opus
        int opus_size = jetkvm_audio_read_encode(opus_buffer);

        if (opus_size < 0) {
            consecutive_errors++;
            fprintf(stderr, "Audio read/encode failed (error %d/%d)\n",
                    consecutive_errors, max_consecutive_errors);

            if (consecutive_errors >= max_consecutive_errors) {
                fprintf(stderr, "Too many consecutive errors, giving up\n");
                return -1;
            }

            usleep(10000);  // 10ms backoff
            continue;
        }

        if (opus_size == 0) {
            // No data available (non-blocking mode or empty frame)
            usleep(1000);  // 1ms sleep
            continue;
        }

        // Reset error counter on success
        consecutive_errors = 0;
        frame_count++;

        // Send Opus frame via IPC
        if (ipc_write_message(client_sock, IPC_MAGIC_OUTPUT, IPC_MSG_TYPE_OPUS_FRAME,
                             opus_buffer, opus_size) != 0) {
            fprintf(stderr, "Failed to send frame to client\n");
            break;  // Client disconnected
        }

        // Trace logging (periodic)
        if (frame_count % 1000 == 1) {
            printf("Sent frame %d (size=%d bytes)\n", frame_count, opus_size);
        }

        // Small delay to prevent busy-waiting (frame rate ~50 FPS @ 48kHz/960)
        usleep(1000);  // 1ms
    }

    printf("Audio output loop ended after %d frames\n", frame_count);
    return 0;
}

// ============================================================================
// MAIN
// ============================================================================

int main(int argc, char **argv) {
    printf("JetKVM Audio Output Server Starting...\n");

    // Setup signal handlers
    setup_signal_handlers();

    // Load configuration from environment
    audio_config_t config;
    load_audio_config(&config);

    // Set trace logging
    set_trace_logging(config.trace_logging);

    // Apply audio constants to audio.c
    update_audio_constants(
        config.opus_bitrate,
        config.opus_complexity,
        config.opus_vbr,
        config.opus_vbr_constraint,
        config.opus_signal_type,
        config.opus_bandwidth,
        config.opus_dtx,
        config.opus_lsb_depth,
        config.sample_rate,
        config.channels,
        config.frame_size,
        1500,      // max_packet_size
        1000,      // sleep_microseconds
        5,         // max_attempts
        500000     // max_backoff_us
    );

    // Initialize audio capture
    printf("Initializing audio capture on device: %s\n", config.alsa_device);
    if (jetkvm_audio_capture_init() != 0) {
        fprintf(stderr, "Failed to initialize audio capture\n");
        return 1;
    }

    // Create IPC server
    int server_sock = ipc_create_server(IPC_SOCKET_OUTPUT);
    if (server_sock < 0) {
        fprintf(stderr, "Failed to create IPC server\n");
        jetkvm_audio_capture_close();
        return 1;
    }

    // Main connection loop
    while (g_running) {
        printf("Waiting for client connection...\n");

        int client_sock = ipc_accept_client(server_sock);
        if (client_sock < 0) {
            if (g_running) {
                fprintf(stderr, "Failed to accept client, retrying...\n");
                sleep(1);
                continue;
            }
            break;  // Shutting down
        }

        // Run audio loop with this client
        run_audio_loop(client_sock);

        // Close client connection
        close(client_sock);

        if (g_running) {
            printf("Client disconnected, waiting for next client...\n");
        }
    }

    // Cleanup
    printf("Shutting down audio output server...\n");
    close(server_sock);
    unlink(IPC_SOCKET_OUTPUT);
    jetkvm_audio_capture_close();

    printf("Audio output server exited cleanly\n");
    return 0;
}
