package native

import (
	"time"

	"github.com/rs/zerolog"
)

// RGBFrameFormat indicates the pixel format of the frame data
type RGBFrameFormat int

const (
	// RGBFrameFormatYUV422 indicates YUV422 YUYV format (needs software conversion)
	RGBFrameFormatYUV422 RGBFrameFormat = iota
	// RGBFrameFormatBGRX indicates BGRX format (ready to use, from RGA hardware)
	RGBFrameFormatBGRX
)

// RGBFrame represents a video frame for RDP bitmap mode.
// When RGA hardware acceleration is available, Format will be RGBFrameFormatBGRX
// and Data contains ready-to-use BGRX pixels. Otherwise, Format is RGBFrameFormatYUV422
// and Data needs software conversion.
type RGBFrame struct {
	Data   []byte
	Width  uint32
	Height uint32
	Format RGBFrameFormat
}

var (
	videoFrameChan chan []byte           = make(chan []byte)
	jpegFrameChan  chan []byte           = make(chan []byte, 2) // Buffered for non-blocking JPEG delivery
	rgbFrameChan   chan RGBFrame         = make(chan RGBFrame, 2) // Buffered for non-blocking RGB delivery
	videoStateChan chan VideoState       = make(chan VideoState)
	logChan        chan nativeLogMessage = make(chan nativeLogMessage)
	indevEventChan chan int              = make(chan int)
	rpcEventChan   chan string           = make(chan string)
)

func (n *Native) handleVideoFrameChan() {
	lastFrame := time.Now()
	for {
		frame := <-videoFrameChan
		now := time.Now()
		sinceLastFrame := now.Sub(lastFrame)
		lastFrame = now

		n.onVideoFrameReceived(frame, sinceLastFrame)
	}
}

func (n *Native) handleVideoStateChan() {
	for {
		state := <-videoStateChan

		n.onVideoStateChange(state)
	}
}

func (n *Native) handleLogChan() {
	for {
		entry := <-logChan
		l := n.l.With().
			Str("file", entry.File).
			Str("func", entry.FuncName).
			Int("line", entry.Line).
			Logger()

		switch entry.Level {
		case zerolog.DebugLevel:
			l.Debug().Msg(entry.Message)
		case zerolog.InfoLevel:
			l.Info().Msg(entry.Message)
		case zerolog.WarnLevel:
			l.Warn().Msg(entry.Message)
		case zerolog.ErrorLevel:
			l.Error().Msg(entry.Message)
		case zerolog.PanicLevel:
			l.Panic().Msg(entry.Message)
		case zerolog.FatalLevel:
			l.Fatal().Msg(entry.Message)
		case zerolog.TraceLevel:
			l.Trace().Msg(entry.Message)
		case zerolog.NoLevel:
			l.Info().Msg(entry.Message)
		default:
			l.Info().Msg(entry.Message)
		}
	}
}

func (n *Native) handleIndevEventChan() {
	for {
		event := <-indevEventChan
		name := uiEventCodeToName(event)
		n.onIndevEvent(name)
	}
}

func (n *Native) handleRpcEventChan() {
	for {
		event := <-rpcEventChan
		n.onRpcEvent(event)
	}
}

func (n *Native) handleRGBFrameChan() {
	for {
		frame := <-rgbFrameChan
		n.onRGBFrameReceived(frame)
	}
}
