/*
 * JetKVM Audio IPC Protocol
 *
 * Wire protocol for Unix domain socket communication between main process
 * and audio subprocesses. This protocol is 100% compatible with the Go
 * implementation in internal/audio/ipc_*.go
 *
 * CRITICAL: All multi-byte integers use LITTLE-ENDIAN byte order.
 */

#ifndef JETKVM_IPC_PROTOCOL_H
#define JETKVM_IPC_PROTOCOL_H

#include <stdint.h>
#include <sys/types.h>

// PROTOCOL CONSTANTS

// Magic numbers (ASCII representation when read as little-endian)
#define IPC_MAGIC_OUTPUT 0x4A4B4F55  // "JKOU" - JetKVM Output (device → browser)
#define IPC_MAGIC_INPUT  0x4A4B4D49  // "JKMI" - JetKVM Microphone Input (browser → device)

// Message types (matches Go UnifiedMessageType enum)
#define IPC_MSG_TYPE_OPUS_FRAME  0  // Audio frame data (Opus encoded)
#define IPC_MSG_TYPE_CONFIG      1  // Basic audio config (12 bytes)
#define IPC_MSG_TYPE_OPUS_CONFIG 2  // Complete Opus config (36 bytes)
#define IPC_MSG_TYPE_STOP        3  // Shutdown signal
#define IPC_MSG_TYPE_HEARTBEAT   4  // Keep-alive ping
#define IPC_MSG_TYPE_ACK         5  // Acknowledgment

// Size constraints
#define IPC_HEADER_SIZE 9         // Fixed header size (reduced from 17)
#define IPC_MAX_FRAME_SIZE 1024   // Maximum payload size (128kbps @ 20ms = ~600 bytes worst case with VBR+FEC)

// Socket paths
#define IPC_SOCKET_OUTPUT "/var/run/audio_output.sock"
#define IPC_SOCKET_INPUT  "/var/run/audio_input.sock"

// WIRE FORMAT STRUCTURES

/**
 * IPC message header (9 bytes, little-endian)
 *
 * Byte layout:
 * [0-3]   magic      uint32_t LE  Magic number (0x4A4B4F55 or 0x4A4B4D49)
 * [4]     type       uint8_t      Message type (0-5)
 * [5-8]   length     uint32_t LE  Payload size in bytes
 * [9+]    data       uint8_t[]    Variable payload
 *
 * CRITICAL: Must use __attribute__((packed)) to prevent padding.
 *
 * NOTE: Timestamp removed (was unused, saved 8 bytes per message)
 */
typedef struct __attribute__((packed)) {
    uint32_t magic;      // Magic number (LE)
    uint8_t  type;       // Message type
    uint32_t length;     // Payload length in bytes (LE)
} ipc_header_t;

/**
 * Basic audio configuration (12 bytes)
 * Message type: IPC_MSG_TYPE_CONFIG
 *
 * All fields are uint32_t little-endian.
 */
typedef struct __attribute__((packed)) {
    uint32_t sample_rate;  // Samples per second (e.g., 48000)
    uint32_t channels;     // Number of channels (e.g., 2 for stereo)
    uint32_t frame_size;   // Samples per frame (e.g., 960)
} ipc_config_t;

/**
 * Complete Opus encoder/decoder configuration (36 bytes)
 * Message type: IPC_MSG_TYPE_OPUS_CONFIG
 *
 * All fields are uint32_t little-endian.
 * Note: Negative values (like signal_type=-1000) are stored as two's complement uint32.
 */
typedef struct __attribute__((packed)) {
    uint32_t sample_rate;  // Samples per second (48000)
    uint32_t channels;     // Number of channels (2)
    uint32_t frame_size;   // Samples per frame per channel (960 = 20ms @ 48kHz)
    uint32_t bitrate;      // Bits per second (128000)
    uint32_t complexity;   // Encoder complexity 0-10 (2=balanced quality/speed)
    uint32_t vbr;          // Variable bitrate: 0=disabled, 1=enabled
    uint32_t signal_type;  // Signal type: -1000=auto, 3001=voice, 3002=music
    uint32_t bandwidth;    // Bandwidth: 1101=narrowband, 1102=mediumband, 1103=wideband, 1104=superwideband, 1105=fullband
    uint32_t dtx;          // Discontinuous transmission: 0=disabled, 1=enabled
} ipc_opus_config_t;

/**
 * Complete IPC message (header + payload)
 */
typedef struct {
    ipc_header_t header;
    uint8_t *data;         // Dynamically allocated payload (NULL if length=0)
} ipc_message_t;


/**
 * Read a complete IPC message from socket.
 *
 * This function:
 * 1. Reads exactly 9 bytes (header)
 * 2. Validates magic number
 * 3. Validates length <= IPC_MAX_FRAME_SIZE
 * 4. Allocates and reads payload if length > 0
 * 5. Stores result in msg->header and msg->data
 *
 * @param sock Socket file descriptor
 * @param msg Output message (data will be malloc'd if length > 0)
 * @param expected_magic Expected magic number (IPC_MAGIC_OUTPUT or IPC_MAGIC_INPUT)
 * @return 0 on success, -1 on error
 *
 * CALLER MUST FREE msg->data if non-NULL!
 */
int ipc_read_message(int sock, ipc_message_t *msg, uint32_t expected_magic);

/**
 * Read a complete IPC message using pre-allocated buffer (zero-copy).
 *
 * @param sock Socket file descriptor
 * @param msg Message structure to fill
 * @param expected_magic Expected magic number for validation
 * @param buffer Pre-allocated buffer for message data
 * @param buffer_size Size of pre-allocated buffer
 * @return 0 on success, -1 on error
 *
 * msg->data will point to buffer (no allocation). Caller does NOT need to free.
 */
int ipc_read_message_zerocopy(int sock, ipc_message_t *msg, uint32_t expected_magic,
                               uint8_t *buffer, uint32_t buffer_size);

/**
 * Write a complete IPC message to socket.
 *
 * This function writes header + payload atomically (if possible via writev).
 *
 * @param sock Socket file descriptor
 * @param magic Magic number (IPC_MAGIC_OUTPUT or IPC_MAGIC_INPUT)
 * @param type Message type (IPC_MSG_TYPE_*)
 * @param data Payload data (can be NULL if length=0)
 * @param length Payload length in bytes
 * @return 0 on success, -1 on error
 */
int ipc_write_message(int sock, uint32_t magic, uint8_t type,
                      const uint8_t *data, uint32_t length);

/**
 * Parse Opus configuration from message data.
 *
 * @param data Payload data (must be exactly 36 bytes)
 * @param length Payload length (must be 36)
 * @param config Output Opus configuration
 * @return 0 on success, -1 if length != 36
 */
int ipc_parse_opus_config(const uint8_t *data, uint32_t length, ipc_opus_config_t *config);

/**
 * Parse basic audio configuration from message data.
 *
 * @param data Payload data (must be exactly 12 bytes)
 * @param length Payload length (must be 12)
 * @param config Output audio configuration
 * @return 0 on success, -1 if length != 12
 */
int ipc_parse_config(const uint8_t *data, uint32_t length, ipc_config_t *config);

/**
 * Free message resources.
 *
 * @param msg Message to free (frees msg->data if non-NULL)
 */
void ipc_free_message(ipc_message_t *msg);


/**
 * Create Unix domain socket server.
 *
 * This function:
 * 1. Creates socket with AF_UNIX, SOCK_STREAM
 * 2. Removes existing socket file
 * 3. Binds to specified path
 * 4. Listens with backlog=1 (single client)
 *
 * @param socket_path Path to Unix socket (e.g., "/var/run/audio_output.sock")
 * @return Socket fd on success, -1 on error
 */
int ipc_create_server(const char *socket_path);

/**
 * Accept client connection with automatic retry.
 *
 * Blocks until client connects or error occurs.
 *
 * @param server_sock Server socket fd from ipc_create_server()
 * @return Client socket fd on success, -1 on error
 */
int ipc_accept_client(int server_sock);

/**
 * Helper: Read exactly N bytes from socket (loops until complete or error).
 *
 * @param sock Socket file descriptor
 * @param buf Output buffer
 * @param len Number of bytes to read
 * @return 0 on success, -1 on error
 */
int ipc_read_full(int sock, void *buf, size_t len);

#endif // JETKVM_IPC_PROTOCOL_H
