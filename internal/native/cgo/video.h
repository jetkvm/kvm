#ifndef VIDEO_DAEMON_VIDEO_H
#define VIDEO_DAEMON_VIDEO_H

/**
 * @brief Initialize the video subsystem
 *
 * @return int 0 on success, -1 on failure
 */
int video_init();

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

#endif //VIDEO_DAEMON_VIDEO_H
