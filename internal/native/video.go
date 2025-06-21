package native

import "fmt"

type VideoState struct {
	Ready          bool    `json:"ready"`
	Error          string  `json:"error,omitempty"` //no_signal, no_lock, out_of_range
	Width          int     `json:"width"`
	Height         int     `json:"height"`
	FramePerSecond float64 `json:"fps"`
}

func (n *Native) handleVideoStateMessage(state VideoState) {
	nativeLogger.Info().Msg("video state handler")
	nativeLogger.Info().Msg(fmt.Sprintf("state: %+v", state))
}
