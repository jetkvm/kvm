#ifndef VIDEO_DAEMON_VIDEO_H
#define VIDEO_DAEMON_VIDEO_H

#include <stddef.h>
#include <stdint.h>

/**
 * @brief Initialize the video subsystem
 *
 * @return int 0 on success, -1 on failure
 */
int video_init(float quality_factor);

/**
 * @brief Shutdown the video subsystem
 */
void video_shutdown();

/**
 * @brief Run the detect format thread
 *
 * @param arg The argument to pass to the thread
 * @return void* The result of the thread
 */
void *run_detect_format(void *arg);

/**
 * @brief Start the video streaming
 */
void video_start_streaming();

/**
 * @brief Stop the video streaming
 */
void video_stop_streaming();

/**
 * @brief Get the streaming status of the video
 *
 * @return uint8_t 1 if the video streaming is active, 2 if the video streaming is stopping, 0 otherwise
 */
uint8_t video_get_streaming_status();

/**
 * @brief Set the quality factor of the video
 *
 * @param factor The quality factor to set
 */
void video_set_quality_factor(float factor);

/**
 * @brief Get the quality factor of the video
 *
 * @return float The quality factor of the video
 */
float video_get_quality_factor();

/**
 * @brief Set the codec type (0 = H.264, 1 = H.265)
 */
void video_set_codec_type(int type);

/**
 * @brief Get the codec type (0 = H.264, 1 = H.265)
 */
int video_get_codec_type();

#define VIDEO_SNAPSHOT_ERR_NOT_STREAMING (-1) // no active video stream to snapshot
#define VIDEO_SNAPSHOT_ERR_TIMEOUT       (-2) // no frame captured within the deadline
#define VIDEO_SNAPSHOT_ERR_ENCODE        (-3) // JPEG encoder failed
#define VIDEO_SNAPSHOT_ERR_NOMEM         (-4) // failed to allocate the output buffer

/**
 * @brief Capture a single JPEG-encoded snapshot of the current video frame.
 *
 * Blocks until the next captured frame has been JPEG-encoded, or until an
 * internal deadline expires. On success, *out_buf is a malloc'd buffer of
 * *out_len bytes that the caller must release with video_free_snapshot().
 *
 * @return 0 on success, a negative VIDEO_SNAPSHOT_ERR_* code on failure
 */
int video_get_snapshot(uint8_t **out_buf, size_t *out_len);

/**
 * @brief Free a buffer returned by video_get_snapshot()
 */
void video_free_snapshot(uint8_t *buf);

#endif //VIDEO_DAEMON_VIDEO_H
