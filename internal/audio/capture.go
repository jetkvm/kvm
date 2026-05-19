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

type Reader interface {
	ReadEncoded(codec Codec) ([]byte, error)
	Close() error
}

type unavailableCapture struct {
	err error
}

func (c *unavailableCapture) ReadEncoded(Codec) ([]byte, error) {
	return nil, c.err
}

func (c *unavailableCapture) Close() error {
	return nil
}

var ErrNoAudioData = io.ErrNoProgress
