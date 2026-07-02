//go:build cgo

package native

/*
#include "cgo/video_limits.h"
*/
import "C"

// videoPixelClockOutOfRange reports whether a detected HDMI timing exceeds the
// pixel clock the capture chip can handle. It mirrors the check used by the
// native format detection loop so the limit can be tested without hardware.
func videoPixelClockOutOfRange(pixelClock uint64) bool {
	return bool(C.jetkvm_video_pixel_clock_out_of_range(C.uint64_t(pixelClock)))
}
