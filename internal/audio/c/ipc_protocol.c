/*
 * JetKVM Audio IPC Protocol Implementation
 *
 * Implements Unix domain socket communication with exact byte-level
 * compatibility with Go implementation in internal/audio/ipc_*.go
 */

#include "ipc_protocol.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <errno.h>
#include <time.h>
#include <sys/socket.h>
#include <sys/un.h>
#include <sys/uio.h>
#include <endian.h>

// ============================================================================
// HELPER FUNCTIONS
// ============================================================================

/**
 * Read exactly N bytes from socket (loops until complete or error).
 * This is critical because read() may return partial data.
 */
int ipc_read_full(int sock, void *buf, size_t len) {
    uint8_t *ptr = (uint8_t *)buf;
    size_t remaining = len;

    while (remaining > 0) {
        ssize_t n = read(sock, ptr, remaining);

        if (n < 0) {
            if (errno == EINTR) {
                continue;  // Interrupted by signal, retry
            }
            return -1;  // Read error
        }

        if (n == 0) {
            return -1;  // EOF (connection closed)
        }

        ptr += n;
        remaining -= n;
    }

    return 0;  // Success
}

/**
 * Get current time in nanoseconds (Unix epoch).
 * Compatible with Go time.Now().UnixNano().
 */
int64_t ipc_get_time_ns(void) {
    struct timespec ts;
    if (clock_gettime(CLOCK_REALTIME, &ts) != 0) {
        return 0;  // Fallback on error
    }
    return (int64_t)ts.tv_sec * 1000000000LL + (int64_t)ts.tv_nsec;
}

// ============================================================================
// MESSAGE READ/WRITE
// ============================================================================

/**
 * Read a complete IPC message from socket.
 * Returns 0 on success, -1 on error.
 * Caller MUST free msg->data if non-NULL!
 */
int ipc_read_message(int sock, ipc_message_t *msg, uint32_t expected_magic) {
    if (msg == NULL) {
        return -1;
    }

    // Initialize message
    memset(msg, 0, sizeof(ipc_message_t));

    // 1. Read header (17 bytes)
    if (ipc_read_full(sock, &msg->header, IPC_HEADER_SIZE) != 0) {
        return -1;
    }

    // 2. Convert from little-endian (required on big-endian systems)
    msg->header.magic = le32toh(msg->header.magic);
    msg->header.length = le32toh(msg->header.length);
    msg->header.timestamp = le64toh(msg->header.timestamp);
    // Note: type is uint8_t, no conversion needed

    // 3. Validate magic number
    if (msg->header.magic != expected_magic) {
        fprintf(stderr, "IPC: Invalid magic number: got 0x%08X, expected 0x%08X\n",
                msg->header.magic, expected_magic);
        return -1;
    }

    // 4. Validate length
    if (msg->header.length > IPC_MAX_FRAME_SIZE) {
        fprintf(stderr, "IPC: Message too large: %u bytes (max %d)\n",
                msg->header.length, IPC_MAX_FRAME_SIZE);
        return -1;
    }

    // 5. Read payload if present
    if (msg->header.length > 0) {
        msg->data = malloc(msg->header.length);
        if (msg->data == NULL) {
            fprintf(stderr, "IPC: Failed to allocate %u bytes for payload\n",
                    msg->header.length);
            return -1;
        }

        if (ipc_read_full(sock, msg->data, msg->header.length) != 0) {
            free(msg->data);
            msg->data = NULL;
            return -1;
        }
    }

    return 0;  // Success
}

/**
 * Write a complete IPC message to socket.
 * Uses writev() for atomic header+payload write.
 * Returns 0 on success, -1 on error.
 */
int ipc_write_message(int sock, uint32_t magic, uint8_t type,
                      const uint8_t *data, uint32_t length) {
    // Validate length
    if (length > IPC_MAX_FRAME_SIZE) {
        fprintf(stderr, "IPC: Message too large: %u bytes (max %d)\n",
                length, IPC_MAX_FRAME_SIZE);
        return -1;
    }

    // Prepare header
    ipc_header_t header;
    header.magic = htole32(magic);
    header.type = type;
    header.length = htole32(length);
    header.timestamp = htole64(ipc_get_time_ns());

    // Use writev for atomic write (if possible)
    struct iovec iov[2];
    iov[0].iov_base = &header;
    iov[0].iov_len = IPC_HEADER_SIZE;
    iov[1].iov_base = (void *)data;
    iov[1].iov_len = length;

    int iovcnt = (length > 0) ? 2 : 1;
    size_t total_len = IPC_HEADER_SIZE + length;

    ssize_t written = writev(sock, iov, iovcnt);

    if (written < 0) {
        if (errno == EINTR) {
            // Retry once on interrupt
            written = writev(sock, iov, iovcnt);
        }

        if (written < 0) {
            perror("IPC: writev failed");
            return -1;
        }
    }

    if ((size_t)written != total_len) {
        fprintf(stderr, "IPC: Partial write: %zd/%zu bytes\n", written, total_len);
        return -1;
    }

    return 0;  // Success
}

// ============================================================================
// CONFIGURATION PARSING
// ============================================================================

/**
 * Parse Opus configuration from message data (36 bytes, little-endian).
 */
int ipc_parse_opus_config(const uint8_t *data, uint32_t length, ipc_opus_config_t *config) {
    if (data == NULL || config == NULL) {
        return -1;
    }

    if (length != 36) {
        fprintf(stderr, "IPC: Invalid Opus config size: %u bytes (expected 36)\n", length);
        return -1;
    }

    // Parse little-endian uint32 fields
    const uint32_t *u32_data = (const uint32_t *)data;
    config->sample_rate = le32toh(u32_data[0]);
    config->channels = le32toh(u32_data[1]);
    config->frame_size = le32toh(u32_data[2]);
    config->bitrate = le32toh(u32_data[3]);
    config->complexity = le32toh(u32_data[4]);
    config->vbr = le32toh(u32_data[5]);
    config->signal_type = le32toh(u32_data[6]);
    config->bandwidth = le32toh(u32_data[7]);
    config->dtx = le32toh(u32_data[8]);

    return 0;  // Success
}

/**
 * Parse basic audio configuration from message data (12 bytes, little-endian).
 */
int ipc_parse_config(const uint8_t *data, uint32_t length, ipc_config_t *config) {
    if (data == NULL || config == NULL) {
        return -1;
    }

    if (length != 12) {
        fprintf(stderr, "IPC: Invalid config size: %u bytes (expected 12)\n", length);
        return -1;
    }

    // Parse little-endian uint32 fields
    const uint32_t *u32_data = (const uint32_t *)data;
    config->sample_rate = le32toh(u32_data[0]);
    config->channels = le32toh(u32_data[1]);
    config->frame_size = le32toh(u32_data[2]);

    return 0;  // Success
}

/**
 * Free message resources.
 */
void ipc_free_message(ipc_message_t *msg) {
    if (msg != NULL && msg->data != NULL) {
        free(msg->data);
        msg->data = NULL;
    }
}

// ============================================================================
// SOCKET MANAGEMENT
// ============================================================================

/**
 * Create Unix domain socket server.
 */
int ipc_create_server(const char *socket_path) {
    if (socket_path == NULL) {
        return -1;
    }

    // 1. Create socket
    int sock = socket(AF_UNIX, SOCK_STREAM, 0);
    if (sock < 0) {
        perror("IPC: socket() failed");
        return -1;
    }

    // 2. Remove existing socket file (ignore errors)
    unlink(socket_path);

    // 3. Bind to path
    struct sockaddr_un addr;
    memset(&addr, 0, sizeof(addr));
    addr.sun_family = AF_UNIX;

    if (strlen(socket_path) >= sizeof(addr.sun_path)) {
        fprintf(stderr, "IPC: Socket path too long: %s\n", socket_path);
        close(sock);
        return -1;
    }

    strncpy(addr.sun_path, socket_path, sizeof(addr.sun_path) - 1);

    if (bind(sock, (struct sockaddr *)&addr, sizeof(addr)) < 0) {
        perror("IPC: bind() failed");
        close(sock);
        return -1;
    }

    // 4. Listen with backlog=1 (single client)
    if (listen(sock, 1) < 0) {
        perror("IPC: listen() failed");
        close(sock);
        return -1;
    }

    printf("IPC: Server listening on %s\n", socket_path);
    return sock;
}

/**
 * Accept client connection.
 */
int ipc_accept_client(int server_sock) {
    int client_sock = accept(server_sock, NULL, NULL);

    if (client_sock < 0) {
        perror("IPC: accept() failed");
        return -1;
    }

    printf("IPC: Client connected (fd=%d)\n", client_sock);
    return client_sock;
}
