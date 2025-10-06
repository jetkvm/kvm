/*
 * JetKVM Audio Output Server
 *
 * Standalone C binary for audio output path:
 * ALSA Capture (TC358743 HDMI) → Opus Encode → IPC Send → Go Process → WebRTC → Browser
 *
 * This replaces the Go subprocess that was running with --audio-output-server flag.
 */

#include "ipc_protocol.h"
#include "audio_common.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <signal.h>
#include <errno.h>
#include <fcntl.h>
#include <sched.h>
#include <time.h>

// Forward declarations from audio.c
extern int jetkvm_audio_capture_init(void);
extern void jetkvm_audio_capture_close(void);
extern int jetkvm_audio_read_encode(void *opus_buf);
extern void update_audio_constants(uint32_t bitrate, uint8_t complexity,
                                   uint32_t sr, uint8_t ch, uint16_t fs, uint16_t max_pkt,
                                   uint32_t sleep_us, uint8_t max_attempts, uint32_t max_backoff);
extern int update_opus_encoder_params(uint32_t bitrate, uint8_t complexity);


static volatile sig_atomic_t g_running = 1;


static void load_output_config(audio_config_t *common) {
    audio_common_load_config(common, 1);  // 1 = output server
}


/**
 * Handle incoming IPC messages from client (non-blocking).
 * Returns 0 on success, -1 on error.
 */
static int handle_incoming_messages(int client_sock, volatile sig_atomic_t *running) {
    // Static buffer for zero-copy IPC (control messages are small)
    static uint8_t msg_buffer[IPC_MAX_FRAME_SIZE] __attribute__((aligned(64)));

    // Set non-blocking mode for client socket
    int flags = fcntl(client_sock, F_GETFL, 0);
    fcntl(client_sock, F_SETFL, flags | O_NONBLOCK);

    ipc_message_t msg;

    // Try to read message (non-blocking, zero-copy)
    int result = ipc_read_message_zerocopy(client_sock, &msg, IPC_MAGIC_OUTPUT,
                                           msg_buffer, sizeof(msg_buffer));

    // Restore blocking mode
    fcntl(client_sock, F_SETFL, flags);

    if (result != 0) {
        if (errno == EAGAIN || errno == EWOULDBLOCK) {
            return 0;  // No message available, not an error
        }
        return -1;
    }

    switch (msg.header.type) {
        case IPC_MSG_TYPE_OPUS_CONFIG:
            audio_common_handle_opus_config(msg.data, msg.header.length, 1);
            break;

        case IPC_MSG_TYPE_STOP:
            printf("Received stop message\n");
            *running = 0;
            break;

        case IPC_MSG_TYPE_HEARTBEAT:
            break;

        default:
            printf("Warning: Unknown message type: %u\n", msg.header.type);
            break;
    }

    return 0;
}


/**
 * Main audio capture and encode loop.
 * Continuously reads from ALSA, encodes to Opus, sends via IPC.
 */
static int run_audio_loop(int client_sock, volatile sig_atomic_t *running) {
    uint8_t opus_buffer[IPC_MAX_FRAME_SIZE];
    audio_error_tracker_t tracker;
    audio_error_tracker_init(&tracker);

    printf("Starting audio output loop...\n");

    while (*running) {
        if (handle_incoming_messages(client_sock, running) < 0) {
            fprintf(stderr, "Client disconnected, waiting for reconnection...\n");
            break;
        }

        int opus_size = jetkvm_audio_read_encode(opus_buffer);

        if (opus_size < 0) {
            fprintf(stderr, "Audio read/encode failed (error %d/%d)\n",
                    tracker.consecutive_errors + 1, AUDIO_MAX_CONSECUTIVE_ERRORS);

            if (audio_error_tracker_record_error(&tracker)) {
                fprintf(stderr, "Too many consecutive errors, giving up\n");
                return -1;
            }
            // No sleep needed - jetkvm_audio_read_encode already uses snd_pcm_wait internally
            continue;
        }

        if (opus_size == 0) {
            // Frame skipped for recovery, minimal yield
            sched_yield();
            continue;
        }

        audio_error_tracker_record_success(&tracker);

        if (ipc_write_message(client_sock, IPC_MAGIC_OUTPUT, IPC_MSG_TYPE_OPUS_FRAME,
                             opus_buffer, opus_size) != 0) {
            fprintf(stderr, "Failed to send frame to client\n");
            break;
        }

        if (audio_error_tracker_should_trace(&tracker)) {
            printf("Sent frame %u (size=%d bytes)\n", tracker.frame_count, opus_size);
        }
    }

    printf("Audio output loop ended after %u frames\n", tracker.frame_count);
    return 0;
}


int main(int argc, char **argv) {
    audio_common_print_startup("Audio Output Server");

    // Setup signal handlers
    audio_common_setup_signal_handlers(&g_running);

    // Load configuration from environment
    audio_config_t common;
    load_output_config(&common);

    // Apply audio constants to audio.c
    update_audio_constants(
        common.opus_bitrate,
        common.opus_complexity,
        common.sample_rate,
        common.channels,
        common.frame_size,
        AUDIO_MAX_PACKET_SIZE,
        AUDIO_SLEEP_MICROSECONDS,
        AUDIO_MAX_ATTEMPTS,
        AUDIO_MAX_BACKOFF_US
    );

    // Initialize audio capture
    printf("Initializing audio capture on device: %s\n", common.alsa_device);
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
    audio_common_server_loop(server_sock, &g_running, run_audio_loop);

    audio_common_print_shutdown("audio output server");
    close(server_sock);
    unlink(IPC_SOCKET_OUTPUT);
    jetkvm_audio_capture_close();

    printf("Audio output server exited cleanly\n");
    return 0;
}
