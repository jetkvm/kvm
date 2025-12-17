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

// handleMjpegFrame handles an MJPEG frame from the native encoder.
func handleMjpegFrame(frame []byte) {
	if cameraManager == nil {
		return
	}
	cameraManager.HandleMjpegFrame(frame)
}
