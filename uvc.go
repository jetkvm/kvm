package kvm

// initUVC initializes UVC streaming.
func initUVC() {
	if cameraManager == nil {
		return
	}
	cameraManager.InitUVC(config.UsbDevices.UVC)
}

// stopUVC stops UVC streaming.
func stopUVC() {
	if cameraManager == nil {
		return
	}
	cameraManager.StopUVC()
}

// reinitUVC reinitializes UVC if needed.
func reinitUVC() {
	if cameraManager == nil {
		return
	}
	cameraManager.ReinitUVC(config.UsbDevices.UVC)
}

// handleH264Frame handles an H.264 frame from the native encoder for UVC streaming.
// This routes H.264 frames to the UVC gadget when source is HDMI.
func handleH264FrameForUVC(frame []byte) {
	if cameraManager == nil {
		return
	}
	cameraManager.HandleH264Frame(frame)
}

// handleMjpegFrameForUVC handles an MJPEG frame from the native encoder for UVC streaming.
// This routes MJPEG frames to the UVC gadget when MJPEG format is selected by the host.
func handleMjpegFrameForUVC(frame []byte) {
	if cameraManager == nil {
		return
	}
	cameraManager.HandleMjpegFrame(frame)
}

// restoreUVCMjpegState restores MJPEG encoder state after native restart.
// Called from OnNativeRestart callback to re-enable MJPEG encoding if UVC was streaming.
func restoreUVCMjpegState() {
	if cameraManager == nil {
		return
	}
	cameraManager.RestoreMjpegState()
}
