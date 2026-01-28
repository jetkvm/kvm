#define _POSIX_C_SOURCE 200809L
#include <unistd.h>
#include <time.h>
#include <rk_type.h>
#include <rk_mpi_venc.h>
#include <rk_mpi_sys.h>
#include <string.h>
#include <rk_debug.h>
#include <malloc.h>
#include <stdlib.h>
#include <stdbool.h>
#include <rk_mpi_mb.h>
#include <fcntl.h>
#include <linux/videodev2.h>
#include <sys/ioctl.h>
#include <errno.h>
#include <unistd.h>
#include <stdatomic.h>
#include <sys/socket.h>
#include <netinet/in.h>
#include <arpa/inet.h>
#include <rk_mpi_mmz.h>
#include <pthread.h>
#include <assert.h>
#include <sys/un.h>
#include <sys/socket.h>
#include "video.h"
#include "ctrl.h"
#include "log.h"

// RGA hardware 2D graphics acceleration for YUV→BGRX conversion
// Conditional compilation: HAS_RGA is defined by CMake if RGA headers are available
#ifdef HAS_RGA
#include <im2d.h>
#include <rga.h>
#endif

#define VIDEO_DEV "/dev/video0"
#define SUB_DEV "/dev/v4l-subdev2"
#define SLEEP_MODE_FILE "/sys/devices/platform/ff470000.i2c/i2c-4/4-000f/sleep_mode"

#define RK_ALIGN(x, a) (((x) + (a)-1) & ~((a)-1))
#define RK_ALIGN_2(x) RK_ALIGN(x, 2)
#define RK_ALIGN_16(x) RK_ALIGN(x, 16)
#define RK_ALIGN_32(x) RK_ALIGN(x, 32)

int sub_dev_fd = -1;
#define VENC_CHANNEL 0
#define JPEG_CHANNEL 1
MB_POOL memPool = MB_INVALID_POOLID;

bool sleep_mode_available = false;
bool should_exit = false;
float quality_factor = 1.0f;

// Video detection state - volatile because accessed from multiple threads
volatile uint32_t detected_width = 0, detected_height = 0;
volatile bool detected_signal = false;

// JPEG encoder state
static pthread_t *jpeg_read_thread = NULL;
static volatile bool jpeg_running = false;
static int jpeg_quality = 80;  // Default quality 1-99
static uint32_t jpeg_width = 0;
static uint32_t jpeg_height = 0;
static pthread_mutex_t jpeg_mutex = PTHREAD_MUTEX_INITIALIZER;

// Raw frame encoder state (uses RGA hardware for YUV→BGRX conversion if available)
static volatile bool rgb_running = false;
static uint32_t rgb_width = 0;
static uint32_t rgb_height = 0;
static pthread_mutex_t rgb_mutex = PTHREAD_MUTEX_INITIALIZER;

#ifdef HAS_RGA
// RGA hardware acceleration state using DMA buffers (no IOMMU required)
static bool rga_initialized = false;
static bool rga_ready = false;                  // Fast-path flag: true when buffers are allocated and ready
static MB_BLK rga_output_blk = NULL;           // DMA buffer for output BGRX data
static int rga_output_fd = -1;                  // DMA fd for output buffer
static uint8_t *rga_output_vaddr = NULL;        // Virtual address for reading output
static rga_buffer_handle_t rga_output_handle = 0; // RGA handle for output buffer
static rga_buffer_t rga_output_buffer;          // Pre-computed wrapped buffer (avoids call per frame)
static size_t rga_output_buffer_size = 0;       // Current buffer size
static uint32_t rga_output_width = 0;           // Current output dimensions
static uint32_t rga_output_height = 0;
static pthread_mutex_t rga_mutex = PTHREAD_MUTEX_INITIALIZER;

// Pool for RGA output buffers (separate from video capture pool)
static MB_POOL rga_output_pool = MB_INVALID_POOLID;

// Input handle cache - video capture uses triple-buffering with 3 fds
// Caching handles avoids expensive importbuffer_fd/releasebuffer_handle on every frame
#define RGA_INPUT_CACHE_SIZE 8
static struct {
    int fd;
    rga_buffer_handle_t handle;
    rga_buffer_t buffer;                        // Pre-computed wrapped buffer
    uint32_t width;
    uint32_t height;
} rga_input_cache[RGA_INPUT_CACHE_SIZE];
static int rga_input_cache_count = 0;
#endif

static void *venc_read_stream(void *arg);
static void *jpeg_read_stream(void *arg);
static int send_yuv_frame_fd(int yuv_fd, uint32_t width, uint32_t height, uint32_t yuv_size);
#ifdef HAS_RGA
static void rga_cleanup(void);
#endif

RK_U64 get_us()
{
    struct timespec time = {0, 0};
    clock_gettime(CLOCK_MONOTONIC, &time);
    return (RK_U64)time.tv_sec * 1000000 + (RK_U64)time.tv_nsec / 1000; /* microseconds */
}

static void ensure_sleep_mode_disabled()
{
    if (!sleep_mode_available)
    {
        return;
    }

    int fd = open(SLEEP_MODE_FILE, O_RDWR);
    if (fd < 0)
    {
        log_error("Failed to open sleep mode file: %s", strerror(errno));
        return;
    }
    lseek(fd, 0, SEEK_SET);
    char buffer[1];
    read(fd, buffer, 1);
    if (buffer[0] == '0') {
        close(fd);
        return;
    }
    log_warn("HDMI sleep mode is not disabled, disabling it");
    lseek(fd, 0, SEEK_SET);
    write(fd, "0", 1);
    close(fd);

    usleep(1000); // give some time to the system to disable the sleep mode
    return;
}

static void detect_sleep_mode()
{
    if (access(SLEEP_MODE_FILE, F_OK) != 0) {
        sleep_mode_available = false;
        return;
    }
    sleep_mode_available = true;
    ensure_sleep_mode_disabled();
}

double calculate_bitrate(float bitrate_factor, int width, int height)
{
    const int32_t base_bitrate_high = 2000;
    const int32_t base_bitrate_low = 512;

    double pixels = (double)width * height;
    double ref_pixels = 1920.0 * 1080.0;

    double scale_factor = pixels / ref_pixels;

    int32_t base_bitrate = base_bitrate_low + (int32_t)((base_bitrate_high - base_bitrate_low) * bitrate_factor);

    int32_t bitrate = (int32_t)(base_bitrate * scale_factor);

    const int32_t min_bitrate = 100;
    if (bitrate < min_bitrate)
    {
        bitrate = min_bitrate;
    }

    return bitrate;
}

static void populate_venc_attr(VENC_CHN_ATTR_S *stAttr, RK_U32 bitrate, RK_U32 max_bitrate, RK_U32 width, RK_U32 height)
{
    memset(stAttr, 0, sizeof(VENC_CHN_ATTR_S));

    stAttr->stRcAttr.enRcMode = VENC_RC_MODE_H264VBR;
    stAttr->stRcAttr.stH264Vbr.u32BitRate = bitrate;
    stAttr->stRcAttr.stH264Vbr.u32MaxBitRate = max_bitrate;
    stAttr->stRcAttr.stH264Vbr.u32Gop = 60;

    stAttr->stVencAttr.enType = RK_VIDEO_ID_AVC;
    stAttr->stVencAttr.enPixelFormat = RK_FMT_YUV422_YUYV;
    stAttr->stVencAttr.u32Profile = H264E_PROFILE_HIGH;
    stAttr->stVencAttr.u32PicWidth = width;
    stAttr->stVencAttr.u32PicHeight = height;
    // stAttr->stVencAttr.u32VirWidth = (width + 15) & (~15);
    // stAttr->stVencAttr.u32VirHeight = (height + 15) & (~15);
    stAttr->stVencAttr.u32VirWidth = RK_ALIGN_2(width);
    stAttr->stVencAttr.u32VirHeight = RK_ALIGN_2(height);
    stAttr->stVencAttr.u32StreamBufCnt = 3;
    stAttr->stVencAttr.u32BufSize = width * height * 3 / 2;
    stAttr->stVencAttr.enMirror = MIRROR_NONE;
}

static void populate_jpeg_attr(VENC_CHN_ATTR_S *stAttr, RK_U32 width, RK_U32 height, RK_U32 quality)
{
    memset(stAttr, 0, sizeof(VENC_CHN_ATTR_S));

    // MJPEG doesn't use rate control like H.264, it uses quality-based compression
    stAttr->stRcAttr.enRcMode = VENC_RC_MODE_MJPEGCBR;
    // Set a reasonable bitrate for MJPEG (will be quality-controlled)
    stAttr->stRcAttr.stMjpegCbr.u32BitRate = 10000; // 10 Mbps default

    stAttr->stVencAttr.enType = RK_VIDEO_ID_MJPEG;
    stAttr->stVencAttr.enPixelFormat = RK_FMT_YUV422_YUYV;
    stAttr->stVencAttr.u32PicWidth = width;
    stAttr->stVencAttr.u32PicHeight = height;
    stAttr->stVencAttr.u32VirWidth = RK_ALIGN_2(width);
    stAttr->stVencAttr.u32VirHeight = RK_ALIGN_2(height);
    stAttr->stVencAttr.u32StreamBufCnt = 2;  // JPEG needs fewer buffers
    stAttr->stVencAttr.u32BufSize = width * height * 3 / 2;
    stAttr->stVencAttr.enMirror = MIRROR_NONE;
}

pthread_t *venc_read_thread = NULL;
volatile bool venc_running = false;
static int32_t venc_start(int32_t bitrate, int32_t max_bitrate, int32_t width, int32_t height)
{
    int32_t ret;
    VENC_CHN_ATTR_S stAttr;
    populate_venc_attr(&stAttr, bitrate, max_bitrate, width, height);

    ret = RK_MPI_VENC_CreateChn(VENC_CHANNEL, &stAttr);
    if (ret < 0)
    {
        RK_LOGE("error RK_MPI_VENC_CreateChn, %d", ret);
        return ret;
    }

    VENC_RECV_PIC_PARAM_S stRecvParam;
    memset(&stRecvParam, 0, sizeof(VENC_RECV_PIC_PARAM_S));
    stRecvParam.s32RecvPicNum = -1;
    ret = RK_MPI_VENC_StartRecvFrame(VENC_CHANNEL, &stRecvParam);
    if (ret < 0)
    {
        RK_LOGE("error RK_MPI_VENC_StartRecvFrame, %d", ret);
        return ret;
    }

    venc_running = true;
    venc_read_thread = malloc(sizeof(pthread_t));
    if (pthread_create(venc_read_thread, NULL, venc_read_stream, NULL) != 0)
    {
        RK_LOGE("Failed to create venc_read_thread");
        return RK_FAILURE;
    }

    return RK_SUCCESS;
}

static int32_t venc_stop()
{
    venc_running = false;

    int32_t ret;
    ret = RK_MPI_VENC_StopRecvFrame(VENC_CHANNEL);
    if (ret != RK_SUCCESS)
    {
        RK_LOGE("Failed to stop receiving frames for VENC_CHANNEL, error code: %d", ret);
        return ret;
    }

    if (venc_read_thread != NULL)
    {
        pthread_join(*venc_read_thread, NULL);
        free(venc_read_thread);
        venc_read_thread = NULL;
    }

    ret = RK_MPI_VENC_DestroyChn(VENC_CHANNEL);
    if (ret != RK_SUCCESS)
    {
        RK_LOGE("Failed to destroy VENC_CHANNEL, error code: %d", ret);
        return ret;
    }

    return RK_SUCCESS;
}

// JPEG encoder functions
static int32_t jpeg_channel_start(int32_t width, int32_t height, int32_t quality)
{
    int32_t ret;
    VENC_CHN_ATTR_S stAttr;
    populate_jpeg_attr(&stAttr, width, height, quality);

    ret = RK_MPI_VENC_CreateChn(JPEG_CHANNEL, &stAttr);
    if (ret < 0)
    {
        RK_LOGE("error creating JPEG_CHANNEL, %d", ret);
        return ret;
    }

    // Set JPEG quality parameter
    VENC_JPEG_PARAM_S stJpegParam;
    memset(&stJpegParam, 0, sizeof(VENC_JPEG_PARAM_S));
    stJpegParam.u32Qfactor = quality;
    ret = RK_MPI_VENC_SetJpegParam(JPEG_CHANNEL, &stJpegParam);
    if (ret != RK_SUCCESS)
    {
        RK_LOGE("Failed to set JPEG quality, error code: %d", ret);
        // Continue anyway, quality will be default
    }

    VENC_RECV_PIC_PARAM_S stRecvParam;
    memset(&stRecvParam, 0, sizeof(VENC_RECV_PIC_PARAM_S));
    stRecvParam.s32RecvPicNum = -1;
    ret = RK_MPI_VENC_StartRecvFrame(JPEG_CHANNEL, &stRecvParam);
    if (ret < 0)
    {
        RK_LOGE("error RK_MPI_VENC_StartRecvFrame for JPEG, %d", ret);
        RK_MPI_VENC_DestroyChn(JPEG_CHANNEL);
        return ret;
    }

    return RK_SUCCESS;
}

static int32_t jpeg_channel_stop()
{
    int32_t ret;
    ret = RK_MPI_VENC_StopRecvFrame(JPEG_CHANNEL);
    if (ret != RK_SUCCESS)
    {
        RK_LOGE("Failed to stop receiving frames for JPEG_CHANNEL, error code: %d", ret);
    }

    ret = RK_MPI_VENC_DestroyChn(JPEG_CHANNEL);
    if (ret != RK_SUCCESS)
    {
        RK_LOGE("Failed to destroy JPEG_CHANNEL, error code: %d", ret);
        return ret;
    }

    return RK_SUCCESS;
}

static void *jpeg_read_stream(void *arg)
{
    (void)arg;
    void *pData = RK_NULL;
    int s32Ret;
    int frameCount = 0;
    int emptyCount = 0;

    log_info("JPEG read thread started");

    VENC_STREAM_S stFrame;
    stFrame.pstPack = malloc(sizeof(VENC_PACK_S));

    while (jpeg_running)
    {
        log_trace("JPEG: RK_MPI_VENC_GetStream");
        s32Ret = RK_MPI_VENC_GetStream(JPEG_CHANNEL, &stFrame, 200); // blocks max 200ms
        if (s32Ret == RK_SUCCESS)
        {
            frameCount++;
            emptyCount = 0;
            // Frame logging handled by Go proxy - only log first frame here for confirmation
            if (frameCount == 1)
            {
                log_info("JPEG: first frame received, size=%d", stFrame.pstPack->u32Len);
            }
            pData = RK_MPI_MB_Handle2VirAddr(stFrame.pstPack->pMbBlk);
            video_send_jpeg_frame(pData, (ssize_t)stFrame.pstPack->u32Len);
            s32Ret = RK_MPI_VENC_ReleaseStream(JPEG_CHANNEL, &stFrame);
            if (s32Ret != RK_SUCCESS)
            {
                log_error("JPEG: RK_MPI_VENC_ReleaseStream fail %x", s32Ret);
            }
        }
        else
        {
            if (s32Ret == RK_ERR_VENC_BUF_EMPTY)
            {
                emptyCount++;
                // Only log buffer empty on first occurrence
                if (emptyCount == 1)
                {
                    log_trace("JPEG: buffer empty, waiting for frames");
                }
                continue;
            }
            log_error("JPEG: RK_MPI_VENC_GetStream fail %x", s32Ret);
            break;
        }
    }

    log_info("JPEG read thread stopped, total frames=%d", frameCount);
    free(stFrame.pstPack);
    return NULL;
}

// Public JPEG encoder API
int jpeg_encoder_start(int quality)
{
    // Use jetkvm_video_get_status() to get the video state from ctrl.c
    volatile jetkvm_video_state_t *video_state = jetkvm_video_get_status();

    log_debug("JPEG start: quality=%d, detected=%dx%d signal=%d, state ready=%d %dx%d",
              quality, detected_width, detected_height, detected_signal ? 1 : 0,
              video_state->ready, video_state->width, video_state->height);

    // Wait for video signal to be detected (up to 10 seconds)
    int retries = 0;
    const int max_retries = 100; // 100 * 100ms = 10 seconds
    while ((detected_width == 0 || detected_height == 0 || !detected_signal) && retries < max_retries)
    {
        if (retries == 0)
        {
            log_debug("JPEG: waiting for video signal...");
        }
        usleep(100000); // 100ms
        retries++;
        if (retries % 10 == 0)
        {
            video_state = jetkvm_video_get_status();
            log_debug("JPEG: waiting retry %d, detected=%dx%d signal=%d",
                      retries, detected_width, detected_height, detected_signal ? 1 : 0);
        }
    }

    pthread_mutex_lock(&jpeg_mutex);

    if (jpeg_running)
    {
        log_warn("JPEG encoder already running");
        pthread_mutex_unlock(&jpeg_mutex);
        return 0;
    }

    // Use local volatile variables which are directly updated by FORMAT THREAD
    if (detected_width == 0 || detected_height == 0 || !detected_signal)
    {
        log_error("Cannot start JPEG encoder: no video signal after %d retries (detected=%dx%d signal=%d)",
                  retries, detected_width, detected_height, detected_signal ? 1 : 0);
        pthread_mutex_unlock(&jpeg_mutex);
        return -1;
    }

    jpeg_width = detected_width;
    jpeg_height = detected_height;
    jpeg_quality = (quality > 0 && quality <= 99) ? quality : 80;

    // Check if video streaming is running - JPEG encoder needs it to receive frames
    uint8_t streaming_status = video_get_streaming_status();
    log_debug("JPEG: streaming_status=%d", streaming_status);

    if (streaming_status == 0)
    {
        log_debug("JPEG: video streaming stopped, starting it");
        video_start_streaming();
        usleep(500000); // 500ms
    }

    int32_t ret = jpeg_channel_start(jpeg_width, jpeg_height, jpeg_quality);
    if (ret != RK_SUCCESS)
    {
        log_error("Failed to start JPEG channel: %d", ret);
        pthread_mutex_unlock(&jpeg_mutex);
        return -1;
    }

    jpeg_running = true;
    jpeg_read_thread = malloc(sizeof(pthread_t));
    if (pthread_create(jpeg_read_thread, NULL, jpeg_read_stream, NULL) != 0)
    {
        log_error("Failed to create jpeg_read_thread");
        jpeg_running = false;
        jpeg_channel_stop();
        free(jpeg_read_thread);
        jpeg_read_thread = NULL;
        pthread_mutex_unlock(&jpeg_mutex);
        return -1;
    }

    log_info("JPEG encoder started: %dx%d quality=%d", jpeg_width, jpeg_height, jpeg_quality);
    pthread_mutex_unlock(&jpeg_mutex);
    return 0;
}

void jpeg_encoder_stop()
{
    pthread_mutex_lock(&jpeg_mutex);

    if (!jpeg_running)
    {
        log_info("JPEG encoder already stopped");
        pthread_mutex_unlock(&jpeg_mutex);
        return;
    }

    jpeg_running = false;

    if (jpeg_read_thread != NULL)
    {
        pthread_join(*jpeg_read_thread, NULL);
        free(jpeg_read_thread);
        jpeg_read_thread = NULL;
    }

    jpeg_channel_stop();
    log_info("JPEG encoder stopped");
    pthread_mutex_unlock(&jpeg_mutex);
}

int jpeg_encoder_set_quality(int quality)
{
    if (quality < 1 || quality > 99)
    {
        return -1;
    }

    pthread_mutex_lock(&jpeg_mutex);
    jpeg_quality = quality;

    if (jpeg_running)
    {
        // Update quality on running encoder
        VENC_JPEG_PARAM_S stJpegParam;
        memset(&stJpegParam, 0, sizeof(VENC_JPEG_PARAM_S));
        stJpegParam.u32Qfactor = quality;
        int32_t ret = RK_MPI_VENC_SetJpegParam(JPEG_CHANNEL, &stJpegParam);
        if (ret != RK_SUCCESS)
        {
            log_error("Failed to update JPEG quality: %d", ret);
            pthread_mutex_unlock(&jpeg_mutex);
            return -1;
        }
    }

    pthread_mutex_unlock(&jpeg_mutex);
    return 0;
}

int jpeg_encoder_get_quality()
{
    return jpeg_quality;
}

bool jpeg_encoder_is_running()
{
    return jpeg_running;
}

struct buffer
{
    struct v4l2_plane plane_buffer;
    MB_BLK mb_blk;
};

const int input_buffer_count = 3;

static int32_t buf_init()
{
    MB_POOL_CONFIG_S stMbPoolCfg;
    memset(&stMbPoolCfg, 0, sizeof(MB_POOL_CONFIG_S));
    stMbPoolCfg.u64MBSize = 1920 * 1080 * 3; // max resolution
    stMbPoolCfg.u32MBCnt = input_buffer_count;
    stMbPoolCfg.enAllocType = MB_ALLOC_TYPE_DMA;
    stMbPoolCfg.bPreAlloc = RK_TRUE;
    memPool = RK_MPI_MB_CreatePool(&stMbPoolCfg);
    if (memPool == MB_INVALID_POOLID)
    {
        return -1;
    }
    log_info("created memory pool");

    return RK_SUCCESS;
}

pthread_t *format_thread = NULL;

// Function in ctrl.c to set counter (avoids extern linker issues with CGO)
extern void ctrl_set_report_count(uint32_t value);

int video_init(float factor)
{
    // Test: use function call instead of extern variable to see if CGO can read it
    ctrl_set_report_count(999);

    detect_sleep_mode();

    if (factor <= 0 || factor > 1) {
        factor = 1.0f;
    }
    quality_factor = factor;

    if (RK_MPI_SYS_Init() != RK_SUCCESS)
    {
        log_error("RK_MPI_SYS_Init failed");
        return RK_FAILURE;
    }

    if (sub_dev_fd < 0)
    {
        sub_dev_fd = open(SUB_DEV, O_RDWR);
        if (sub_dev_fd < 0)
        {
            log_error("failed to open control sub device %s: %s", SUB_DEV, strerror(errno));
            return errno;
        }
        log_info("opened control sub device %s", SUB_DEV);
    }

    int32_t ret = buf_init();
    if (ret != RK_SUCCESS)
    {
        log_error("buf_init failed with error: %d", ret);
        return ret;
    }
    log_info("buf_init completed successfully");

    format_thread = malloc(sizeof(pthread_t));
    pthread_create(format_thread, NULL, run_detect_format, NULL);
    return RK_SUCCESS;
}

// static int32_t venc_set_param(int32_t bitrate, int32_t max_bitrate, int32_t width, int32_t height)
// {

//     VENC_CHN_ATTR_S stAttr;
//     populate_venc_attr(&stAttr, bitrate, max_bitrate, width, height);
//     VENC_CHN_PARAM_S stParam;
//     memset(&stParam, 0, sizeof(VENC_CHN_PARAM_S));

//     RK_MPI_VENC_StopRecvFrame(VENC_CHANNEL);

//     int32_t ret = RK_MPI_VENC_SetChnParam(VENC_CHANNEL, &stAttr);
//     if (ret < 0)
//     {
//         RK_LOGE("error RK_MPI_VENC_SetChnParam, %d", ret);
//         return ret;
//     }
//     VENC_RECV_PIC_PARAM_S stRecvParam;
//     memset(&stRecvParam, 0, sizeof(VENC_RECV_PIC_PARAM_S));
//     stRecvParam.s32RecvPicNum = -1;
//     ret = RK_MPI_VENC_StartRecvFrame(VENC_CHANNEL, &stRecvParam);
//     if (ret < 0)
//     {
//         RK_LOGE("error RK_MPI_VENC_StartRecvFrame, %d", ret);
//         return ret;
//     }

//     return RK_SUCCESS;
// }

/**
 * @brief Continuously reads encoded video streams and sends them over unix socket.
 *
 * @param arg Unused parameter (void pointer for thread compatibility)
 * @return NULL Always returns NULL
 */
static void *venc_read_stream(void *arg)
{
    (void)arg;
    void *pData = RK_NULL;
    int loopCount = 0;
    int s32Ret;

    VENC_STREAM_S stFrame;
    stFrame.pstPack = malloc(sizeof(VENC_PACK_S));
    while (venc_running)
    {
        log_trace("RK_MPI_VENC_GetStream");
        s32Ret = RK_MPI_VENC_GetStream(VENC_CHANNEL, &stFrame, 200); // blocks max 200ms
        if (s32Ret == RK_SUCCESS)
        {
            RK_U64 nowUs = get_us();
            log_trace("chn:0, loopCount:%d enc->seq:%d wd:%d pts=%llu delay=%lldus",
                   loopCount, stFrame.u32Seq, stFrame.pstPack->u32Len,
                   stFrame.pstPack->u64PTS, nowUs - stFrame.pstPack->u64PTS);
            pData = RK_MPI_MB_Handle2VirAddr(stFrame.pstPack->pMbBlk);
            video_send_frame(pData, (ssize_t)stFrame.pstPack->u32Len);
            s32Ret = RK_MPI_VENC_ReleaseStream(VENC_CHANNEL, &stFrame);
            if (s32Ret != RK_SUCCESS)
            {
                log_error("RK_MPI_VENC_ReleaseStream fail %x", s32Ret);
            }
            loopCount++;
        }
        else
        {
            if (s32Ret == RK_ERR_VENC_BUF_EMPTY)
            {
                continue;
            }
            log_error("RK_MPI_VENC_GetStream fail %x", s32Ret);
            break;
        }
    }
    log_info("exiting venc_read_stream");
    free(stFrame.pstPack);
    return NULL;
}

bool streaming_flag = false;

bool streaming_stopped = true;
pthread_mutex_t streaming_stopped_mutex = PTHREAD_MUTEX_INITIALIZER;

pthread_t *streaming_thread = NULL;
pthread_mutex_t streaming_mutex = PTHREAD_MUTEX_INITIALIZER;

bool get_streaming_flag()
{
    log_info("getting streaming flag");
    pthread_mutex_lock(&streaming_mutex);
    bool flag = streaming_flag;
    pthread_mutex_unlock(&streaming_mutex);
    return flag;
}

void set_streaming_flag(bool flag)
{
    log_info("setting streaming flag to %d", flag);

    pthread_mutex_lock(&streaming_mutex);
    streaming_flag = flag;
    pthread_mutex_unlock(&streaming_mutex);

    video_send_format_report();
}

void set_streaming_stopped(bool stopped)
{
    pthread_mutex_lock(&streaming_stopped_mutex);
    streaming_stopped = stopped;
    pthread_mutex_unlock(&streaming_stopped_mutex);

    video_send_format_report();
}

bool get_streaming_stopped()
{
    pthread_mutex_lock(&streaming_stopped_mutex);
    bool stopped = streaming_stopped;
    pthread_mutex_unlock(&streaming_stopped_mutex);
    return stopped;
}

void write_buffer_to_file(const uint8_t *buffer, size_t length, const char *filename)
{
    FILE *file = fopen(filename, "wb");
    fwrite(buffer, 1, length, file);
    fclose(file);
}

void *run_video_stream(void *arg)
{
    enum v4l2_buf_type type = V4L2_BUF_TYPE_VIDEO_CAPTURE_MPLANE;

    log_info("running video stream");

    set_streaming_stopped(false);
    while (streaming_flag)
    {
        if (detected_signal == false)
        {
            usleep(10000); // Reduced to 10ms for better responsiveness to streaming_flag changes
            continue;
        }

        int video_dev_fd = open(VIDEO_DEV, O_RDWR);
        if (video_dev_fd < 0)
        {
            log_error("failed to open video capture device %s: %s", VIDEO_DEV, strerror(errno));
            usleep(1000000);
            continue;
        }
        log_info("opened video capture device %s", VIDEO_DEV);

        uint32_t width = detected_width;
        uint32_t height = detected_height;
        struct v4l2_format fmt;
        memset(&fmt, 0, sizeof(struct v4l2_format));
        fmt.type = type;
        fmt.fmt.pix_mp.width = width;
        fmt.fmt.pix_mp.height = height;
        fmt.fmt.pix_mp.pixelformat = V4L2_PIX_FMT_YUYV;
        fmt.fmt.pix_mp.field = V4L2_FIELD_ANY;

        if (ioctl(video_dev_fd, VIDIOC_S_FMT, &fmt) < 0)
        {
            log_error("Set format fail: %s", strerror(errno));
            usleep(100000); // Sleep for 100 milliseconds
            close(video_dev_fd);
            continue;
        }

        struct v4l2_buffer buf;

        struct v4l2_requestbuffers req;
        req.count = input_buffer_count;
        req.type = V4L2_BUF_TYPE_VIDEO_CAPTURE_MPLANE;
        req.memory = V4L2_MEMORY_DMABUF;

        if (ioctl(video_dev_fd, VIDIOC_REQBUFS, &req) < 0)
        {
            log_error("VIDIOC_REQBUFS failed: %s", strerror(errno));
            close(video_dev_fd);
            return (void *)errno;
        }
        log_info("VIDIOC_REQBUFS successful");

        struct buffer buffers[3] = {};
        log_info("allocated buffers");

        for (int i = 0; i < input_buffer_count; i++)
        {
            struct v4l2_plane *planes_buffer = &buffers[i].plane_buffer;
            memset(planes_buffer, 0, sizeof(struct v4l2_plane));

            memset(&buf, 0, sizeof(buf));
            buf.type = V4L2_BUF_TYPE_VIDEO_CAPTURE_MPLANE;
            buf.memory = V4L2_MEMORY_DMABUF;
            buf.m.planes = planes_buffer;
            buf.length = 1;
            buf.index = i;

            if (-1 == ioctl(video_dev_fd, VIDIOC_QUERYBUF, &buf))
            {
                log_error("VIDIOC_QUERYBUF failed: %s", strerror(errno));
                req.count = i;
                close(video_dev_fd);
                return (void *)errno;
            }
            log_info("VIDIOC_QUERYBUF successful for buffer %d", i);

            log_info("plane: length = %d", planes_buffer->length);
            log_info("plane: offset = %d", planes_buffer->m.mem_offset);

            MB_BLK blk = RK_MPI_MB_GetMB(memPool, (planes_buffer)->length, RK_TRUE);
            if (blk == NULL)
            {
                log_error("get mb blk failed!");
                close(video_dev_fd);
                return (void *)errno;
            }
            log_info("Got memory block for buffer %d", i);

            buffers[i].mb_blk = blk;

            RK_S32 buf_fd = (RK_MPI_MB_Handle2Fd(blk));
            if (buf_fd < 0)
            {
                log_error("RK_MPI_MB_Handle2Fd failed!");
                close(video_dev_fd);
                return (void *)errno;
            }
            log_info("Converted memory block to file descriptor for buffer %d", i);
            planes_buffer->m.fd = buf_fd;
        }

        for (int i = 0; i < input_buffer_count; ++i)
        {
            struct v4l2_buffer buf;
            memset(&buf, 0, sizeof(buf));
            buf.type = V4L2_BUF_TYPE_VIDEO_CAPTURE_MPLANE;
            buf.memory = V4L2_MEMORY_DMABUF;
            buf.length = 1;
            buf.index = i;
            buf.m.planes = &buffers[i].plane_buffer;
            if (ioctl(video_dev_fd, VIDIOC_QBUF, &buf) < 0)
            {
                log_error("VIDIOC_QBUF failed: %s", strerror(errno));
                close(video_dev_fd);
                return (void *)errno;
            }
            log_info("VIDIOC_QBUF successful for buffer %d", i);
        }

        if (ioctl(video_dev_fd, VIDIOC_STREAMON, &type) < 0)
        {
            log_error("VIDIOC_STREAMON failed: %s", strerror(errno));
            close(video_dev_fd);
            return (void *)errno;
        }

        struct v4l2_plane tmp_plane;

        // Set VENC parameters
        int32_t bitrate = calculate_bitrate(quality_factor, width, height);
        RK_S32 ret = venc_start(bitrate, bitrate * 2, width, height);
        if (ret != RK_SUCCESS)
        {
            log_error("Set VENC parameters failed with %#x", ret);
            goto cleanup;
        }

        fd_set fds;
        struct timeval tv;
        int r;
        uint32_t num = 0;
        VIDEO_FRAME_INFO_S stFrame;



        while (streaming_flag)
        {
            FD_ZERO(&fds);
            FD_SET(video_dev_fd, &fds);
            tv.tv_sec = 1;
            tv.tv_usec = 0;

            r = select(video_dev_fd + 1, &fds, NULL, NULL, &tv);
            if (r == 0)
            {
                log_info("select timeout");
                ensure_sleep_mode_disabled();
                break;
            }
            if (r == -1)
            {
                if (errno == EINTR)
                {
                    continue;
                }
                log_error("select in video streaming");
                break;
            }
            memset(&buf, 0, sizeof(buf));
            buf.type = V4L2_BUF_TYPE_VIDEO_CAPTURE_MPLANE;
            buf.memory = V4L2_MEMORY_DMABUF;
            buf.m.planes = &tmp_plane;
            buf.length = 1;
            if (ioctl(video_dev_fd, VIDIOC_DQBUF, &buf) < 0)
            {
                log_error("VIDIOC_DQBUF failed: %s", strerror(errno));
                break;
            }
            log_trace("got frame, bytesused = %d", tmp_plane.bytesused);
            memset(&stFrame, 0, sizeof(VIDEO_FRAME_INFO_S));
            MB_BLK blk = RK_NULL;
            blk = RK_MPI_MMZ_Fd2Handle(tmp_plane.m.fd);
            assert(blk != RK_NULL);
            stFrame.stVFrame.pMbBlk = blk;
            stFrame.stVFrame.u32Width = width;
            stFrame.stVFrame.u32Height = height;
            // stFrame.stVFrame.u32VirWidth = (width + 15) & (~15);
            // stFrame.stVFrame.u32VirHeight = (height + 15) & (~15);
            stFrame.stVFrame.u32VirWidth = RK_ALIGN_2(width);
            stFrame.stVFrame.u32VirHeight = RK_ALIGN_2(height);
            stFrame.stVFrame.u32TimeRef = num; // frame number
            stFrame.stVFrame.u64PTS = get_us();
            stFrame.stVFrame.enPixelFormat = RK_FMT_YUV422_YUYV;
            stFrame.stVFrame.u32FrameFlag |= 0;
            stFrame.stVFrame.enCompressMode = COMPRESS_MODE_NONE;
            bool retried = false;
        retry_send_frame:
            if (RK_MPI_VENC_SendFrame(VENC_CHANNEL, &stFrame, 2000) != RK_SUCCESS)
            {
                if (retried == true)
                {
                    log_error("RK_MPI_VENC_SendFrame retry failed");
                }
                else
                {
                    log_error("RK_MPI_VENC_SendFrame failed,retrying");
                    retried = true;
                    usleep(1000llu);
                    goto retry_send_frame;
                }
            }

            // Also send frame to JPEG encoder if running
            if (jpeg_running)
            {
                if (RK_MPI_VENC_SendFrame(JPEG_CHANNEL, &stFrame, 100) != RK_SUCCESS)
                {
                    // Don't retry for JPEG - it's secondary and shouldn't block H.264
                    log_trace("JPEG: RK_MPI_VENC_SendFrame failed (non-critical)");
                }
            }

            // Send raw YUV422 frame if RGB encoder is running
            // Use DMA fd for RGA hardware acceleration (no IOMMU required on RV1106)
            if (rgb_running)
            {
                // Get the DMA fd from the memory block (already a DMA buffer from V4L2)
                int yuv_fd = RK_MPI_MB_Handle2Fd(blk);
                if (yuv_fd >= 0)
                {
                    // YUV422 YUYV = 2 bytes per pixel
                    uint32_t yuv_size = RK_ALIGN_2(width) * height * 2;
                    send_yuv_frame_fd(yuv_fd, width, height, yuv_size);
                }
            }

            num++;

            if (ioctl(video_dev_fd, VIDIOC_QBUF, &buf) < 0)
                log_error("failure VIDIOC_QBUF: %s", strerror(errno));
        }
    cleanup:
        log_info("cleaning up video capture device %s", VIDEO_DEV);
        if (ioctl(video_dev_fd, VIDIOC_STREAMOFF, &type) < 0)
        {
            log_error("VIDIOC_STREAMOFF failed: %s", strerror(errno));
        }

        // Explicitly free V4L2 buffer queue
        struct v4l2_requestbuffers req_free;
        memset(&req_free, 0, sizeof(req_free));
        req_free.count = 0;
        req_free.type = V4L2_BUF_TYPE_VIDEO_CAPTURE_MPLANE;
        req_free.memory = V4L2_MEMORY_DMABUF;

        if (ioctl(video_dev_fd, VIDIOC_REQBUFS, &req_free) < 0)
        {
            log_error("Failed to free V4L2 buffers: %s", strerror(errno));
        }

        venc_stop();

        for (int i = 0; i < input_buffer_count; i++)
        {
            if (buffers[i].mb_blk != NULL)
            {
                RK_MPI_MB_ReleaseMB((buffers + i)->mb_blk);
            }
        }

        log_info("closing video capture device %s", VIDEO_DEV);
        close(video_dev_fd);
    }

    log_info("video stream thread exiting");

    set_streaming_stopped(true);

    return NULL;
}

void video_shutdown()
{
    if (should_exit == true)
    {
        log_info("shutting down in progress already");
        return;
    }
    video_stop_streaming();

#ifdef HAS_RGA
    // Clean up RGA hardware resources
    rga_cleanup();
#endif

    should_exit = true;
    if (sub_dev_fd > 0)
    {
        shutdown(sub_dev_fd, SHUT_RDWR);
        log_info("Closed sub_dev_fd");
    }

    if (memPool != MB_INVALID_POOLID)
    {
        RK_MPI_MB_DestroyPool(memPool);
    }
    log_info("Destroyed memory pool");

    pthread_mutex_destroy(&streaming_mutex);
    log_info("Destroyed streaming mutex");
}

void video_start_streaming()
{
    log_info("starting video streaming");
    if (streaming_thread != NULL)
    {
        bool stopped = get_streaming_stopped();
        if (stopped == true) {
            log_error("video streaming already stopped but streaming_thread is not NULL");
            assert(stopped == true);
        }
        log_warn("video streaming already started");
        return;
    }

    pthread_t *new_thread = malloc(sizeof(pthread_t));
    if (new_thread == NULL)
    {
        log_error("Failed to allocate memory for streaming thread");
        return;
    }

    set_streaming_flag(true);
    int result = pthread_create(new_thread, NULL, run_video_stream, NULL);
    if (result != 0)
    {
        log_error("Failed to create streaming thread: %s", strerror(result));
        set_streaming_flag(false);
        free(new_thread);
        return;
    }

    // Only set streaming_thread after successful creation
    streaming_thread = new_thread;
}

bool wait_for_streaming_stopped()
{
    int attempts = 0;
    while (attempts < 30) {
        if (get_streaming_stopped() == true) {
            log_info("video streaming stopped after %d attempts", attempts);
            return true;
        }
        usleep(100000); // 100ms
        attempts++;
    }
    log_error("video streaming did not stop after 3s");
    return false;
}

void video_stop_streaming()
{
    if (streaming_thread == NULL) {
        log_info("video streaming already stopped");
        return;
    }

    log_info("stopping video streaming");
    set_streaming_flag(false);

    log_info("waiting for video streaming thread to exit");
    wait_for_streaming_stopped();

    pthread_join(*streaming_thread, NULL);
    free(streaming_thread);
    streaming_thread = NULL;

    log_info("video streaming stopped");
}

uint8_t video_get_streaming_status() {
    // streaming flag can be false when stopping streaming
    if (get_streaming_flag() == true) return 1;
    if (get_streaming_stopped() == false) return 2;
    return 0;
}

void video_restart_streaming()
{
    uint8_t streaming_status = video_get_streaming_status();
    // 0 = stopped, 1 = running, 2 = stopping

    // If stopped and no signal detected, don't restart
    // But if signal is present, allow restart even when stopped (needed for audio sync)
    if (streaming_status == 0 && !detected_signal) {
        log_info("will not restart video streaming because it's stopped and no signal detected");
        return;
    }

    if (streaming_status == 1 || streaming_status == 2) {
        video_stop_streaming();
    }

    if (!wait_for_streaming_stopped()) {
        return;
    }

    video_start_streaming();
}

void *run_detect_format(void *arg)
{
    log_debug("FORMAT THREAD: starting, variable addresses: detected_width=%p, detected_height=%p, detected_signal=%p",
            (void*)&detected_width, (void*)&detected_height, (void*)&detected_signal);

    struct v4l2_event_subscription sub;
    struct v4l2_event ev;
    struct v4l2_dv_timings dv_timings;

    memset(&sub, 0, sizeof(sub));
    sub.type = V4L2_EVENT_SOURCE_CHANGE;
    if (ioctl(sub_dev_fd, VIDIOC_SUBSCRIBE_EVENT, &sub) == -1)
    {
        log_error("FORMAT THREAD: cannot subscribe to event");
        goto exit;
    }
    log_debug("FORMAT THREAD: subscribed to events, entering loop");

    while (!should_exit)
    {
        memset(&dv_timings, 0, sizeof(dv_timings));
        if (ioctl(sub_dev_fd, VIDIOC_QUERY_DV_TIMINGS, &dv_timings) != 0)
        {
            detected_signal = false;
            if (errno == ENOLINK)
            {
                // No timings could be detected because no signal was found.
                log_info("HDMI status: no signal");
                video_report_format(false, "no_signal", 0, 0, 0);
            }
            else if (errno == ENOLCK)
            {
                // The signal was unstable and the hardware could not lock on to it.
                log_info("HDMI status: no lock");
                video_report_format(false, "no_lock", 0, 0, 0);
            }
            else if (errno == ERANGE)
            {
                // Timings were found, but they are out of range of the hardware capabilities.
                log_warn("HDMI status: out of range");
                video_report_format(false, "out_of_range", 0, 0, 0);
            }
            else
            {
                log_error("error VIDIOC_QUERY_DV_TIMINGS: %s", strerror(errno));
                sleep(1);
                continue;
            }
        }
        else
        {
            log_info("Active width: %d", dv_timings.bt.width);
            log_info("Active height: %d", dv_timings.bt.height);
            double frames_per_second = (double)dv_timings.bt.pixelclock /
                                       ((dv_timings.bt.height + dv_timings.bt.vfrontporch + dv_timings.bt.vsync +
                                         dv_timings.bt.vbackporch) *
                                        (dv_timings.bt.width + dv_timings.bt.hfrontporch + dv_timings.bt.hsync +
                                         dv_timings.bt.hbackporch));
            log_info("Frames per second: %.2f fps", frames_per_second);

            bool should_restart = dv_timings.bt.width != detected_width || dv_timings.bt.height != detected_height || !detected_signal;

            detected_width = dv_timings.bt.width;
            detected_height = dv_timings.bt.height;
            detected_signal = true;
            log_debug("FORMAT THREAD: Signal detected! Set detected_width=%d, detected_height=%d, detected_signal=true",
                    detected_width, detected_height);
            video_report_format(true, NULL, detected_width, detected_height, frames_per_second);

            if (should_restart) {
                log_info("restarting video streaming due to format change");
                video_restart_streaming();
            }
        }

        memset(&ev, 0, sizeof(ev));
        if (ioctl(sub_dev_fd, VIDIOC_DQEVENT, &ev) != 0)
        {
            log_error("failed to VIDIOC_DQEVENT: %s", strerror(errno));
            break;
        }
        log_info("New event of type %u", ev.type);
        if (ev.type != V4L2_EVENT_SOURCE_CHANGE)
        {
            continue;
        }
        log_info("source change detected!");
    }
exit:
    close(sub_dev_fd);
    return NULL;
}


void video_set_quality_factor(float factor)
{
    quality_factor = factor;

    // TODO: update venc bitrate without stopping streaming
    video_restart_streaming();
}

float video_get_quality_factor() {
    return quality_factor;
}

// Request an IDR (keyframe) from the H.264 encoder
// This is useful for new clients that need to start decoding from a keyframe
int video_request_keyframe()
{
    if (!venc_running)
    {
        log_warn("Cannot request keyframe: encoder not running");
        return -1;
    }

    int32_t ret = RK_MPI_VENC_RequestIDR(VENC_CHANNEL, RK_FALSE);
    if (ret != RK_SUCCESS)
    {
        log_error("RK_MPI_VENC_RequestIDR failed: %d", ret);
        return -1;
    }

    log_info("Keyframe (IDR) requested from encoder");
    return 0;
}

#ifdef HAS_RGA
// Track whether RGA hardware is available (checked once at first use)
static bool rga_available_checked = false;
static bool rga_available = false;
// Global flag to disable RGA at runtime (can be set via env var)
static bool rga_env_checked = false;
// RGA is ENABLED by default (DMA buffer mode works on RV1106 without IOMMU)
// Set JETKVM_DISABLE_RGA=1 to disable for debugging
static bool rga_enabled = true;

// Check if RGA is enabled (can be disabled by environment variable)
static bool rga_check_enabled(void)
{
    if (rga_env_checked)
    {
        return rga_enabled;
    }
    rga_env_checked = true;

    // Check for explicit disable
    const char *disable_rga = getenv("JETKVM_DISABLE_RGA");
    if (disable_rga != NULL && (strcmp(disable_rga, "1") == 0 || strcmp(disable_rga, "true") == 0))
    {
        log_warn("RGA: Disabled by JETKVM_DISABLE_RGA environment variable");
        rga_enabled = false;
        return false;
    }

    // Default: RGA is ENABLED (DMA buffer mode works on RV1106)
    log_info("RGA: Enabled by default (DMA buffer mode)");
    rga_enabled = true;
    return true;
}

// Check if RGA hardware is available by testing /dev/rga
static bool rga_check_available(void)
{
    if (!rga_check_enabled())
    {
        rga_available_checked = true;
        rga_available = false;
        return false;
    }

    if (rga_available_checked)
    {
        return rga_available;
    }

    // Check if /dev/rga exists and is accessible
    int fd = open("/dev/rga", O_RDWR);
    if (fd < 0)
    {
        log_warn("RGA: /dev/rga not available (errno=%d), using software fallback", errno);
        rga_available = false;
    }
    else
    {
        close(fd);
        rga_available = true;
        log_info("RGA: /dev/rga is available");
    }
    rga_available_checked = true;
    return rga_available;
}

// Initialize RGA hardware for color space conversion using DMA buffers
// This works on RV1106 which has no IOMMU for RGA
static int rga_init_if_needed(uint32_t width, uint32_t height)
{
    // Fast path: if already ready with same dimensions, skip mutex entirely
    // Use memory barriers implicitly via volatile reads
    if (rga_ready && rga_output_width == width && rga_output_height == height)
    {
        return 0;
    }

    // Check if RGA hardware is available first
    if (!rga_check_available())
    {
        return -1;
    }

    pthread_mutex_lock(&rga_mutex);

    // Double-check after acquiring mutex (another thread might have initialized)
    if (rga_ready && rga_output_width == width && rga_output_height == height)
    {
        pthread_mutex_unlock(&rga_mutex);
        return 0;
    }

    // Calculate required buffer size: BGRX = 4 bytes per pixel
    size_t required_size = (size_t)width * height * 4;

    // Check if we need to reallocate (size changed or not allocated)
    bool need_realloc = (rga_output_blk == NULL ||
                         rga_output_buffer_size < required_size ||
                         rga_output_width != width ||
                         rga_output_height != height);

    if (need_realloc)
    {
        // Mark not ready during reallocation
        rga_ready = false;
        // Clean up old resources
        if (rga_output_handle != 0)
        {
            releasebuffer_handle(rga_output_handle);
            rga_output_handle = 0;
        }

        if (rga_output_blk != NULL)
        {
            RK_MPI_MB_ReleaseMB(rga_output_blk);
            rga_output_blk = NULL;
            rga_output_fd = -1;
            rga_output_vaddr = NULL;
        }

        if (rga_output_pool != MB_INVALID_POOLID)
        {
            RK_MPI_MB_DestroyPool(rga_output_pool);
            rga_output_pool = MB_INVALID_POOLID;
        }

        // Create a memory pool for RGA output buffer
        MB_POOL_CONFIG_S pool_cfg;
        memset(&pool_cfg, 0, sizeof(MB_POOL_CONFIG_S));
        pool_cfg.u64MBSize = required_size;
        pool_cfg.u32MBCnt = 1;  // Single buffer for output
        pool_cfg.enAllocType = MB_ALLOC_TYPE_DMA;
        pool_cfg.bPreAlloc = RK_TRUE;

        rga_output_pool = RK_MPI_MB_CreatePool(&pool_cfg);
        if (rga_output_pool == MB_INVALID_POOLID)
        {
            log_error("RGA: Failed to create output memory pool");
            pthread_mutex_unlock(&rga_mutex);
            return -1;
        }

        // Get a buffer from the pool
        rga_output_blk = RK_MPI_MB_GetMB(rga_output_pool, required_size, RK_TRUE);
        if (rga_output_blk == NULL)
        {
            log_error("RGA: Failed to get output buffer from pool");
            RK_MPI_MB_DestroyPool(rga_output_pool);
            rga_output_pool = MB_INVALID_POOLID;
            pthread_mutex_unlock(&rga_mutex);
            return -1;
        }

        // Get the DMA fd for the output buffer
        rga_output_fd = RK_MPI_MB_Handle2Fd(rga_output_blk);
        if (rga_output_fd < 0)
        {
            log_error("RGA: Failed to get fd from output buffer");
            RK_MPI_MB_ReleaseMB(rga_output_blk);
            rga_output_blk = NULL;
            RK_MPI_MB_DestroyPool(rga_output_pool);
            rga_output_pool = MB_INVALID_POOLID;
            pthread_mutex_unlock(&rga_mutex);
            return -1;
        }

        // Get virtual address for reading the output (to send to Go)
        rga_output_vaddr = (uint8_t *)RK_MPI_MB_Handle2VirAddr(rga_output_blk);
        if (rga_output_vaddr == NULL)
        {
            log_error("RGA: Failed to get virtual address from output buffer");
            RK_MPI_MB_ReleaseMB(rga_output_blk);
            rga_output_blk = NULL;
            rga_output_fd = -1;
            RK_MPI_MB_DestroyPool(rga_output_pool);
            rga_output_pool = MB_INVALID_POOLID;
            pthread_mutex_unlock(&rga_mutex);
            return -1;
        }

        // Import the output buffer fd into RGA
        im_handle_param_t output_param = {width, height, RK_FORMAT_BGRX_8888};
        rga_output_handle = importbuffer_fd(rga_output_fd, &output_param);
        if (rga_output_handle == 0)
        {
            log_error("RGA: Failed to import output buffer into RGA");
            RK_MPI_MB_ReleaseMB(rga_output_blk);
            rga_output_blk = NULL;
            rga_output_fd = -1;
            rga_output_vaddr = NULL;
            RK_MPI_MB_DestroyPool(rga_output_pool);
            rga_output_pool = MB_INVALID_POOLID;
            pthread_mutex_unlock(&rga_mutex);
            return -1;
        }

        rga_output_buffer_size = required_size;
        rga_output_width = width;
        rga_output_height = height;

        // Pre-compute the wrapped output buffer (avoids wrapbuffer_handle call per frame)
        rga_output_buffer = wrapbuffer_handle(rga_output_handle, width, height, RK_FORMAT_BGRX_8888);

        log_info("RGA: Allocated DMA output buffer: %zu bytes for %dx%d, fd=%d, handle=%u",
                 required_size, width, height, rga_output_fd, (unsigned)rga_output_handle);
    }

    if (!rga_initialized)
    {
        // Query RGA hardware capabilities using im2d API
        const char *version_str = querystring(RGA_VERSION);
        if (version_str != NULL && version_str[0] != '\0')
        {
            log_info("RGA: Hardware initialized (DMA mode), version: %s", version_str);
        }
        else
        {
            log_info("RGA: Hardware initialized (DMA mode)");
        }
        rga_initialized = true;
    }

    // Mark as ready for fast-path
    rga_ready = true;

    pthread_mutex_unlock(&rga_mutex);
    return 0;
}

// Cleanup RGA resources (DMA buffers and handles)
static void rga_cleanup(void)
{
    pthread_mutex_lock(&rga_mutex);

    // Release cached input handles
    for (int i = 0; i < rga_input_cache_count; i++)
    {
        if (rga_input_cache[i].handle != 0)
        {
            releasebuffer_handle(rga_input_cache[i].handle);
            rga_input_cache[i].handle = 0;
            rga_input_cache[i].fd = -1;
        }
    }
    rga_input_cache_count = 0;

    // Release RGA output handle
    if (rga_output_handle != 0)
    {
        releasebuffer_handle(rga_output_handle);
        rga_output_handle = 0;
    }

    // Release DMA buffer
    if (rga_output_blk != NULL)
    {
        RK_MPI_MB_ReleaseMB(rga_output_blk);
        rga_output_blk = NULL;
        rga_output_fd = -1;
        rga_output_vaddr = NULL;
    }

    // Destroy memory pool
    if (rga_output_pool != MB_INVALID_POOLID)
    {
        RK_MPI_MB_DestroyPool(rga_output_pool);
        rga_output_pool = MB_INVALID_POOLID;
    }

    rga_output_buffer_size = 0;
    rga_output_width = 0;
    rga_output_height = 0;
    rga_initialized = false;
    rga_ready = false;

    pthread_mutex_unlock(&rga_mutex);
    log_info("RGA: DMA resources cleaned up");
}

// Get or create a cached RGA buffer for the input fd
// Returns pointer to cached rga_buffer_t (valid until cache is modified)
// Must be called with rga_mutex held
static rga_buffer_t *rga_get_input_buffer(int fd, uint32_t width, uint32_t height)
{
    // Fast path: look for exact match (most common case)
    for (int i = 0; i < rga_input_cache_count; i++)
    {
        if (rga_input_cache[i].fd == fd)
        {
            // Found fd - check if dimensions match
            if (rga_input_cache[i].width == width && rga_input_cache[i].height == height)
            {
                return &rga_input_cache[i].buffer;
            }
            // Same fd but different dimensions (resolution change) - release old handle
            // This prevents memory leak when resolution changes
            releasebuffer_handle(rga_input_cache[i].handle);
            // Remove this entry by shifting remaining entries down
            for (int j = i + 1; j < rga_input_cache_count; j++)
            {
                rga_input_cache[j-1] = rga_input_cache[j];
            }
            rga_input_cache_count--;
            break;
        }
    }

    // Not found or removed due to dimension mismatch - need to import
    if (rga_input_cache_count >= RGA_INPUT_CACHE_SIZE)
    {
        // Cache full - release oldest entry (shouldn't happen with triple-buffering)
        log_warn("RGA: input cache full, releasing oldest entry");
        releasebuffer_handle(rga_input_cache[0].handle);
        for (int i = 1; i < rga_input_cache_count; i++)
        {
            rga_input_cache[i-1] = rga_input_cache[i];
        }
        rga_input_cache_count--;
    }

    // Import the new buffer
    im_handle_param_t src_param = {width, height, RK_FORMAT_YUYV_422};
    rga_buffer_handle_t handle = importbuffer_fd(fd, &src_param);
    if (handle == 0)
    {
        log_error("RGA: failed to import input buffer fd=%d", fd);
        return NULL;
    }

    // Cache the handle and pre-compute the wrapped buffer
    int idx = rga_input_cache_count;
    rga_input_cache[idx].fd = fd;
    rga_input_cache[idx].handle = handle;
    rga_input_cache[idx].width = width;
    rga_input_cache[idx].height = height;
    rga_input_cache[idx].buffer = wrapbuffer_handle(handle, width, height, RK_FORMAT_YUYV_422);
    rga_input_cache_count++;

    log_info("RGA: cached input handle=%u for fd=%d (%dx%d), cache_size=%d",
             (unsigned)handle, fd, width, height, rga_input_cache_count);

    return &rga_input_cache[idx].buffer;
}

// Convert YUV422 YUYV to BGRX using RGA hardware acceleration with DMA buffers
// Uses fd-based buffers which work on RV1106 (no IOMMU required)
// Returns pointer to converted BGRX data (in rga_output_vaddr), or NULL on failure
// HOT PATH - optimized for minimal overhead per frame
static uint8_t *rga_convert_yuv422_to_bgrx_fd(int yuv_fd, uint32_t width, uint32_t height, uint32_t yuv_size)
{
    // Fast validation (common case: valid input)
    if (__builtin_expect(yuv_fd < 0 || width == 0 || height == 0, 0))
    {
        return NULL;
    }

    // Initialize RGA and allocate output DMA buffer if needed
    // Has fast-path when already initialized with same dimensions
    if (__builtin_expect(rga_init_if_needed(width, height) != 0, 0))
    {
        return NULL;
    }

    pthread_mutex_lock(&rga_mutex);

    // Get pre-computed input buffer from cache (or create new entry)
    rga_buffer_t *src = rga_get_input_buffer(yuv_fd, width, height);
    if (__builtin_expect(src == NULL, 0))
    {
        pthread_mutex_unlock(&rga_mutex);
        return NULL;
    }

    // Perform color space conversion using RGA hardware
    // Use BT.601 limited range (16-235) which is standard for HDMI video
    // sync=1 means synchronous operation (wait for completion)
    // Uses pre-computed src and dst buffers to avoid wrapbuffer_handle calls
    IM_STATUS status = imcvtcolor(*src, rga_output_buffer, src->format, rga_output_buffer.format,
                                   IM_YUV_TO_RGB_BT601_LIMIT, 1);

    pthread_mutex_unlock(&rga_mutex);

    // Check for success (common case)
    if (__builtin_expect(status != IM_STATUS_SUCCESS && status != IM_STATUS_NOERROR, 0))
    {
        log_error("RGA: imcvtcolor failed with status %d: %s", status, imStrError(status));
        return NULL;
    }

    return rga_output_vaddr;
}
#endif // HAS_RGA

// Send YUV frame to Go using DMA fd (with RGA hardware conversion if available)
// This version takes a DMA fd instead of virtual address, which is required for
// RGA on RV1106 (no IOMMU)
static int send_yuv_frame_fd(int yuv_fd, uint32_t width, uint32_t height, uint32_t yuv_size)
{
    if (!rgb_running)
    {
        return -1;
    }

#ifdef HAS_RGA
    // Convert YUV422 to BGRX using RGA hardware acceleration with DMA fd
    uint8_t *bgrx_data = rga_convert_yuv422_to_bgrx_fd(yuv_fd, width, height, yuv_size);
    if (bgrx_data != NULL)
    {
        // Send the hardware-converted BGRX frame to Go
        // BGRX = 4 bytes per pixel
        size_t bgrx_size = (size_t)width * height * 4;
        video_send_rgb_frame(bgrx_data, bgrx_size, width, height);
        return 0;
    }
    // Fallback: need to map the fd to virtual address for software conversion
    log_trace("RGA conversion failed, falling back to software conversion");
#endif
    // No RGA or RGA failed: need to get virtual address from fd
    // Use RK_MPI_MMZ_Fd2Handle to get the MB handle, then get vaddr
    MB_BLK blk = RK_MPI_MMZ_Fd2Handle(yuv_fd);
    if (blk == NULL)
    {
        log_error("send_yuv_frame_fd: failed to get MB from fd=%d", yuv_fd);
        return -1;
    }
    const uint8_t *yuv_data = (const uint8_t *)RK_MPI_MB_Handle2VirAddr(blk);
    if (yuv_data == NULL)
    {
        log_error("send_yuv_frame_fd: failed to get vaddr from fd=%d", yuv_fd);
        return -1;
    }
    // Send raw YUV for software conversion in Go
    video_send_rgb_frame(yuv_data, yuv_size, width, height);
    return 0;
}

// Public RGB encoder API (uses RGA hardware for YUV422→BGRX conversion if available)
int rgb_encoder_start()
{
#ifdef HAS_RGA
    log_info("RGB encoder start requested (RGA hardware acceleration available)");
#else
    log_info("RGB encoder start requested (software conversion mode)");
#endif

    // Wait for video signal to be detected (up to 10 seconds)
    int retries = 0;
    const int max_retries = 100; // 100 * 100ms = 10 seconds
    while ((detected_width == 0 || detected_height == 0 || !detected_signal) && retries < max_retries)
    {
        if (retries == 0)
        {
            log_info("RGB: waiting for video signal...");
        }
        usleep(100000); // 100ms
        retries++;
    }

    pthread_mutex_lock(&rgb_mutex);

    if (rgb_running)
    {
        log_info("RGB encoder already running");
        pthread_mutex_unlock(&rgb_mutex);
        return 0;
    }

    if (detected_width == 0 || detected_height == 0 || !detected_signal)
    {
        log_error("Cannot start RGB encoder: no video signal (detected=%dx%d signal=%d)",
                  detected_width, detected_height, detected_signal ? 1 : 0);
        pthread_mutex_unlock(&rgb_mutex);
        return -1;
    }

    rgb_width = detected_width;
    rgb_height = detected_height;
    log_info("RGB: detected resolution %dx%d", rgb_width, rgb_height);

#ifdef HAS_RGA
    // Pre-initialize RGA buffers
    int rga_ret = rga_init_if_needed(rgb_width, rgb_height);
    if (rga_ret != 0)
    {
        log_warn("RGB: RGA initialization failed, will use software fallback");
    }
    else
    {
        log_info("RGB: RGA hardware acceleration enabled for %dx%d", rgb_width, rgb_height);
    }
#else
    log_info("RGB: Software conversion mode for %dx%d", rgb_width, rgb_height);
#endif

    // Check if video streaming is running - RGB encoder needs it to receive frames
    uint8_t streaming_status = video_get_streaming_status();
    log_debug("RGB: streaming_status=%d", streaming_status);

    if (streaming_status == 0)
    {
        log_debug("RGB: video streaming stopped, starting it");
        video_start_streaming();
        usleep(500000); // 500ms
    }

    rgb_running = true;
#ifdef HAS_RGA
    log_info("RGB encoder started: %dx%d (RGA hardware YUV422→BGRX)", rgb_width, rgb_height);
#else
    log_info("RGB encoder started: %dx%d (software YUV422→BGRX)", rgb_width, rgb_height);
#endif
    pthread_mutex_unlock(&rgb_mutex);
    return 0;
}

void rgb_encoder_stop()
{
    pthread_mutex_lock(&rgb_mutex);

    if (!rgb_running)
    {
        log_info("RGB encoder already stopped");
        pthread_mutex_unlock(&rgb_mutex);
        return;
    }

    rgb_running = false;

    log_info("RGB encoder stopped");
    pthread_mutex_unlock(&rgb_mutex);

#ifdef HAS_RGA
    // Clean up RGA resources when encoder stops
    rga_cleanup();
#endif
}

bool rgb_encoder_is_running()
{
    return rgb_running;
}

// Query RGA hardware status for diagnostics
const char *rga_get_status(void)
{
    static char status_buf[256];
#ifdef HAS_RGA
    pthread_mutex_lock(&rga_mutex);
    snprintf(status_buf, sizeof(status_buf),
             "RGA: initialized=%s, buffer=%zu bytes, fd=%d, handle=%u, running=%s",
             rga_initialized ? "yes" : "no",
             rga_output_buffer_size,
             rga_output_fd,
             (unsigned)rga_output_handle,
             rgb_running ? "yes" : "no");
    pthread_mutex_unlock(&rga_mutex);
#else
    snprintf(status_buf, sizeof(status_buf),
             "RGA: not available (software conversion), running=%s",
             rgb_running ? "yes" : "no");
#endif
    return status_buf;
}
