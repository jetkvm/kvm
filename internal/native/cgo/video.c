#define _POSIX_C_SOURCE 200809L
#include <unistd.h>
#include <time.h>
#include <rk_type.h>
#include <rk_mpi_venc.h>
#include <rk_mpi_sys.h>
#include <string.h>
#include <rk_debug.h>
#include <malloc.h>
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

#define VIDEO_DEV "/dev/video0"
#define SUB_DEV "/dev/v4l-subdev2"

#define RK_ALIGN(x, a) (((x) + (a)-1) & ~((a)-1))
#define RK_ALIGN_2(x) RK_ALIGN(x, 2)
#define RK_ALIGN_16(x) RK_ALIGN(x, 16)
#define RK_ALIGN_32(x) RK_ALIGN(x, 32)

int sub_dev_fd = -1;
#define VENC_CHANNEL 0
MB_POOL memPool = MB_INVALID_POOLID;

bool should_exit = false;
float quality_factor = 1.0f;

static void *venc_read_stream(void *arg);

RK_U64 get_us()
{
    struct timespec time = {0, 0};
    clock_gettime(CLOCK_MONOTONIC, &time);
    return (RK_U64)time.tv_sec * 1000000 + (RK_U64)time.tv_nsec / 1000; /* microseconds */
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

int video_init()
{
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
        // printf("RK_MPI_VENC_GetStream\n");
        s32Ret = RK_MPI_VENC_GetStream(VENC_CHANNEL, &stFrame, 200); // blocks max 200ms
        if (s32Ret == RK_SUCCESS)
        {
            RK_U64 nowUs = get_us();
            // printf("chn:0, loopCount:%d enc->seq:%d wd:%d pts=%llu delay=%lldus\n",
            //        loopCount, stFrame.u32Seq, stFrame.pstPack->u32Len,
            //        stFrame.pstPack->u64PTS, nowUs - stFrame.pstPack->u64PTS);
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

uint32_t detected_width, detected_height;
bool detected_signal = false, streaming_flag = false;

pthread_t *streaming_thread = NULL;

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

    while (streaming_flag)
    {
        if (detected_signal == false)
        {
            usleep(100000);
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
            perror("Set format fail");
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
            perror("VIDIOC_REQBUFS failed");
            return errno;
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
                perror("VIDIOC_QUERYBUF failed");
                req.count = i;
                return errno;
            }
            printf("VIDIOC_QUERYBUF successful for buffer %d\n", i);

            printf("plane: length = %d\n", planes_buffer->length);
            printf("plane: offset = %d\n", planes_buffer->m.mem_offset);

            MB_BLK blk = RK_MPI_MB_GetMB(memPool, (planes_buffer)->length, RK_TRUE);
            if (blk == NULL)
            {
                RK_LOGE("get mb blk failed!");
                return -1;
            }
            printf("Got memory block for buffer %d\n", i);

            buffers[i].mb_blk = blk;

            RK_S32 buf_fd = (RK_MPI_MB_Handle2Fd(blk));
            if (buf_fd < 0)
            {
                RK_LOGE("RK_MPI_MB_Handle2Fd failed!");
                return -1;
            }
            printf("Converted memory block to file descriptor for buffer %d\n", i);
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
                perror("VIDIOC_QBUF failed");
                return errno;
            }
            printf("VIDIOC_QBUF successful for buffer %d\n", i);
        }

        if (ioctl(video_dev_fd, VIDIOC_STREAMON, &type) < 0)
        {
            perror("VIDIOC_STREAMON failed");
            goto cleanup;
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
                log_info("select timeout \n");
                break;
            }
            if (r == -1)
            {
                if (errno == EINTR)
                {
                    continue;
                }
                perror("select in video streaming");
                break;
            }
            memset(&buf, 0, sizeof(buf));
            buf.type = V4L2_BUF_TYPE_VIDEO_CAPTURE_MPLANE;
            buf.memory = V4L2_MEMORY_DMABUF;
            buf.m.planes = &tmp_plane;
            buf.length = 1;
            if (ioctl(video_dev_fd, VIDIOC_DQBUF, &buf) < 0)
            {
                perror("VIDIOC_DQBUF failed");
                break;
            }
            // printf("got frame, bytesused = %d\n", tmp_plane.bytesused);
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
        // if (num == 100) {
        //     RK_VOID *pData = RK_MPI_MB_Handle2VirAddr(stFrame.stVFrame.pMbBlk);
        //     if (pData) {
        //         size_t frameSize = tmp_plane.bytesused; // Use the actual size reported by the driver
        //         write_buffer_to_file(pData, frameSize, "/userdata/banana.raw");
        //         printf("Frame 100 written to /userdata/banana.raw\n");
        //     } else {
        //         printf("Failed to get virtual address for frame 100\n");
        //     }
        // }
        retry_send_frame:
            if (RK_MPI_VENC_SendFrame(VENC_CHANNEL, &stFrame, 2000) != RK_SUCCESS)
            {
                if (retried == true)
                {
                    RK_LOGE("RK_MPI_VENC_SendFrame retry failed");
                }
                else
                {
                    RK_LOGE("RK_MPI_VENC_SendFrame failed,retrying");
                    retried = true;
                    usleep(1000llu);
                    goto retry_send_frame;
                }
            }

            num++;

            if (ioctl(video_dev_fd, VIDIOC_QBUF, &buf) < 0)
                printf("failture VIDIOC_QBUF\n");
        }
    cleanup:
        if (ioctl(video_dev_fd, VIDIOC_STREAMOFF, &type) < 0)
        {
            perror("VIDIOC_STREAMOFF failed");
        }

        venc_stop();

        for (int i = 0; i < input_buffer_count; i++)
        {
            if (buffers[i].mb_blk != NULL)
            {
                RK_MPI_MB_ReleaseMB((buffers + i)->mb_blk);
            }
        }

        close(video_dev_fd);
    }

    return NULL;
}

void video_shutdown()
{
    if (should_exit == true)
    {
        printf("shutting down in progress already\n");
        return;
    }
    video_stop_streaming();
    // if (buffers != NULL) {
    //     for (int i = 0; i < input_buffer_count; i++) {
    //         if ((buffers + i)->mb_blk != NULL) {
    //             RK_MPI_MB_ReleaseMB((buffers + i)->mb_blk);
    //         }
    //         free((buffers + i)->planes_buffer);
    //     }
    //     free(buffers);
    // }
    should_exit = true;
    if (sub_dev_fd > 0)
    {
        shutdown(sub_dev_fd, SHUT_RDWR);
        // close(sub_dev_fd);
        printf("Closed sub_dev_fd\n");
    }

    if (memPool != MB_INVALID_POOLID)
    {
        RK_MPI_MB_DestroyPool(memPool);
    }
    printf("Destroyed memory pool\n");
    // if (format_thread != NULL) {
    //     pthread_join(*format_thread, NULL);
    //     free(format_thread);
    //     format_thread = NULL;
    // }
    // printf("Joined format detection thread\n");
}

// TODO: mutex?

void video_start_streaming()
{
    if (streaming_thread != NULL)
    {
        log_info("video streaming already started");
        return;
    }
    streaming_thread = malloc(sizeof(pthread_t));
    assert(streaming_thread != NULL);
    streaming_flag = true;
    pthread_create(streaming_thread, NULL, run_video_stream, NULL);
}

void video_stop_streaming()
{
    if (streaming_thread != NULL)
    {
        streaming_flag = false;
        pthread_join(*streaming_thread, NULL);
        free(streaming_thread);
        streaming_thread = NULL;
        log_info("video streaming stopped");
    }
}

void *run_detect_format(void *arg)
{
    struct v4l2_event_subscription sub;
    struct v4l2_event ev;
    struct v4l2_dv_timings dv_timings;

    memset(&sub, 0, sizeof(sub));
    sub.type = V4L2_EVENT_SOURCE_CHANGE;
    if (ioctl(sub_dev_fd, VIDIOC_SUBSCRIBE_EVENT, &sub) == -1)
    {
        log_error("cannot subscribe to event");
        perror("Cannot subscribe to event");
        goto exit;
    }

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
                printf("HDMI status: out of range\n");
                video_report_format(false, "out_of_range", 0, 0, 0);
            }
            else
            {
                perror("error VIDIOC_QUERY_DV_TIMINGS");
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
            detected_width = dv_timings.bt.width;
            detected_height = dv_timings.bt.height;
            detected_signal = true;
            video_report_format(true, NULL, detected_width, detected_height, frames_per_second);
            if (streaming_flag == true)
            {
                log_info("restarting on going video streaming");
                video_stop_streaming();
                video_start_streaming();
            }
        }

        memset(&ev, 0, sizeof(ev));
        if (ioctl(sub_dev_fd, VIDIOC_DQEVENT, &ev) != 0)
        {
            log_error("failed to VIDIOC_DQEVENT");
            perror("failed to VIDIOC_DQEVENT");
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

    if (streaming_flag == true)
    {
        log_info("restarting on going video streaming due to quality factor change");
        video_stop_streaming();
        video_start_streaming();
    }
}

float video_get_quality_factor() {
    return quality_factor;
}