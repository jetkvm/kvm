/**
 * @file h264_transcode.h
 * @brief Hyper-optimized H.264 to MJPEG transcoder for camera redirection.
 *
 * BETA FEATURE: Software H.264 decode + Hardware MJPEG encode.
 * WARNING: High CPU usage (~80-100% on Cortex-A7 at 640x480@25fps).
 *
 * Architecture:
 *   H.264 NAL → OpenH264 decode (NEON) → NV12 → HW VENC MJPEG → Output
 *
 * Optimizations:
 *   - Zero-copy DMA buffers where possible
 *   - Pre-allocated buffer pools
 *   - Hardware MJPEG encoder via RK MPP
 *   - OpenH264 with ARM NEON optimizations
 *   - Frame skipping under CPU pressure
 */

#ifndef H264_TRANSCODE_H
#define H264_TRANSCODE_H

#include <stdbool.h>
#include <stdint.h>
#include <stddef.h>

#ifdef __cplusplus
extern "C" {
#endif

/**
 * @brief Callback for transcoded MJPEG frames.
 * @param jpeg_data Pointer to MJPEG data (valid only during callback)
 * @param jpeg_len Length of MJPEG data in bytes
 * @param user_data User-provided context pointer
 */
typedef void (*transcode_output_cb)(const uint8_t *jpeg_data, size_t jpeg_len, void *user_data);

/**
 * @brief Transcoder configuration.
 *
 * The transcoder supports resolution scaling via RGA hardware acceleration.
 * If output_width/output_height differ from input dimensions, RGA will scale
 * the frame before MJPEG encoding. This is useful when:
 * - RDP client sends high-res H.264 but USB host wants lower resolution
 * - Reducing MJPEG size for lower USB bandwidth
 *
 * For H.264 input, the input resolution is auto-detected from the stream.
 * For raw YUV input, input resolution must match the actual frame size.
 */
typedef struct {
    uint32_t width;           // Input width (0 = auto-detect for H.264)
    uint32_t height;          // Input height (0 = auto-detect for H.264)
    uint32_t output_width;    // Output width (0 = same as input)
    uint32_t output_height;   // Output height (0 = same as input)
    uint32_t target_fps;      // Target framerate (e.g., 25)
    uint32_t jpeg_quality;    // JPEG quality 1-99 (default: 70)
    bool     enable_skip;     // Enable frame skipping under CPU pressure
    transcode_output_cb output_cb;  // Callback for encoded frames
    void    *user_data;       // User context for callback
} transcode_config_t;

/**
 * @brief Initialize the transcoder.
 * @param config Transcoder configuration
 * @return 0 on success, negative error code on failure
 */
int transcode_init(const transcode_config_t *config);

/**
 * @brief Shutdown the transcoder and free resources.
 */
void transcode_shutdown(void);

/**
 * @brief Check if transcoder is initialized and running.
 * @return true if running, false otherwise
 */
bool transcode_is_running(void);

/**
 * @brief Feed an H.264 NAL unit or access unit to the transcoder.
 *
 * This function is designed for the hot path and minimizes allocations.
 * The data is copied internally, so the caller can reuse the buffer immediately.
 *
 * @param h264_data Pointer to H.264 NAL data (with or without start codes)
 * @param h264_len Length of H.264 data in bytes
 * @return 0 on success, negative error code on failure
 *         -EAGAIN if transcoder is busy (frame will be dropped)
 */
int transcode_feed_h264(const uint8_t *h264_data, size_t h264_len);

/**
 * @brief Feed raw NV12 data directly to the MJPEG encoder.
 *
 * This is the fastest path - no decoding or color conversion needed.
 * Data is copied to DMA buffer and sent to hardware encoder.
 *
 * @param nv12_data Pointer to NV12 data (Y plane followed by interleaved UV)
 * @param nv12_len Length of NV12 data (should be width * height * 3 / 2)
 * @return 0 on success, negative error code on failure
 */
int transcode_feed_nv12(const uint8_t *nv12_data, size_t nv12_len);

/**
 * @brief Feed raw I420 data to the transcoder.
 *
 * I420 is converted to NV12 using NEON acceleration, then hardware encoded.
 * Faster than H.264 path (no decode step).
 *
 * @param i420_data Pointer to I420 data (Y plane, U plane, V plane)
 * @param i420_len Length of I420 data (should be width * height * 3 / 2)
 * @return 0 on success, negative error code on failure
 */
int transcode_feed_i420(const uint8_t *i420_data, size_t i420_len);

/**
 * @brief Feed raw YUY2 (YUYV) data to the transcoder.
 *
 * YUY2 is converted to NV12 using NEON acceleration, then hardware encoded.
 * Faster than H.264 path (no decode step).
 *
 * @param yuy2_data Pointer to YUY2 packed data
 * @param yuy2_len Length of YUY2 data (should be width * height * 2)
 * @return 0 on success, negative error code on failure
 */
int transcode_feed_yuy2(const uint8_t *yuy2_data, size_t yuy2_len);

/**
 * @brief Get transcoder statistics.
 */
typedef struct {
    uint64_t frames_in;       // H.264 frames received
    uint64_t frames_out;      // MJPEG frames output
    uint64_t frames_dropped;  // Frames dropped due to backpressure
    uint64_t decode_time_us;  // Total decode time in microseconds
    uint64_t encode_time_us;  // Total encode time in microseconds
    uint32_t avg_decode_ms;   // Average decode time per frame (ms)
    uint32_t avg_encode_ms;   // Average encode time per frame (ms)
} transcode_stats_t;

/**
 * @brief Get current transcoder statistics.
 * @param stats Output statistics structure
 */
void transcode_get_stats(transcode_stats_t *stats);

/**
 * @brief Reset transcoder statistics.
 */
void transcode_reset_stats(void);

#ifdef __cplusplus
}
#endif

#endif // H264_TRANSCODE_H
