package native

import (
	"sync"
	"time"

	"github.com/rs/zerolog"
)

var (
	videoFrameChan chan []byte           = make(chan []byte)
	jpegFrameChan  chan []byte           = make(chan []byte, 2) // Buffered for non-blocking JPEG delivery
	videoStateChan chan VideoState       = make(chan VideoState)
	logChan        chan nativeLogMessage = make(chan nativeLogMessage)
	indevEventChan chan int              = make(chan int)
	rpcEventChan   chan string           = make(chan string)

	// H.264 frame subscribers for VNC
	h264Subscribers     = make(map[chan<- []byte]struct{})
	h264SubscriberMutex sync.RWMutex
)

func (n *Native) handleVideoFrameChan() {
	lastFrame := time.Now()
	for {
		frame := <-videoFrameChan
		now := time.Now()
		sinceLastFrame := now.Sub(lastFrame)
		lastFrame = now

		// Broadcast H.264 frame to VNC and other subscribers
		broadcastH264Frame(frame)

		// Call the original callback (WebRTC)
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

// SubscribeH264Frames adds a channel to receive H.264 frames
// Returns an unsubscribe function that should be called when done
func SubscribeH264Frames(ch chan<- []byte) func() {
	h264SubscriberMutex.Lock()
	h264Subscribers[ch] = struct{}{}
	h264SubscriberMutex.Unlock()

	return func() {
		UnsubscribeH264Frames(ch)
	}
}

// UnsubscribeH264Frames removes a channel from H.264 frame subscribers
func UnsubscribeH264Frames(ch chan<- []byte) {
	h264SubscriberMutex.Lock()
	delete(h264Subscribers, ch)
	h264SubscriberMutex.Unlock()
}

// broadcastH264Frame sends an H.264 frame to all subscribers (non-blocking)
func broadcastH264Frame(frame []byte) {
	h264SubscriberMutex.RLock()
	defer h264SubscriberMutex.RUnlock()

	for ch := range h264Subscribers {
		// Non-blocking send - drop frame if channel is full
		select {
		case ch <- frame:
		default:
		}
	}
}
