#ifndef JETKVM_VIDEO_BITRATE_H
#define JETKVM_VIDEO_BITRATE_H

#include <stdint.h>

typedef struct {
    uint32_t target;
    uint32_t maximum;
} video_bitrate;

// Encoder rates are in kbit/s; REMB is in bit/s. Auto (factor 0) uses
// High's resolution-scaled ceiling, including its existing VBR headroom.
static inline video_bitrate video_bitrate_for(float factor, int width, int height, uint32_t remb)
{
    int base = 512 + (int)((4000 - 512) * (factor == 0 ? 1 : factor));
    uint32_t target = (uint32_t)(base * ((double)width * height / (1920.0 * 1080.0)));
    if (target < 200) target = 200;
    uint32_t maximum = target * 3 / 2;
    if (factor == 0 && remb != 0) {
        uint32_t limit = remb / 1000;
        // Keep the encoder usable even on paths below its supported floor.
        if (limit < 200) limit = 200;
        if (maximum > limit) {
            maximum = limit;
            target = maximum * 2 / 3;
            if (target < 200) target = 200;
        }
    }
    return (video_bitrate){target, maximum};
}

#endif
