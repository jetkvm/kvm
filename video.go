package kvm

// max frame size for 1080p video, specified in mpp venc setting
const maxFrameSize = 1920 * 1080 / 2

func writeCtrlAction(action string) error {
	return nil
}

type VideoInputState struct {
	Ready          bool    `json:"ready"`
	Error          string  `json:"error,omitempty"` //no_signal, no_lock, out_of_range
	Width          int     `json:"width"`
	Height         int     `json:"height"`
	FramePerSecond float64 `json:"fps"`
}

var lastVideoState VideoInputState

func triggerVideoStateUpdate() {
	go func() {
		writeJSONRPCEvent("videoInputState", lastVideoState, currentSession)
	}()
}

// func HandleVideoStateMessage(event CtrlResponse) {
// 	videoState := VideoInputState{}
// 	err := json.Unmarshal(event.Data, &videoState)
// 	if err != nil {
// 		logger.Warn().Err(err).Msg("Error parsing video state json")
// 		return
// 	}
// 	lastVideoState = videoState
// 	triggerVideoStateUpdate()
// 	requestDisplayUpdate(true)
// }

func rpcGetVideoState() (VideoInputState, error) {
	return lastVideoState, nil
}
