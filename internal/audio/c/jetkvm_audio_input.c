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
#include "audio_common.h"
#include <stdio.h>
#include <stdlib.h>
#include <unistd.h>
#include <signal.h>
#include <errno.h>

// Forward declarations from audio.c
extern int jetkvm_audio_playback_init(void);
extern void jetkvm_audio_playback_close(void);
extern int jetkvm_audio_decode_write(void *opus_buf, int opus_size);
extern void update_audio_decoder_constants(uint32_t sr, uint8_t ch, uint16_t fs, uint16_t max_pkt,
                                           uint32_t sleep_us, uint8_t max_attempts, uint32_t max_backoff);


static volatile sig_atomic_t g_running = 1;


/**
 * Send ACK response for heartbeat messages.
 */
static inline int32_t send_ack(int32_t client_sock) {
    return ipc_write_message(client_sock, IPC_MAGIC_INPUT, IPC_MSG_TYPE_ACK, NULL, 0);
}


/**
 * Main audio decode and playback loop.
 * Receives Opus frames via IPC, decodes, writes to ALSA.
 */
static int run_audio_loop(int client_sock, volatile sig_atomic_t *running) {
    audio_error_tracker_t tracker;
    audio_error_tracker_init(&tracker);

    // Static buffer for zero-copy IPC (no malloc/free per frame)
    static uint8_t frame_buffer[IPC_MAX_FRAME_SIZE] __attribute__((aligned(64)));

    printf("Starting audio input loop...\n");

    while (*running) {
        ipc_message_t msg;

        if (ipc_read_message_zerocopy(client_sock, &msg, IPC_MAGIC_INPUT,
                                      frame_buffer, sizeof(frame_buffer)) != 0) {
            if (*running) {
                fprintf(stderr, "Failed to read message from client\n");
            }
            break;
        }

        switch (msg.header.type) {
            case IPC_MSG_TYPE_OPUS_FRAME: {
                if (msg.header.length == 0 || msg.data == NULL) {
                    fprintf(stderr, "Warning: Empty Opus frame received\n");
                    continue;
                }

                int frames_written = jetkvm_audio_decode_write(msg.data, msg.header.length);

                if (frames_written < 0) {
                    fprintf(stderr, "Audio decode/write failed (error %d/%d)\n",
                            tracker.consecutive_errors + 1, AUDIO_MAX_CONSECUTIVE_ERRORS);

                    if (audio_error_tracker_record_error(&tracker)) {
                        fprintf(stderr, "Too many consecutive errors, giving up\n");
                        return -1;
                    }
                } else {
                    audio_error_tracker_record_success(&tracker);

                    if (audio_error_tracker_should_trace(&tracker)) {
                        printf("Processed frame %u (opus_size=%u, pcm_frames=%d)\n",
                               tracker.frame_count, msg.header.length, frames_written);
                    }
                }

                break;
            }

            case IPC_MSG_TYPE_CONFIG:
                printf("Received basic audio config\n");
                send_ack(client_sock);
                break;

            case IPC_MSG_TYPE_OPUS_CONFIG:
                audio_common_handle_opus_config(msg.data, msg.header.length, 0);
                send_ack(client_sock);
                break;

            case IPC_MSG_TYPE_STOP:
                printf("Received stop message\n");
                *running = 0;
                return 0;

            case IPC_MSG_TYPE_HEARTBEAT:
                send_ack(client_sock);
                break;

            default:
                printf("Warning: Unknown message type: %u\n", msg.header.type);
                break;
        }
    }

    printf("Audio input loop ended after %u frames\n", tracker.frame_count);
    return 0;
}


int main(int argc, char **argv) {
    audio_common_print_startup("Audio Input Server");

    // Setup signal handlers
    audio_common_setup_signal_handlers(&g_running);

    // Load configuration from environment
    audio_config_t config;
    audio_common_load_config(&config, 0);  // 0 = input server

    // Apply decoder constants to audio.c (encoder params not needed)
    update_audio_decoder_constants(
        config.sample_rate,
        config.channels,
        config.frame_size,
        AUDIO_MAX_PACKET_SIZE,
        AUDIO_SLEEP_MICROSECONDS,
        AUDIO_MAX_ATTEMPTS,
        AUDIO_MAX_BACKOFF_US
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
    audio_common_server_loop(server_sock, &g_running, run_audio_loop);

    audio_common_print_shutdown("audio input server");
    close(server_sock);
    unlink(IPC_SOCKET_INPUT);
    jetkvm_audio_playback_close();

    printf("Audio input server exited cleanly\n");
    return 0;
}
