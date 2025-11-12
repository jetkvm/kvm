#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <sys/socket.h>
#include <sys/un.h>
#include <sys/select.h>
#include <fcntl.h>
#include <errno.h>
#include <pthread.h>
#include "ctrl.h"
#include "main.h"

#define SOCKET_PATH "/tmp/video.sock"
#define BUFFER_SIZE 4096

// Global state
static int client_fd = -1;
static pthread_mutex_t client_fd_mutex = PTHREAD_MUTEX_INITIALIZER;

void jetkvm_c_log_handler(int level, const char *filename, const char *funcname, int line, const char *message) {
    // printf("[%s] %s:%d %s: %s\n", filename ? filename : "unknown", funcname ? funcname : "unknown", line, message ? message : "");
    fprintf(stderr, "[%s] %s:%d %s: %s\n", filename ? filename : "unknown", funcname ? funcname : "unknown", line, message ? message : "");
}

// Video handler that pipes frames to the Unix socket
// This will be called by the video subsystem via video_send_frame -> jetkvm_set_video_handler's handler
void jetkvm_video_handler(const uint8_t *frame, ssize_t len) {
    // pthread_mutex_lock(&client_fd_mutex);
    // if (client_fd >= 0 && frame != NULL && len > 0) {
    //     ssize_t bytes_written = 0;
    //     while (bytes_written < len) {
    //         ssize_t n = write(client_fd, frame + bytes_written, len - bytes_written);
    //         if (n < 0) {
    //             if (errno == EPIPE || errno == ECONNRESET) {
    //                 // Client disconnected
    //                 close(client_fd);
    //                 client_fd = -1;
    //                 break;
    //             }
    //             perror("write");
    //             break;
    //         }
    //         bytes_written += n;
    //     }
    // }
    // pthread_mutex_unlock(&client_fd_mutex);
}

void jetkvm_video_state_handler(jetkvm_video_state_t *state) {
    fprintf(stderr, "Video state: {\n"
        "\"ready\": %d,\n"
        "\"error\": \"%s\",\n"
        "\"width\": %d,\n"
        "\"height\": %d,\n"
        "\"frame_per_second\": %f\n"
    "}\n", state->ready, state->error, state->width, state->height, state->frame_per_second);
}

void jetkvm_indev_handler(int code) {
    fprintf(stderr, "Video indev: %d\n", code);
}

void jetkvm_rpc_handler(const char *method, const char *params) {
    fprintf(stderr, "Video rpc: %s %s\n", method, params);
}

// Note: jetkvm_set_video_handler, jetkvm_set_indev_handler, jetkvm_set_rpc_handler,
// jetkvm_call_rpc_handler, and jetkvm_set_video_state_handler are implemented in
// the library (ctrl.c) and will be used from there when linking.

int main(int argc, char *argv[]) {
    const char *socket_path = SOCKET_PATH;
    
    // Allow custom socket path via command line argument
    if (argc > 1) {
        socket_path = argv[1];
    }
    
    // Remove existing socket file if it exists
    unlink(socket_path);

    // Set handlers
    jetkvm_set_log_handler(&jetkvm_c_log_handler);
    jetkvm_set_video_handler(&jetkvm_video_handler);
    jetkvm_set_video_state_handler(&jetkvm_video_state_handler);
    jetkvm_set_indev_handler(&jetkvm_indev_handler);
    jetkvm_set_rpc_handler(&jetkvm_rpc_handler);
    
    // Initialize video first (before accepting connections)
    fprintf(stderr, "Initializing video...\n");
    if (jetkvm_video_init(1.0) != 0) {
        fprintf(stderr, "Failed to initialize video\n");
        return 1;
    }
    
    // Start video streaming - frames will be sent via video_send_frame
    // which calls the video handler we set up
    jetkvm_video_start();
    fprintf(stderr, "Video streaming started.\n");

    // Create Unix domain socket
    int server_fd = socket(AF_UNIX, SOCK_STREAM, 0);
    if (server_fd < 0) {
        perror("socket");
        jetkvm_video_stop();
        jetkvm_video_shutdown();
        return 1;
    }
    
    // Make socket non-blocking
    int flags = fcntl(server_fd, F_GETFL, 0);
    if (flags < 0 || fcntl(server_fd, F_SETFL, flags | O_NONBLOCK) < 0) {
        perror("fcntl");
        close(server_fd);
        jetkvm_video_stop();
        jetkvm_video_shutdown();
        return 1;
    }
    
    // Bind socket to path
    struct sockaddr_un addr;
    memset(&addr, 0, sizeof(addr));
    addr.sun_family = AF_UNIX;
    strncpy(addr.sun_path, socket_path, sizeof(addr.sun_path) - 1);
    
    if (bind(server_fd, (struct sockaddr *)&addr, sizeof(addr)) < 0) {
        perror("bind");
        close(server_fd);
        jetkvm_video_stop();
        jetkvm_video_shutdown();
        return 1;
    }
    
    // Listen for connections
    if (listen(server_fd, 1) < 0) {
        perror("listen");
        close(server_fd);
        jetkvm_video_stop();
        jetkvm_video_shutdown();
        return 1;
    }
    
    fprintf(stderr, "Listening on Unix socket: %s (non-blocking)\n", socket_path);
    fprintf(stderr, "Video frames will be sent to connected clients...\n");
    
    // Main loop: check for new connections and handle client disconnections
    fd_set read_fds;
    struct timeval timeout;
    
    while (1) {
        FD_ZERO(&read_fds);
        FD_SET(server_fd, &read_fds);
        
        pthread_mutex_lock(&client_fd_mutex);
        int current_client_fd = client_fd;
        if (current_client_fd >= 0) {
            FD_SET(current_client_fd, &read_fds);
        }
        int max_fd = (current_client_fd > server_fd) ? current_client_fd : server_fd;
        pthread_mutex_unlock(&client_fd_mutex);
        
        timeout.tv_sec = 1;
        timeout.tv_usec = 0;
        
        int result = select(max_fd + 1, &read_fds, NULL, NULL, &timeout);
        if (result < 0) {
            if (errno == EINTR) {
                continue;
            }
            perror("select");
            break;
        }
        
        // Check for new connection
        if (FD_ISSET(server_fd, &read_fds)) {
            int accepted_fd = accept(server_fd, NULL, NULL);
            if (accepted_fd >= 0) {
                fprintf(stderr, "Client connected\n");
                pthread_mutex_lock(&client_fd_mutex);
                if (client_fd >= 0) {
                    // Close previous client if any
                    close(client_fd);
                }
                client_fd = accepted_fd;
                pthread_mutex_unlock(&client_fd_mutex);
            } else if (errno != EAGAIN && errno != EWOULDBLOCK) {
                perror("accept");
            }
        }
        
        // Check if client disconnected
        pthread_mutex_lock(&client_fd_mutex);
        current_client_fd = client_fd;
        pthread_mutex_unlock(&client_fd_mutex);
        
        if (current_client_fd >= 0 && FD_ISSET(current_client_fd, &read_fds)) {
            // Client sent data or closed connection
            char buffer[1];
            if (read(current_client_fd, buffer, 1) <= 0) {
                fprintf(stderr, "Client disconnected\n");
                pthread_mutex_lock(&client_fd_mutex);
                close(client_fd);
                client_fd = -1;
                pthread_mutex_unlock(&client_fd_mutex);
            }
        }
    }
    
    // Stop video streaming
    jetkvm_video_stop();
    jetkvm_video_shutdown();
    
    // Cleanup
    pthread_mutex_lock(&client_fd_mutex);
    if (client_fd >= 0) {
        close(client_fd);
        client_fd = -1;
    }
    pthread_mutex_unlock(&client_fd_mutex);
    
    close(server_fd);
    unlink(socket_path);
    
    return 0;
}

