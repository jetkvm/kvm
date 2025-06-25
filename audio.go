package kvm

import (
	"os/exec"
)

func runAudioClient() (cmd *exec.Cmd, err error) {
	return startNativeBinary("/userdata/jetkvm/bin/jetkvm_audio")
}

func StartAudioServer() {
	nativeAudioSocketListener = StartNativeSocketServer("/var/run/jetkvm_audio.sock", handleAudioClient, false)
	nativeLogger.Debug().Msg("native app audio sock started")
}
