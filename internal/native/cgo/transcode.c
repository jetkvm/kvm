/*
 * H.264 to MJPEG hardware transcoder - STUB implementation
 *
 * RV1106 SoC does NOT have hardware VDEC (video decoder), so on-device
 * transcoding from H.264 to MJPEG is not possible.
 *
 * For camera passthrough, the browser encodes directly in the format
 * requested by the UVC host (either MJPEG or H.264), eliminating the
 * need for on-device transcoding.
 */

#include <stdbool.h>
#include <stdint.h>
#include <stddef.h>
#include <sys/types.h>
#include "log.h"

int transcode_init(int width, int height)
{
    (void)width;
    (void)height;
    log_warn("Transcoder not available: RV1106 has no hardware VDEC");
    return -1;
}

int transcode_start()
{
    log_warn("Transcoder not available: RV1106 has no hardware VDEC");
    return -1;
}

void transcode_stop()
{
    // No-op
}

void transcode_shutdown()
{
    // No-op
}

int transcode_send_h264_frame(const uint8_t *frame, ssize_t len)
{
    (void)frame;
    (void)len;
    return -1;
}

bool transcode_is_running()
{
    return false;
}
