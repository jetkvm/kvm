#ifndef VIDEO_LIMITS_H
#define VIDEO_LIMITS_H

#include <stdbool.h>
#include <stdint.h>

// The tc358743 capture chip accepts pixel clocks up to about 165 MHz. Modes
// above that limit (for example 2560x1440@60 at ~241 MHz or 3840x2160@30 at
// ~297 MHz) cannot be captured even when the frame would fit a capture buffer.
#define JETKVM_VIDEO_MAX_PIXEL_CLOCK 165000000ULL

static inline bool jetkvm_video_pixel_clock_out_of_range(uint64_t pixelclock)
{
    return pixelclock > JETKVM_VIDEO_MAX_PIXEL_CLOCK;
}

#endif // VIDEO_LIMITS_H
