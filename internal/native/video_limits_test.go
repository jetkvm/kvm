//go:build cgo

package native

import "testing"

func TestVideoPixelClockLimit(t *testing.T) {
	tests := []struct {
		name       string
		pixelClock uint64
		outOfRange bool
	}{
		// These modes fit the capture chip even though 1920x1200 and
		// 1280x1440 have more lines than 1080p.
		{name: "1920x1080@60 stays ready", pixelClock: 148500000},
		{name: "1920x1200@60 stays ready", pixelClock: 154000000},
		{name: "1280x1440@60 stays ready", pixelClock: 127750000},
		{name: "1280x720@120 stays ready", pixelClock: 148500000},
		{name: "165MHz stays ready", pixelClock: 165000000},
		// These modes exceed the ~165 MHz pixel clock limit of the chip.
		{name: "2560x1440@60 is out of range", pixelClock: 241500000, outOfRange: true},
		{name: "3840x2160@30 is out of range", pixelClock: 297000000, outOfRange: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := videoPixelClockOutOfRange(tt.pixelClock)
			if got != tt.outOfRange {
				t.Errorf("pixel clock %d Hz: out of range = %v, want %v", tt.pixelClock, got, tt.outOfRange)
			}
		})
	}
}
