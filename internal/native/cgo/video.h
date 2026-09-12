#ifndef VIDEO_DAEMON_VIDEO_H
#define VIDEO_DAEMON_VIDEO_H

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

/**
 * @brief Capture a single JPEG frame from the live video stream.
 *
 * On success, *out_buf is set to a malloc'd buffer containing the JPEG bytes
 * and *out_len is set to its length. The caller is responsible for freeing
 * *out_buf with free().
 *
 * @param out_buf  Output pointer for the JPEG buffer (caller must free).
 * @param out_len  Output pointer for the buffer length.
 * @return 0 on success, non-zero on error (ENODEV = no signal, ENODATA = not
 *         streaming, EBUSY = capture already in progress, ETIMEDOUT = timeout).
 */
int video_capture_jpeg(uint8_t **out_buf, size_t *out_len);

#endif //VIDEO_DAEMON_VIDEO_H
