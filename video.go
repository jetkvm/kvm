package kvm

import (
	"github.com/jetkvm/kvm/internal/native"
)

var lastVideoState native.VideoState

func triggerVideoStateUpdate() {
	go func() {
		writeJSONRPCEvent("videoInputState", lastVideoState, currentSession)
	}()

	nativeLogger.Info().Interface("state", lastVideoState).Msg("video state updated")
}

func rpcGetVideoState() (native.VideoState, error) {
	return lastVideoState, nil
}
