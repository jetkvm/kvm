#include <assert.h>
#include "../cgo/video_bitrate.h"

int main(void)
{
    video_bitrate high = video_bitrate_for(1, 1920, 1080, 0);
    assert(high.target == 4000 && high.maximum == 6000);
    video_bitrate automatic = video_bitrate_for(0, 1920, 1080, 0);
    assert(automatic.target == high.target && automatic.maximum == high.maximum);
    automatic = video_bitrate_for(0, 1920, 1080, UINT32_MAX);
    assert(automatic.target == high.target && automatic.maximum == high.maximum);
    /* The gated limit is the target; VBR headroom stays on top of it. */
    automatic = video_bitrate_for(0, 1920, 1080, 1500000);
    assert(automatic.target == 1500 && automatic.maximum == 2250);
    automatic = video_bitrate_for(0, 1920, 1080, 1);
    assert(automatic.target == 200 && automatic.maximum == 300);
    video_bitrate medium = video_bitrate_for(.5f, 1920, 1080, 100000);
    assert(medium.target == 2256 && medium.maximum == 3384);
    video_bitrate low = video_bitrate_for(.1f, 1920, 1080, 100000);
    assert(low.target == 860 && low.maximum == 1290);
    automatic = video_bitrate_for(0, 1280, 720, UINT32_MAX);
    high = video_bitrate_for(1, 1280, 720, 0);
    assert(automatic.target == 1777 && automatic.maximum == high.maximum);
    automatic = video_bitrate_for(0, 640, 480, 500000);
    assert(automatic.target == 500 && automatic.maximum == 750);
    return 0;
}
