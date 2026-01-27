package native

import (
	"sync/atomic"
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
//
// Memory management: Data points to a pooled buffer. Call Release() when done
// processing the frame to return the buffer to the pool.
type RGBFrame struct {
	Data   []byte
	Width  uint32
	Height uint32
	Format RGBFrameFormat
	pooled bool // true if Data is from the pool and should be released
}

// Release returns the frame's buffer to the pool.
// Must be called after the frame data is no longer needed.
// Safe to call multiple times or on non-pooled frames.
func (f *RGBFrame) Release() {
	if f.pooled && f.Data != nil {
		rgbFrameBufferPool.release(f.Data)
		f.Data = nil
		f.pooled = false
	}
}

// rgbFrameBufferPool is a bounded pool of pre-allocated frame buffers.
// This prevents OOM by limiting the number of concurrent frame allocations.
// Max 3 buffers: 1 being filled by native, 1 in channel, 1 being processed.
var rgbFrameBufferPool = newBoundedFramePool(3, 1920*1080*4) // 8MB per buffer for 1080p BGRX

// boundedFramePool is a fixed-size pool of frame buffers.
// Unlike sync.Pool, this has a hard limit on concurrent buffers.
type boundedFramePool struct {
	buffers chan []byte
	size    int
	dropped atomic.Uint64 // count of frames dropped due to no available buffer
}

func newBoundedFramePool(maxBuffers, bufSize int) *boundedFramePool {
	p := &boundedFramePool{
		buffers: make(chan []byte, maxBuffers),
		size:    bufSize,
	}
	// Pre-allocate all buffers
	for i := 0; i < maxBuffers; i++ {
		p.buffers <- make([]byte, bufSize)
	}
	return p
}

// acquire tries to get a buffer from the pool.
// Returns nil if all buffers are in use (caller should drop the frame).
func (p *boundedFramePool) acquire() []byte {
	select {
	case buf := <-p.buffers:
		return buf
	default:
		p.dropped.Add(1)
		return nil
	}
}

// release returns a buffer to the pool.
func (p *boundedFramePool) release(buf []byte) {
	select {
	case p.buffers <- buf:
	default:
		// Pool full - shouldn't happen with bounded pool, but don't leak
	}
}

// DroppedFrames returns the count of frames dropped due to buffer exhaustion.
func (p *boundedFramePool) DroppedFrames() uint64 {
	return p.dropped.Load()
}

var (
	videoFrameChan chan []byte           = make(chan []byte)
	jpegFrameChan  chan []byte           = make(chan []byte, 2)   // Buffered for non-blocking JPEG delivery
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

func (n *Native) handleJpegFrameChan() {
	for frame := range jpegFrameChan {
		n.onJpegFrameReceived(frame)
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
