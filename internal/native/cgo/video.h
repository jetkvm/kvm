#ifndef VIDEO_DAEMON_VIDEO_H
#define VIDEO_DAEMON_VIDEO_H

#include <stdbool.h>
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
 * @brief Start the JPEG encoder
 *
 * @param quality The JPEG quality (1-99)
 * @return int 0 on success, -1 on failure
 */
int jpeg_encoder_start(int quality);

/**
 * @brief Stop the JPEG encoder
 */
void jpeg_encoder_stop();

/**
 * @brief Set the JPEG encoder quality
 *
 * @param quality The JPEG quality (1-99)
 * @return int 0 on success, -1 on failure
 */
int jpeg_encoder_set_quality(int quality);

/**
 * @brief Get the JPEG encoder quality
 *
 * @return int The JPEG quality (1-99)
 */
int jpeg_encoder_get_quality();

/**
 * @brief Check if the JPEG encoder is running
 *
 * @return bool true if the JPEG encoder is running, false otherwise
 */
bool jpeg_encoder_is_running();

/**
 * @brief Request an IDR (keyframe) from the H.264 encoder
 *
 * This forces the encoder to produce an instant decoder refresh frame,
 * which is useful for new clients that need to start decoding from a keyframe.
 *
 * @return int 0 on success, -1 on failure
 */
int video_request_keyframe();

#endif //VIDEO_DAEMON_VIDEO_H
