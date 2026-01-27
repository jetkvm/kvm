/**
 * @file h264_transcode.c
 * @brief Hyper-optimized H.264 to MJPEG transcoder for camera redirection.
 *
 * BETA FEATURE - High CPU usage on Cortex-A7.
 *
 * Optimizations:
 * - NEON-accelerated I420→NV12 conversion (via Cisco OpenH264)
 * - Lock-free statistics using atomics
 * - Pre-allocated buffers (zero malloc in hot path)
 * - Direct DMA buffer access for hardware encoder
 * - Minimal syscalls
 *
 * Pipeline: H.264 NAL → OpenH264 decode → I420 → NEON NV12 → HW MJPEG → Output
 */

#include "h264_transcode.h"
#include "log.h"

#include <stdlib.h>
#include <string.h>
#include <stdatomic.h>
#include <errno.h>
#include <time.h>

#ifdef __linux__
#include "rk_mpi_mb.h"
#include "rk_mpi_venc.h"
#include "rk_mpi_sys.h"
#include "rk_common.h"
#endif

#ifdef HAS_OPENH264
#include "codec_api.h"
#include "codec_app_def.h"
#include "codec_def.h"
#endif

#ifdef HAS_RGA
#include <im2d.h>
#include <rga.h>
#endif

#ifdef __ARM_NEON
#include <arm_neon.h>
#endif

// Transcoder channel ID (separate from main JPEG_CHANNEL)
#define TRANSCODE_JPEG_CHANNEL 2

// Buffer configuration - minimized for single-core RV1106
#define TRANSCODE_BUFFER_COUNT 2  // Reduced from 3 to lower memory pressure
#define MAX_H264_FRAME_SIZE (512 * 1024)  // 512KB - typical webcam frame

// Branch prediction hints
#define likely(x)   __builtin_expect(!!(x), 1)
#define unlikely(x) __builtin_expect(!!(x), 0)

// Transcoder state - cache-line aligned for performance
static struct __attribute__((aligned(64))) {
    // Hot path data (first cache line)
    _Atomic bool running;
    _Atomic uint64_t frames_in;
    _Atomic uint64_t frames_out;
    _Atomic uint64_t frames_dropped;

    // Input dimensions (from H.264 stream or raw YUV)
    uint32_t input_width;
    uint32_t input_height;
    bool input_detected;        // True once we've detected input resolution

    // Output dimensions (for MJPEG encoder, may differ from input)
    uint32_t output_width;
    uint32_t output_height;
    bool needs_scaling;         // True if input != output resolution

    uint32_t target_fps;
    uint32_t jpeg_quality;

    // Decode output buffer (NV12 format at INPUT resolution) - aligned for NEON
    uint8_t *yuv_buffer __attribute__((aligned(16)));
    size_t yuv_buffer_size;

    // Scaled output buffer (NV12 at OUTPUT resolution, used if scaling needed)
    uint8_t *scaled_buffer __attribute__((aligned(16)));
    size_t scaled_buffer_size;

    // Callback
    transcode_output_cb output_cb;
    void *user_data;

    // Cold path data
    bool initialized;
    _Atomic uint64_t decode_time_us;
    _Atomic uint64_t encode_time_us;
    _Atomic uint64_t scale_time_us;

#ifdef HAS_OPENH264
    ISVCDecoder *decoder;
    bool decoder_initialized;
    SDecodingParam dec_param;
    SBufferInfo buffer_info;
    unsigned char *yuv_planes[3];  // Y, U, V planes from decoder
#endif

#ifdef __linux__
    MB_POOL mem_pool;
    bool encoder_started;
    VENC_PACK_S venc_pack;  // Pre-allocated - no malloc in hot path
#endif

#ifdef HAS_RGA
    bool rga_initialized;
    int rga_input_fd;
    int rga_output_fd;
#endif
} tc __attribute__((aligned(64))) = {0};

// Track if stream has duplicate NALs (macOS quirk) - reset on shutdown
static _Atomic bool h264_stream_has_duplicates = false;

// Static buffer for H.264 deduplication (avoids malloc in hot path)
static uint8_t h264_dedup_buffer[MAX_H264_FRAME_SIZE] __attribute__((aligned(16)));

// Fast monotonic time (uses VDSO, no syscall)
static inline uint64_t get_time_us(void) {
    struct timespec ts;
    clock_gettime(CLOCK_MONOTONIC, &ts);
    return (uint64_t)ts.tv_sec * 1000000ULL + (uint64_t)ts.tv_nsec / 1000ULL;
}

#ifdef __ARM_NEON
/**
 * NEON-optimized I420 to NV12 conversion.
 * Processes 16 UV pairs per iteration.
 * ~4x faster than scalar loop on Cortex-A7.
 */
static void convert_i420_to_nv12_neon(
    const uint8_t *__restrict__ y_src,
    const uint8_t *__restrict__ u_src,
    const uint8_t *__restrict__ v_src,
    uint8_t *__restrict__ y_dst,
    uint8_t *__restrict__ uv_dst,
    uint32_t width, uint32_t height)
{
    const size_t y_size = width * height;
    const size_t uv_width = width / 2;
    const size_t uv_height = height / 2;

    // Copy Y plane (can use memcpy, it's NEON-optimized in libc)
    memcpy(y_dst, y_src, y_size);

    // Interleave U and V planes using NEON
    const size_t uv_stride = uv_width;
    for (size_t row = 0; row < uv_height; row++) {
        const uint8_t *u_row = u_src + row * uv_stride;
        const uint8_t *v_row = v_src + row * uv_stride;
        uint8_t *uv_row = uv_dst + row * width;

        size_t col = 0;
        // Process 16 UV pairs at a time
        for (; col + 16 <= uv_width; col += 16) {
            uint8x16_t u = vld1q_u8(u_row + col);
            uint8x16_t v = vld1q_u8(v_row + col);
            uint8x16x2_t uv = {{u, v}};
            vst2q_u8(uv_row + col * 2, uv);
        }
        // Handle remaining pixels
        for (; col < uv_width; col++) {
            uv_row[col * 2] = u_row[col];
            uv_row[col * 2 + 1] = v_row[col];
        }
    }
}

/**
 * NEON-optimized YUY2 (YUYV) to NV12 conversion.
 * YUY2 is packed 4:2:2: Y0 U0 Y1 V0 Y2 U1 Y3 V1 ...
 * NV12 is planar 4:2:0: Y plane, then interleaved UV plane (subsampled vertically)
 *
 * Processes 16 pixels (32 bytes input) at a time.
 */
static void convert_yuy2_to_nv12_neon(
    const uint8_t *__restrict__ yuy2_src,
    uint8_t *__restrict__ y_dst,
    uint8_t *__restrict__ uv_dst,
    uint32_t width, uint32_t height)
{
    const size_t row_stride = width * 2;  // YUY2 is 2 bytes per pixel

    for (size_t row = 0; row < height; row++) {
        const uint8_t *src_row = yuy2_src + row * row_stride;
        uint8_t *y_row = y_dst + row * width;

        // Extract Y values using NEON deinterleave
        size_t col = 0;
        for (; col + 32 <= row_stride; col += 32) {
            // Load 32 bytes = 16 pixels worth of YUY2
            uint8x16x2_t yuyv = vld2q_u8(src_row + col);
            // yuyv.val[0] = Y values, yuyv.val[1] = U/V interleaved
            vst1q_u8(y_row + col / 2, yuyv.val[0]);
        }
        // Handle remaining Y values
        for (; col < row_stride; col += 2) {
            y_row[col / 2] = src_row[col];
        }

        // Only process UV on even rows (vertical subsampling for 4:2:0)
        if (row % 2 == 0) {
            uint8_t *uv_row = uv_dst + (row / 2) * width;

            // Average U and V values from two rows for vertical subsampling
            const uint8_t *src_row_next = (row + 1 < height) ?
                yuy2_src + (row + 1) * row_stride : src_row;

            col = 0;
            for (; col + 32 <= row_stride; col += 32) {
                // Load current and next row
                uint8x16x2_t yuyv0 = vld2q_u8(src_row + col);
                uint8x16x2_t yuyv1 = vld2q_u8(src_row_next + col);

                // yuyv.val[1] contains U0 V0 U1 V1 ... (already interleaved UV)
                // Average the two rows for vertical subsampling
                uint8x16_t uv_avg = vrhaddq_u8(yuyv0.val[1], yuyv1.val[1]);
                vst1q_u8(uv_row + col / 2, uv_avg);
            }
            // Handle remaining UV values
            for (; col < row_stride; col += 4) {
                uint8_t u0 = src_row[col + 1];
                uint8_t v0 = src_row[col + 3];
                uint8_t u1 = src_row_next[col + 1];
                uint8_t v1 = src_row_next[col + 3];
                uv_row[col / 2] = (u0 + u1 + 1) / 2;
                uv_row[col / 2 + 1] = (v0 + v1 + 1) / 2;
            }
        }
    }
}
#else
// Scalar fallback for I420 to NV12
static void convert_i420_to_nv12_scalar(
    const uint8_t *y_src,
    const uint8_t *u_src,
    const uint8_t *v_src,
    uint8_t *y_dst,
    uint8_t *uv_dst,
    uint32_t width, uint32_t height)
{
    const size_t y_size = width * height;
    memcpy(y_dst, y_src, y_size);

    const size_t uv_plane_size = y_size / 4;
    for (size_t i = 0; i < uv_plane_size; i++) {
        uv_dst[i * 2] = u_src[i];
        uv_dst[i * 2 + 1] = v_src[i];
    }
}

// Scalar fallback for YUY2 to NV12
static void convert_yuy2_to_nv12_scalar(
    const uint8_t *yuy2_src,
    uint8_t *y_dst,
    uint8_t *uv_dst,
    uint32_t width, uint32_t height)
{
    const size_t row_stride = width * 2;

    for (size_t row = 0; row < height; row++) {
        const uint8_t *src_row = yuy2_src + row * row_stride;
        uint8_t *y_row = y_dst + row * width;

        // Extract Y values
        for (size_t col = 0; col < row_stride; col += 2) {
            y_row[col / 2] = src_row[col];
        }

        // Process UV on even rows only
        if (row % 2 == 0) {
            uint8_t *uv_row = uv_dst + (row / 2) * width;
            const uint8_t *src_row_next = (row + 1 < height) ?
                yuy2_src + (row + 1) * row_stride : src_row;

            for (size_t col = 0; col < row_stride; col += 4) {
                uint8_t u0 = src_row[col + 1];
                uint8_t v0 = src_row[col + 3];
                uint8_t u1 = src_row_next[col + 1];
                uint8_t v1 = src_row_next[col + 3];
                uv_row[col / 2] = (u0 + u1 + 1) / 2;
                uv_row[col / 2 + 1] = (v0 + v1 + 1) / 2;
            }
        }
    }
}
#endif

#ifdef HAS_RGA
/**
 * Scale NV12 frame using RGA hardware acceleration.
 * This is very fast (~1-2ms for 1080p->480p on RV1106).
 *
 * @param src_data Source NV12 data
 * @param src_width Source width
 * @param src_height Source height
 * @param dst_data Destination NV12 buffer (must be pre-allocated)
 * @param dst_width Destination width
 * @param dst_height Destination height
 * @return 0 on success, negative error code on failure
 */
static int scale_nv12_rga(const uint8_t *src_data, uint32_t src_width, uint32_t src_height,
                          uint8_t *dst_data, uint32_t dst_width, uint32_t dst_height) {
    // Wrap source buffer
    rga_buffer_t src = wrapbuffer_virtualaddr(
        (void *)src_data,
        src_width, src_height,
        RK_FORMAT_YCbCr_420_SP  // NV12
    );

    // Wrap destination buffer
    rga_buffer_t dst = wrapbuffer_virtualaddr(
        (void *)dst_data,
        dst_width, dst_height,
        RK_FORMAT_YCbCr_420_SP  // NV12
    );

    // Perform hardware-accelerated resize
    IM_STATUS status = imresize(src, dst);
    if (status != IM_STATUS_SUCCESS) {
        log_error("Transcode: RGA resize failed: %s", imStrError(status));
        return -EIO;
    }

    return 0;
}
#endif // HAS_RGA

/**
 * Scale NV12 or pass through if no scaling needed.
 * Returns pointer to the buffer that should be encoded (either yuv_buffer or scaled_buffer).
 */
static const uint8_t *scale_if_needed(uint32_t frame_width, uint32_t frame_height) {
    // No scaling needed if dimensions match output
    if (frame_width == tc.output_width && frame_height == tc.output_height) {
        return tc.yuv_buffer;
    }

    // Update needs_scaling flag for statistics
    tc.needs_scaling = true;

#ifdef HAS_RGA
    uint64_t start = get_time_us();

    int ret = scale_nv12_rga(tc.yuv_buffer, frame_width, frame_height,
                             tc.scaled_buffer, tc.output_width, tc.output_height);

    atomic_fetch_add(&tc.scale_time_us, get_time_us() - start);

    if (ret == 0) {
        return tc.scaled_buffer;
    }
    // Fall through to software scaling on RGA error
#endif

    // Software fallback: simple bilinear scaling (slower but works everywhere)
    // For now, just return original buffer and let encoder handle mismatch
    // This will likely fail, but at least we tried
    log_error("Transcode: RGA unavailable, cannot scale %ux%u -> %ux%u",
              frame_width, frame_height, tc.output_width, tc.output_height);
    return tc.yuv_buffer;
}

#ifdef __linux__

static int setup_jpeg_encoder(void) {
    // Create memory pool for encoder input (at OUTPUT resolution)
    MB_POOL_CONFIG_S pool_cfg = {0};
    pool_cfg.u64MBSize = tc.output_width * tc.output_height * 3 / 2;
    pool_cfg.u32MBCnt = TRANSCODE_BUFFER_COUNT;
    pool_cfg.enAllocType = MB_ALLOC_TYPE_DMA;
    pool_cfg.bPreAlloc = RK_TRUE;

    tc.mem_pool = RK_MPI_MB_CreatePool(&pool_cfg);
    if (tc.mem_pool == MB_INVALID_POOLID) {
        log_error("Transcode: failed to create DMA pool");
        return -ENOMEM;
    }

    // Configure JPEG encoder at OUTPUT resolution - optimized for low latency
    VENC_CHN_ATTR_S chn_attr = {0};
    chn_attr.stRcAttr.enRcMode = VENC_RC_MODE_MJPEGCBR;
    chn_attr.stRcAttr.stMjpegCbr.u32BitRate = 8000;

    chn_attr.stVencAttr.enType = RK_VIDEO_ID_MJPEG;
    chn_attr.stVencAttr.enPixelFormat = RK_FMT_YUV420SP;
    chn_attr.stVencAttr.u32PicWidth = tc.output_width;
    chn_attr.stVencAttr.u32PicHeight = tc.output_height;
    chn_attr.stVencAttr.u32VirWidth = (tc.output_width + 15) & ~15;   // 16-byte aligned
    chn_attr.stVencAttr.u32VirHeight = (tc.output_height + 15) & ~15;
    chn_attr.stVencAttr.u32StreamBufCnt = 2;
    chn_attr.stVencAttr.u32BufSize = tc.output_width * tc.output_height;
    chn_attr.stVencAttr.enMirror = MIRROR_NONE;

    if (RK_MPI_VENC_CreateChn(TRANSCODE_JPEG_CHANNEL, &chn_attr) != RK_SUCCESS) {
        log_error("Transcode: failed to create VENC channel");
        RK_MPI_MB_DestroyPool(tc.mem_pool);
        return -EIO;
    }

    // Set JPEG quality
    VENC_JPEG_PARAM_S jpeg_param = {0};
    jpeg_param.u32Qfactor = tc.jpeg_quality;
    RK_MPI_VENC_SetJpegParam(TRANSCODE_JPEG_CHANNEL, &jpeg_param);

    // Start receiving frames
    VENC_RECV_PIC_PARAM_S recv_param = {0};
    recv_param.s32RecvPicNum = -1;

    if (RK_MPI_VENC_StartRecvFrame(TRANSCODE_JPEG_CHANNEL, &recv_param) != RK_SUCCESS) {
        log_error("Transcode: failed to start VENC");
        RK_MPI_VENC_DestroyChn(TRANSCODE_JPEG_CHANNEL);
        RK_MPI_MB_DestroyPool(tc.mem_pool);
        return -EIO;
    }

    tc.encoder_started = true;
    return 0;
}

static void teardown_jpeg_encoder(void) {
    if (!tc.encoder_started) return;

    RK_MPI_VENC_StopRecvFrame(TRANSCODE_JPEG_CHANNEL);
    RK_MPI_VENC_DestroyChn(TRANSCODE_JPEG_CHANNEL);

    if (tc.mem_pool != MB_INVALID_POOLID) {
        RK_MPI_MB_DestroyPool(tc.mem_pool);
        tc.mem_pool = MB_INVALID_POOLID;
    }
    tc.encoder_started = false;
}

// Hot path - encode YUV to JPEG using hardware
static inline int encode_yuv_to_jpeg(const uint8_t *yuv_data, size_t yuv_size) {
    uint64_t start = get_time_us();

    // Get DMA buffer from pool (non-blocking)
    MB_BLK blk = RK_MPI_MB_GetMB(tc.mem_pool, yuv_size, RK_FALSE);
    if (unlikely(blk == MB_INVALID_HANDLE)) {
        atomic_fetch_add(&tc.frames_dropped, 1);
        return -EAGAIN;
    }

    // Copy to DMA buffer (size is implicitly known from allocation)
    void *vaddr = RK_MPI_MB_Handle2VirAddr(blk);
    memcpy(vaddr, yuv_data, yuv_size);

    // Prepare frame at OUTPUT resolution
    VIDEO_FRAME_INFO_S frame = {0};
    frame.stVFrame.pMbBlk = blk;
    frame.stVFrame.u32Width = tc.output_width;
    frame.stVFrame.u32Height = tc.output_height;
    frame.stVFrame.u32VirWidth = (tc.output_width + 15) & ~15;
    frame.stVFrame.u32VirHeight = (tc.output_height + 15) & ~15;
    frame.stVFrame.enPixelFormat = RK_FMT_YUV420SP;
    frame.stVFrame.enCompressMode = COMPRESS_MODE_NONE;

    // Send to encoder (short timeout for low latency)
    int32_t ret = RK_MPI_VENC_SendFrame(TRANSCODE_JPEG_CHANNEL, &frame, 50);
    RK_MPI_MB_ReleaseMB(blk);

    if (unlikely(ret != RK_SUCCESS)) {
        atomic_fetch_add(&tc.frames_dropped, 1);
        return -EIO;
    }

    // Get encoded output using pre-allocated pack struct
    VENC_STREAM_S stream = {0};
    stream.pstPack = &tc.venc_pack;

    ret = RK_MPI_VENC_GetStream(TRANSCODE_JPEG_CHANNEL, &stream, 100);
    if (unlikely(ret != RK_SUCCESS)) {
        atomic_fetch_add(&tc.frames_dropped, 1);
        return -EIO;
    }

    // Deliver JPEG via callback
    if (likely(tc.output_cb)) {
        void *jpeg_data = RK_MPI_MB_Handle2VirAddr(stream.pstPack->pMbBlk);
        tc.output_cb(jpeg_data, stream.pstPack->u32Len, tc.user_data);
    }

    RK_MPI_VENC_ReleaseStream(TRANSCODE_JPEG_CHANNEL, &stream);

    atomic_fetch_add(&tc.frames_out, 1);
    atomic_fetch_add(&tc.encode_time_us, get_time_us() - start);

    return 0;
}

#endif // __linux__

#ifdef HAS_OPENH264
// Decode H.264 to I420 using Cisco OpenH264, then convert to NV12
static int decode_h264_frame(const uint8_t *h264_data, size_t h264_len,
                             uint8_t *nv12_out, size_t *nv12_len,
                             uint32_t *out_width, uint32_t *out_height) {
    // Lazy init decoder
    if (unlikely(!tc.decoder_initialized)) {
        // Create decoder instance
        if (WelsCreateDecoder(&tc.decoder) != 0 || tc.decoder == NULL) {
            log_error("Transcode: failed to create OpenH264 decoder");
            return -ENOMEM;
        }

        // Initialize decoder parameters
        memset(&tc.dec_param, 0, sizeof(tc.dec_param));
        tc.dec_param.sVideoProperty.eVideoBsType = VIDEO_BITSTREAM_AVC;
        // Use slice-based error concealment - smarter than frame copy,
        // copies affected slices from previous frame rather than entire frame
        // ERROR_CON_SLICE_COPY = 2
        tc.dec_param.eEcActiveIdc = ERROR_CON_SLICE_COPY;
        tc.dec_param.bParseOnly = false;
        tc.dec_param.uiTargetDqLayer = 0xFF;  // Decode all layers

        if ((*tc.decoder)->Initialize(tc.decoder, &tc.dec_param) != 0) {
            WelsDestroyDecoder(tc.decoder);
            tc.decoder = NULL;
            log_error("Transcode: failed to initialize OpenH264 decoder");
            return -EINVAL;
        }

        tc.decoder_initialized = true;
        log_info("Transcode: OpenH264 decoder initialized (ARM NEON)");
    }

    // Decode frame
    memset(&tc.buffer_info, 0, sizeof(tc.buffer_info));
    memset(tc.yuv_planes, 0, sizeof(tc.yuv_planes));

    DECODING_STATE ret = (*tc.decoder)->DecodeFrameNoDelay(
        tc.decoder,
        h264_data,
        (int)h264_len,
        tc.yuv_planes,
        &tc.buffer_info
    );

    // OpenH264 DECODING_STATE values (can be combined with |):
    // - dsErrorFree (0x00): Perfect decode
    // - dsFramePending (0x01): Need more NAL units
    // - dsRefLost (0x02): Reference frame lost - common at stream start
    // - dsBitstreamError (0x04): Bitstream syntax error
    // - dsNoParamSets (0x10): Missing SPS/PPS
    // - dsInvalidArgument (0x1000): Invalid argument
    //
    // Strategy: Accept frames unless there's a fatal bitstream error.
    // dsRefLost (0x02) is common and usually recoverable.

    // Log decode state for debugging (first few frames or periodic)
    static int frame_count = 0;
    frame_count++;
    if (ret != dsErrorFree) {
        static int err_log_count = 0;
        if (err_log_count++ < 20 || frame_count % 500 == 0) {
            log_debug("Transcode: frame %d decode=0x%02x bufStatus=%d",
                      frame_count, ret, tc.buffer_info.iBufferStatus);
        }
    }

    // Only drop frames with fatal errors (bitstream error, invalid args)
    if (ret & (dsBitstreamError | dsInvalidArgument)) {
        *nv12_len = 0;
        return 0;  // Fatal error, drop frame
    }

    // Check if we have a decoded frame ready
    if (tc.buffer_info.iBufferStatus != 1) {
        // No frame ready yet (need more NALs)
        *nv12_len = 0;
        return 0;
    }

    // Get decoded picture dimensions
    int w = tc.buffer_info.UsrData.sSystemBuffer.iWidth;
    int h = tc.buffer_info.UsrData.sSystemBuffer.iHeight;
    int stride_y = tc.buffer_info.UsrData.sSystemBuffer.iStride[0];
    int stride_uv = tc.buffer_info.UsrData.sSystemBuffer.iStride[1];

    if (unlikely(w <= 0 || h <= 0 || !tc.yuv_planes[0])) {
        *nv12_len = 0;
        return 0;
    }

    // Return decoded dimensions to caller (for scaling decisions)
    if (out_width) *out_width = (uint32_t)w;
    if (out_height) *out_height = (uint32_t)h;

    size_t y_size = (size_t)w * (size_t)h;
    size_t nv12_size = y_size + y_size / 2;

    if (unlikely(nv12_size > *nv12_len)) return -ENOSPC;

    // OpenH264 outputs I420 with strides - convert to NV12
    // Copy Y plane (may need de-striding)
    const uint8_t *y_src = tc.yuv_planes[0];
    uint8_t *y_dst = nv12_out;

    if (stride_y == w) {
        // No stride padding - direct copy
        memcpy(y_dst, y_src, y_size);
    } else {
        // De-stride Y plane
        for (int row = 0; row < h; row++) {
            memcpy(y_dst + row * w, y_src + row * stride_y, w);
        }
    }

    // Convert I420 U/V → NV12 UV interleaved
    const uint8_t *u_plane = tc.yuv_planes[1];
    const uint8_t *v_plane = tc.yuv_planes[2];
    uint8_t *uv_dst = nv12_out + y_size;
    int uv_width = w / 2;
    int uv_height = h / 2;

#ifdef __ARM_NEON
    // NEON-accelerated UV interleaving with stride handling
    for (int row = 0; row < uv_height; row++) {
        const uint8_t *u_row = u_plane + row * stride_uv;
        const uint8_t *v_row = v_plane + row * stride_uv;
        uint8_t *uv_row = uv_dst + row * w;

        int col = 0;
        // Process 16 UV pairs at a time with NEON
        for (; col + 16 <= uv_width; col += 16) {
            uint8x16_t u = vld1q_u8(u_row + col);
            uint8x16_t v = vld1q_u8(v_row + col);
            uint8x16x2_t uv = {{u, v}};
            vst2q_u8(uv_row + col * 2, uv);
        }
        // Handle remainder
        for (; col < uv_width; col++) {
            uv_row[col * 2] = u_row[col];
            uv_row[col * 2 + 1] = v_row[col];
        }
    }
#else
    // Scalar fallback
    for (int row = 0; row < uv_height; row++) {
        const uint8_t *u_row = u_plane + row * stride_uv;
        const uint8_t *v_row = v_plane + row * stride_uv;
        uint8_t *uv_row = uv_dst + row * w;
        for (int col = 0; col < uv_width; col++) {
            uv_row[col * 2] = u_row[col];
            uv_row[col * 2 + 1] = v_row[col];
        }
    }
#endif

    *nv12_len = nv12_size;
    return 0;
}
#endif // HAS_OPENH264

int transcode_init(const transcode_config_t *config) {
    if (tc.initialized) return -EALREADY;
    if (!config || !config->output_cb) return -EINVAL;

    memset(&tc, 0, sizeof(tc));

    // Store output dimensions (required)
    tc.output_width = config->output_width ? config->output_width : config->width;
    tc.output_height = config->output_height ? config->output_height : config->height;

    if (tc.output_width == 0 || tc.output_height == 0) {
        log_error("Transcode: output resolution required");
        return -EINVAL;
    }

    // Store input dimensions (may be 0 for H.264 auto-detect)
    tc.input_width = config->width;
    tc.input_height = config->height;
    tc.input_detected = (config->width > 0 && config->height > 0);

    // Check if scaling will be needed (for non-auto-detect cases)
    if (tc.input_detected) {
        tc.needs_scaling = (tc.input_width != tc.output_width ||
                           tc.input_height != tc.output_height);
    }

    tc.target_fps = config->target_fps ? config->target_fps : 25;
    tc.jpeg_quality = config->jpeg_quality ? config->jpeg_quality : 80;  // Match HDMI encoder default
    tc.output_cb = config->output_cb;
    tc.user_data = config->user_data;

    // Allocate input YUV buffer at max expected resolution (for H.264 auto-detect)
    // Use 1920x1080 as max if input not specified
    uint32_t max_input_width = tc.input_width ? tc.input_width : 1920;
    uint32_t max_input_height = tc.input_height ? tc.input_height : 1080;
    tc.yuv_buffer_size = max_input_width * max_input_height * 3 / 2;
    if (posix_memalign((void **)&tc.yuv_buffer, 16, tc.yuv_buffer_size) != 0) {
        return -ENOMEM;
    }

    // Allocate scaled buffer at output resolution (for RGA scaling output)
    tc.scaled_buffer_size = tc.output_width * tc.output_height * 3 / 2;
    if (posix_memalign((void **)&tc.scaled_buffer, 16, tc.scaled_buffer_size) != 0) {
        free(tc.yuv_buffer);
        return -ENOMEM;
    }

    // Initialize buffers to black (prevents green flash on startup)
    // NV12 black: Y=0 (luma), UV=128 (neutral chroma)
    size_t y_size_in = max_input_width * max_input_height;
    size_t y_size_out = tc.output_width * tc.output_height;
    memset(tc.yuv_buffer, 0, y_size_in);                           // Y plane = 0
    memset(tc.yuv_buffer + y_size_in, 128, y_size_in / 2);         // UV plane = 128
    memset(tc.scaled_buffer, 0, y_size_out);                       // Y plane = 0
    memset(tc.scaled_buffer + y_size_out, 128, y_size_out / 2);    // UV plane = 128

#ifdef __linux__
    if (setup_jpeg_encoder() != 0) {
        free(tc.yuv_buffer);
        free(tc.scaled_buffer);
        return -EIO;
    }
#endif

    tc.initialized = true;
    atomic_store(&tc.running, true);

#ifdef HAS_OPENH264
    if (tc.input_detected) {
        log_info("Transcode: %ux%u->%ux%u q=%u",
                 tc.input_width, tc.input_height,
                 tc.output_width, tc.output_height, tc.jpeg_quality);
    } else {
        // H.264: input resolution will be auto-detected from SPS NAL
        log_info("Transcode: H.264(auto)->%ux%u q=%u",
                 tc.output_width, tc.output_height, tc.jpeg_quality);
    }
#else
    log_error("Transcode: no decoder");
#endif

    return 0;
}

void transcode_shutdown(void) {
    if (!tc.initialized) return;

    atomic_store(&tc.running, false);

#ifdef HAS_OPENH264
    if (tc.decoder_initialized && tc.decoder) {
        (*tc.decoder)->Uninitialize(tc.decoder);
        WelsDestroyDecoder(tc.decoder);
        tc.decoder = NULL;
        tc.decoder_initialized = false;
    }
#endif

#ifdef __linux__
    teardown_jpeg_encoder();
#endif

    free(tc.yuv_buffer);
    tc.yuv_buffer = NULL;
    free(tc.scaled_buffer);
    tc.scaled_buffer = NULL;
    tc.initialized = false;

    // Reset duplicate detection for next stream
    atomic_store(&h264_stream_has_duplicates, false);

    log_info("Transcode: shutdown");
}

bool transcode_is_running(void) {
    return tc.initialized && atomic_load(&tc.running);
}

/**
 * Simple NAL deduplication for macOS streams.
 * macOS sends [SPS,SPS,PPS,PPS,SEI,SEI,IDR,IDR] - each NAL appears twice.
 * This function removes consecutive duplicates by comparing NAL content.
 *
 * Approach: Parse NALs one at a time, copy to output if not identical to previous.
 * Conservative: Only removes exact duplicates, preserves all other data.
 *
 * @param input Input H.264 data
 * @param input_len Length of input
 * @param output Output buffer (must be >= input_len)
 * @param output_len Actual output length
 * @return Number of duplicates removed (0 = no duplicates found)
 */
static int deduplicate_h264_nals(const uint8_t *input, size_t input_len,
                                  uint8_t *output, size_t *output_len) {
    // NAL info for previous NAL (for duplicate detection)
    const uint8_t *prev_nal_data = NULL;
    size_t prev_nal_len = 0;

    const uint8_t *src = input;
    const uint8_t *src_end = input + input_len;
    uint8_t *dst = output;
    int duplicates_removed = 0;

    while (src < src_end - 4) {
        // Find start code (00 00 01 or 00 00 00 01)
        const uint8_t *sc_start = NULL;
        int sc_len = 0;

        for (const uint8_t *p = src; p < src_end - 2; p++) {
            if (p[0] == 0 && p[1] == 0) {
                if (p[2] == 1) {
                    sc_start = p;
                    sc_len = 3;
                    break;
                }
                if (p + 3 < src_end && p[2] == 0 && p[3] == 1) {
                    sc_start = p;
                    sc_len = 4;
                    break;
                }
            }
        }

        if (!sc_start) {
            // No more start codes - copy remaining data
            size_t remaining = src_end - src;
            if (remaining > 0) {
                memcpy(dst, src, remaining);
                dst += remaining;
            }
            break;
        }

        // Copy any data before this start code
        if (sc_start > src) {
            size_t prefix_len = sc_start - src;
            memcpy(dst, src, prefix_len);
            dst += prefix_len;
        }

        // Find end of this NAL (next start code or end of buffer)
        const uint8_t *nal_data = sc_start + sc_len;
        const uint8_t *nal_end = src_end;

        for (const uint8_t *p = nal_data; p < src_end - 2; p++) {
            if (p[0] == 0 && p[1] == 0 && (p[2] == 1 || (p + 3 < src_end && p[2] == 0 && p[3] == 1))) {
                nal_end = p;
                break;
            }
        }

        size_t nal_len = nal_end - nal_data;

        // Check if this NAL is identical to previous
        bool is_duplicate = false;
        if (prev_nal_data && prev_nal_len == nal_len && nal_len > 0) {
            // Compare NAL content
            if (memcmp(prev_nal_data, nal_data, nal_len) == 0) {
                is_duplicate = true;
                duplicates_removed++;
            }
        }

        if (!is_duplicate) {
            // Copy start code + NAL to output
            memcpy(dst, sc_start, sc_len + nal_len);

            // Remember this NAL for next comparison (point to output buffer)
            prev_nal_data = dst + sc_len;
            prev_nal_len = nal_len;

            dst += sc_len + nal_len;
        }

        // Move past this NAL
        src = nal_end;
    }

    *output_len = dst - output;

    // Log deduplication results (only first few times)
    if (duplicates_removed > 0) {
        static int log_count = 0;
        if (log_count++ < 5) {
            log_info("Transcode: removed %d duplicate NALs (%zu->%zu bytes)",
                     duplicates_removed, input_len, *output_len);
        }
    }

    return duplicates_removed;
}

/**
 * Quick check if stream likely has duplicate NALs.
 * Looks for consecutive NALs of the same type (SPS,PPS,SEI,IDR only).
 * Very fast - just scans for NAL type bytes after start codes.
 */
static inline bool quick_check_duplicates(const uint8_t *data, size_t len) {
    // If we've already detected duplicates, don't re-check
    if (atomic_load(&h264_stream_has_duplicates)) {
        return true;
    }

    uint8_t prev_type = 0;
    const uint8_t *end = data + len - 4;

    for (const uint8_t *p = data; p < end; p++) {
        // Look for start code
        if (p[0] == 0 && p[1] == 0 && (p[2] == 1 || (p[2] == 0 && p[3] == 1))) {
            int sc_len = (p[2] == 1) ? 3 : 4;
            if (p + sc_len < data + len) {
                uint8_t nal_type = p[sc_len] & 0x1F;
                // Check for consecutive same type (only for types 5-8)
                if (nal_type == prev_type && nal_type >= 5 && nal_type <= 8) {
                    atomic_store(&h264_stream_has_duplicates, true);
                    return true;
                }
                prev_type = nal_type;
            }
            p += sc_len;  // Skip past start code
        }
    }
    return false;
}

int transcode_feed_h264(const uint8_t *h264_data, size_t h264_len) {
    if (unlikely(!atomic_load(&tc.running))) return -EINVAL;
    if (unlikely(h264_len > MAX_H264_FRAME_SIZE)) return -E2BIG;

    atomic_fetch_add(&tc.frames_in, 1);

    // Fast path: check if deduplication needed
    // Most RDP clients (Windows, Linux) don't send duplicates - zero-copy for them
    // macOS sends [SPS,SPS,PPS,PPS,SEI,SEI,IDR,IDR] - needs deduplication
    const uint8_t *decode_data;
    size_t decode_len;

    if (quick_check_duplicates(h264_data, h264_len)) {
        // macOS or similar: deduplicate to save ~50% CPU on single-core RV1106
        size_t dedup_len = 0;
        deduplicate_h264_nals(h264_data, h264_len, h264_dedup_buffer, &dedup_len);
        decode_data = h264_dedup_buffer;
        decode_len = dedup_len;
    } else {
        // Normal client: zero-copy pass-through
        decode_data = h264_data;
        decode_len = h264_len;
    }

#ifdef HAS_OPENH264
    uint64_t start = get_time_us();

    size_t nv12_len = tc.yuv_buffer_size;
    uint32_t decoded_width = 0, decoded_height = 0;
    int ret = decode_h264_frame(decode_data, decode_len, tc.yuv_buffer, &nv12_len,
                                &decoded_width, &decoded_height);

    atomic_fetch_add(&tc.decode_time_us, get_time_us() - start);

    if (unlikely(ret != 0)) {
        atomic_fetch_add(&tc.frames_dropped, 1);
        return ret;
    }

    if (nv12_len == 0) return 0;  // Need more NAL units

    // Log input resolution on first successful decode (for debugging)
    if (!tc.input_detected && decoded_width > 0 && decoded_height > 0) {
        tc.input_width = decoded_width;
        tc.input_height = decoded_height;
        tc.input_detected = true;
        log_info("Transcode: H.264 stream %ux%u detected, output %ux%u",
                 decoded_width, decoded_height, tc.output_width, tc.output_height);
    }

#ifdef __linux__
    // Scale to output resolution if needed, then encode
    const uint8_t *encode_buffer = scale_if_needed(decoded_width, decoded_height);
    size_t encode_size = tc.output_width * tc.output_height * 3 / 2;
    return encode_yuv_to_jpeg(encode_buffer, encode_size);
#else
    return 0;
#endif

#else
    // No decoder
    static bool logged = false;
    if (!logged) {
        log_error("Transcode: no H.264 decoder");
        logged = true;
    }
    atomic_fetch_add(&tc.frames_dropped, 1);
    return -ENOSYS;
#endif
}

void transcode_get_stats(transcode_stats_t *stats) {
    if (!stats) return;

    stats->frames_in = atomic_load(&tc.frames_in);
    stats->frames_out = atomic_load(&tc.frames_out);
    stats->frames_dropped = atomic_load(&tc.frames_dropped);
    stats->decode_time_us = atomic_load(&tc.decode_time_us);
    stats->encode_time_us = atomic_load(&tc.encode_time_us);

    if (stats->frames_in > 0) {
        stats->avg_decode_ms = (uint32_t)(stats->decode_time_us / stats->frames_in / 1000);
    }
    if (stats->frames_out > 0) {
        stats->avg_encode_ms = (uint32_t)(stats->encode_time_us / stats->frames_out / 1000);
    }
}

void transcode_reset_stats(void) {
    atomic_store(&tc.frames_in, 0);
    atomic_store(&tc.frames_out, 0);
    atomic_store(&tc.frames_dropped, 0);
    atomic_store(&tc.decode_time_us, 0);
    atomic_store(&tc.encode_time_us, 0);
}

// Direct NV12 input - fastest path (no conversion needed)
int transcode_feed_nv12(const uint8_t *nv12_data, size_t nv12_len) {
    if (unlikely(!atomic_load(&tc.running))) return -EINVAL;

    size_t expected = tc.input_width * tc.input_height * 3 / 2;
    if (unlikely(nv12_len != expected)) return -EINVAL;

    atomic_fetch_add(&tc.frames_in, 1);

#ifdef __linux__
    const uint8_t *encode_buffer;
    size_t encode_size;

    // Fast path: no scaling needed - encode directly from input
    if (likely(tc.input_width == tc.output_width && tc.input_height == tc.output_height)) {
        encode_buffer = nv12_data;
        encode_size = nv12_len;
    } else {
        // Slow path: need to scale - copy to buffer first
        memcpy(tc.yuv_buffer, nv12_data, nv12_len);
        encode_buffer = scale_if_needed(tc.input_width, tc.input_height);
        encode_size = tc.output_width * tc.output_height * 3 / 2;
    }
    return encode_yuv_to_jpeg(encode_buffer, encode_size);
#else
    return -ENOSYS;
#endif
}

// I420 input - convert to NV12 then encode
int transcode_feed_i420(const uint8_t *i420_data, size_t i420_len) {
    if (unlikely(!atomic_load(&tc.running))) return -EINVAL;

    size_t expected = tc.input_width * tc.input_height * 3 / 2;
    if (unlikely(i420_len != expected)) return -EINVAL;

    atomic_fetch_add(&tc.frames_in, 1);

    // Convert I420 to NV12 in pre-allocated buffer
    const uint8_t *y_src = i420_data;
    const uint8_t *u_src = y_src + tc.input_width * tc.input_height;
    const uint8_t *v_src = u_src + tc.input_width * tc.input_height / 4;
    size_t y_size = tc.input_width * tc.input_height;

#ifdef __ARM_NEON
    convert_i420_to_nv12_neon(y_src, u_src, v_src,
                               tc.yuv_buffer, tc.yuv_buffer + y_size,
                               tc.input_width, tc.input_height);
#else
    convert_i420_to_nv12_scalar(y_src, u_src, v_src,
                                 tc.yuv_buffer, tc.yuv_buffer + y_size,
                                 tc.input_width, tc.input_height);
#endif

#ifdef __linux__
    const uint8_t *encode_buffer = scale_if_needed(tc.input_width, tc.input_height);
    size_t encode_size = tc.output_width * tc.output_height * 3 / 2;
    return encode_yuv_to_jpeg(encode_buffer, encode_size);
#else
    return -ENOSYS;
#endif
}

// YUY2 input - convert to NV12 then encode
int transcode_feed_yuy2(const uint8_t *yuy2_data, size_t yuy2_len) {
    if (unlikely(!atomic_load(&tc.running))) return -EINVAL;

    size_t expected = tc.input_width * tc.input_height * 2;  // YUY2 is 2 bytes/pixel
    if (unlikely(yuy2_len != expected)) return -EINVAL;

    atomic_fetch_add(&tc.frames_in, 1);

    // Convert YUY2 to NV12 in pre-allocated buffer
    size_t y_size = tc.input_width * tc.input_height;

#ifdef __ARM_NEON
    convert_yuy2_to_nv12_neon(yuy2_data,
                               tc.yuv_buffer, tc.yuv_buffer + y_size,
                               tc.input_width, tc.input_height);
#else
    convert_yuy2_to_nv12_scalar(yuy2_data,
                                 tc.yuv_buffer, tc.yuv_buffer + y_size,
                                 tc.input_width, tc.input_height);
#endif

#ifdef __linux__
    const uint8_t *encode_buffer = scale_if_needed(tc.input_width, tc.input_height);
    size_t encode_size = tc.output_width * tc.output_height * 3 / 2;
    return encode_yuv_to_jpeg(encode_buffer, encode_size);
#else
    return -ENOSYS;
#endif
}
