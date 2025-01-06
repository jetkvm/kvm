package kvm

import (
	"encoding/json"
	"log"
)

// max frame size for 1080p video, specified in mpp venc setting
const MaxFrameSize = 1920 * 1080 / 2

func writeCtrlAction(action string) error {
	actionMessage := map[string]string{
		"action": action,
	}
	jsonMessage, err := json.Marshal(actionMessage)
	if err != nil {
		return err
	}
	err = WriteCtrlMessage(jsonMessage)
	return err
}

type VideoInputState struct {
	Ready          bool    `json:"ready"`
	Error          string  `json:"error,omitempty"` //no_signal, no_lock, out_of_range
	Width          int     `json:"width"`
	Height         int     `json:"height"`
	FramePerSecond float64 `json:"fps"`
}

var LastVideoState VideoInputState

func TriggerVideoStateUpdate() {
	go func() {
		WriteJSONRPCEvent("videoInputState", LastVideoState, CurrentSession)
	}()
}
func HandleVideoStateMessage(event CtrlResponse) {
	videoState := VideoInputState{}
	err := json.Unmarshal(event.Data, &videoState)
	if err != nil {
		log.Println("Error parsing video state json:", err)
		return
	}
	LastVideoState = videoState
	TriggerVideoStateUpdate()
	RequestDisplayUpdate()
}

func rpcGetVideoState() (VideoInputState, error) {
	return LastVideoState, nil
}
