/*
 * JetKVM Audio Input Server
 *
 * Standalone C binary for audio input path:
 * Browser → WebRTC → Go Process → IPC Receive → Opus Decode → ALSA Playback (USB Gadget)
 *
 * This replaces the Go subprocess that was running with --audio-input-server flag.
 *
 * IMPORTANT: This binary only does OPUS DECODING (not encoding).
 * The browser already encodes audio to Opus before sending via WebRTC.
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
extern int jetkvm_audio_playback_init(void);
extern void jetkvm_audio_playback_close(void);
extern int jetkvm_audio_decode_write(void *opus_buf, int opus_size);
extern void update_audio_constants(int bitrate, int complexity, int vbr, int vbr_constraint,
                                   int signal_type, int bandwidth, int dtx, int lsb_depth,
                                   int sr, int ch, int fs, int max_pkt,
                                   int sleep_us, int max_attempts, int max_backoff);
extern void set_trace_logging(int enabled);

// Note: Input server uses decoder, not encoder, so no update_opus_encoder_params

// ============================================================================
// GLOBAL STATE
// ============================================================================

static volatile sig_atomic_t g_running = 1;  // Shutdown flag

// Audio configuration (from environment variables)
typedef struct {
    const char *alsa_device;     // ALSA playback device (default: "hw:1,0")
    int opus_bitrate;            // Opus bitrate (informational for decoder)
    int opus_complexity;         // Opus complexity (decoder ignores this)
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
        printf("Audio input server: Received signal %d, shutting down...\n", signo);
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

    // Ignore SIGPIPE
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
    config->alsa_device = parse_env_string("ALSA_PLAYBACK_DEVICE", "hw:1,0");

    // Opus configuration (informational only for decoder)
    config->opus_bitrate = parse_env_int("OPUS_BITRATE", 96000);
    config->opus_complexity = parse_env_int("OPUS_COMPLEXITY", 1);

    // Audio format
    config->sample_rate = parse_env_int("AUDIO_SAMPLE_RATE", 48000);
    config->channels = parse_env_int("AUDIO_CHANNELS", 2);
    config->frame_size = parse_env_int("AUDIO_FRAME_SIZE", 960);

    // Logging
    config->trace_logging = is_trace_enabled();

    // Log configuration
    printf("Audio Input Server Configuration:\n");
    printf("  ALSA Device:    %s\n", config->alsa_device);
    printf("  Sample Rate:    %d Hz\n", config->sample_rate);
    printf("  Channels:       %d\n", config->channels);
    printf("  Frame Size:     %d samples\n", config->frame_size);
    printf("  Trace Logging:  %s\n", config->trace_logging ? "enabled" : "disabled");
}

// ============================================================================
// MESSAGE HANDLING
// ============================================================================

/**
 * Handle OpusConfig message: informational only for decoder.
 * Decoder config updates are less critical than encoder.
 * Returns 0 on success.
 */
static int handle_opus_config(const uint8_t *data, uint32_t length) {
    ipc_opus_config_t config;

    if (ipc_parse_opus_config(data, length, &config) != 0) {
        fprintf(stderr, "Failed to parse Opus config\n");
        return -1;
    }

    printf("Received Opus config (informational): bitrate=%u, complexity=%u\n",
           config.bitrate, config.complexity);

    // Note: Decoder doesn't need most of these parameters.
    // Opus decoder automatically adapts to encoder settings embedded in stream.
    // FEC (Forward Error Correction) is enabled automatically when present in packets.

    return 0;
}

/**
 * Send ACK response for heartbeat messages.
 */
static int send_ack(int client_sock) {
    return ipc_write_message(client_sock, IPC_MAGIC_INPUT, IPC_MSG_TYPE_ACK, NULL, 0);
}

// ============================================================================
// MAIN LOOP
// ============================================================================

/**
 * Main audio decode and playback loop.
 * Receives Opus frames via IPC, decodes, writes to ALSA.
 */
static int run_audio_loop(int client_sock) {
    int consecutive_errors = 0;
    const int max_consecutive_errors = 10;
    int frame_count = 0;

    printf("Starting audio input loop...\n");

    while (g_running) {
        ipc_message_t msg;

        // Read message from client (blocking)
        if (ipc_read_message(client_sock, &msg, IPC_MAGIC_INPUT) != 0) {
            if (g_running) {
                fprintf(stderr, "Failed to read message from client\n");
            }
            break;  // Client disconnected or error
        }

        // Process message based on type
        switch (msg.header.type) {
            case IPC_MSG_TYPE_OPUS_FRAME: {
                if (msg.header.length == 0 || msg.data == NULL) {
                    fprintf(stderr, "Warning: Empty Opus frame received\n");
                    ipc_free_message(&msg);
                    continue;
                }

                // Decode Opus and write to ALSA
                int frames_written = jetkvm_audio_decode_write(msg.data, msg.header.length);

                if (frames_written < 0) {
                    consecutive_errors++;
                    fprintf(stderr, "Audio decode/write failed (error %d/%d)\n",
                            consecutive_errors, max_consecutive_errors);

                    if (consecutive_errors >= max_consecutive_errors) {
                        fprintf(stderr, "Too many consecutive errors, giving up\n");
                        ipc_free_message(&msg);
                        return -1;
                    }
                } else {
                    // Success - reset error counter
                    consecutive_errors = 0;
                    frame_count++;

                    // Trace logging (periodic)
                    if (frame_count % 1000 == 1) {
                        printf("Processed frame %d (opus_size=%u, pcm_frames=%d)\n",
                               frame_count, msg.header.length, frames_written);
                    }
                }

                break;
            }

            case IPC_MSG_TYPE_CONFIG:
                printf("Received basic audio config\n");
                send_ack(client_sock);
                break;

            case IPC_MSG_TYPE_OPUS_CONFIG:
                handle_opus_config(msg.data, msg.header.length);
                send_ack(client_sock);
                break;

            case IPC_MSG_TYPE_STOP:
                printf("Received stop message\n");
                ipc_free_message(&msg);
                g_running = 0;
                return 0;

            case IPC_MSG_TYPE_HEARTBEAT:
                send_ack(client_sock);
                break;

            default:
                printf("Warning: Unknown message type: %u\n", msg.header.type);
                break;
        }

        ipc_free_message(&msg);
    }

    printf("Audio input loop ended after %d frames\n", frame_count);
    return 0;
}

// ============================================================================
// MAIN
// ============================================================================

int main(int argc, char **argv) {
    printf("JetKVM Audio Input Server Starting...\n");

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
        1,         // vbr
        1,         // vbr_constraint
        -1000,     // signal_type (auto)
        1103,      // bandwidth (wideband)
        0,         // dtx
        16,        // lsb_depth
        config.sample_rate,
        config.channels,
        config.frame_size,
        1500,      // max_packet_size
        1000,      // sleep_microseconds
        5,         // max_attempts
        500000     // max_backoff_us
    );

    // Initialize audio playback (Opus decoder + ALSA playback)
    printf("Initializing audio playback on device: %s\n", config.alsa_device);
    if (jetkvm_audio_playback_init() != 0) {
        fprintf(stderr, "Failed to initialize audio playback\n");
        return 1;
    }

    // Create IPC server
    int server_sock = ipc_create_server(IPC_SOCKET_INPUT);
    if (server_sock < 0) {
        fprintf(stderr, "Failed to create IPC server\n");
        jetkvm_audio_playback_close();
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
    printf("Shutting down audio input server...\n");
    close(server_sock);
    unlink(IPC_SOCKET_INPUT);
    jetkvm_audio_playback_close();

    printf("Audio input server exited cleanly\n");
    return 0;
}
