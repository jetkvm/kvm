#ifndef JETKVM_VIDEO_BITRATE_H
#define JETKVM_VIDEO_BITRATE_H

#include <stdint.h>

typedef struct {
    uint32_t target;
    uint32_t maximum;
} video_bitrate;

// Encoder rates are in kbit/s; limit is in bit/s. Auto (factor 0) uses
// High's resolution-scaled rates, including the existing VBR headroom, and
// caps the target at the limit the REMB gate derived from receiver feedback
// (video_remb_gate.h). Zero means no limit. Manual presets ignore it.
static inline video_bitrate video_bitrate_for(float factor, int width, int height, uint32_t limit)
{
    int base = 512 + (int)((4000 - 512) * (factor == 0 ? 1 : factor));
    uint32_t target = (uint32_t)(base * ((double)width * height / (1920.0 * 1080.0)));
    if (target < 200) target = 200;
    if (factor == 0 && limit != 0 && limit / 1000 < target) {
        target = limit / 1000;
        // Keep the encoder usable even on paths below its supported floor.
        if (target < 200) target = 200;
    }
    uint32_t maximum = target * 3 / 2;
    return (video_bitrate){target, maximum};
}

#endif
