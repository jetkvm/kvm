package kvm

import (
	"github.com/jetkvm/kvm/internal/camera"
	"github.com/pion/webrtc/v4"
)

var cameraManager *camera.Manager

// initCameraManager initializes the camera manager.
// Must be called after gadget is initialized.
func initCameraManager() {
	cameraManager = camera.NewManager(camera.Config{
		UVCLogger:    uvcLog,
		CameraLogger: cameraLog,
		Gadget:       gadget,
		Native:       nil, // Set later when native instance is available
	})
}

// setCameraEnabled enables or disables camera passthrough.
func setCameraEnabled(enabled bool) {
	if cameraManager != nil {
		cameraManager.SetEnabled(enabled)
	}
}

// isCameraEnabled returns whether camera passthrough is enabled.
func isCameraEnabled() bool {
	if cameraManager != nil {
		return cameraManager.IsEnabled()
	}
	return false
}

// setUVCSource sets the video source for UVC output.
func setUVCSource(source camera.Source) {
	if cameraManager != nil {
		cameraManager.SetSource(source)
	}
}

// getUVCSource returns the current UVC video source.
func getUVCSource() camera.Source {
	if cameraManager != nil {
		return cameraManager.GetSource()
	}
	return camera.SourceHDMI
}

// handleCameraChannel handles the camera DataChannel for JPEG passthrough.
// Browser captures camera frames, encodes to JPEG, and sends over this channel.
// We simply pass the JPEG frames directly to the UVC streamer (no transcoding).
func handleCameraChannel(d *webrtc.DataChannel) {
	if cameraManager == nil {
		return
	}

	d.OnOpen(func() {
		if id := d.ID(); id != nil {
			cameraManager.OnChannelOpen(*id)
		}
	})

	d.OnClose(func() {
		cameraManager.OnChannelClose()
	})

	d.OnMessage(func(msg webrtc.DataChannelMessage) {
		cameraManager.HandleCameraFrame(msg.Data, msg.IsString)
	})
}
