package audio

import "io"

const (
	CaptureSampleRate    = 48000
	CaptureChannels      = 2
	CaptureFrameSize     = 960
	PCMUFrameSize        = 160
	G722InputSampleRate  = 16000
	G722InputFrameSize   = 320
	G722EncodedFrameSize = 160
)

type Codec int

const (
	CodecPCMU Codec = iota
	CodecG722
)

func (c Codec) String() string {
	switch c {
	case CodecG722:
		return "G722"
	case CodecPCMU:
		return "PCMU"
	default:
		return "unknown"
	}
}

// ErrNoAudioData is returned when the underlying capture device is idle.
// The caller should retry; it is not a fatal error.
var ErrNoAudioData = io.ErrNoProgress
