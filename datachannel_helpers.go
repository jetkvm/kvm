package kvm

import "github.com/pion/webrtc/v4"

func handlePermissionDeniedChannel(d *webrtc.DataChannel, message string) {
	d.OnOpen(func() {
		d.SendText(message + "\r\n")
		d.Close()
	})
	d.OnMessage(func(msg webrtc.DataChannelMessage) {})
}
