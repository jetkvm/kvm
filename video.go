package kvm

import "github.com/jetkvm/kvm/internal/native"

// max frame size for 1080p video, specified in mpp venc setting
const maxFrameSize = 1920 * 1080 / 2

func writeCtrlAction(action string) error {
	return nil
}

var lastVideoState native.VideoState

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

func rpcGetVideoState() (native.VideoState, error) {
	return lastVideoState, nil
}
