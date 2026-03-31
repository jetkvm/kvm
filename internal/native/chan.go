package native

import (
	"time"

	"github.com/rs/zerolog"
)

type timestampedFrame struct {
	data      []byte
	timestamp time.Time
}

var (
	videoFrameChan chan timestampedFrame = make(chan timestampedFrame)
	videoStateChan chan VideoState       = make(chan VideoState)
	logChan        chan nativeLogMessage = make(chan nativeLogMessage)
	indevEventChan chan int              = make(chan int)
	rpcEventChan   chan string           = make(chan string)
)

func (n *Native) handleVideoFrameChan() {
	var lastTimestamp time.Time
	for {
		frame := <-videoFrameChan
		var duration time.Duration
		if !lastTimestamp.IsZero() {
			duration = frame.timestamp.Sub(lastTimestamp)
		}
		lastTimestamp = frame.timestamp
		n.onVideoFrameReceived(frame.data, duration)
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
