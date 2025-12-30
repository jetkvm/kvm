package kvm

// initUVC initializes UVC streaming for camera passthrough.
func initUVC() {
	if cameraManager == nil {
		return
	}
	cameraManager.InitUVC(config.UsbDevices.UVC)
}

// reinitUVC reinitializes UVC if needed.
func reinitUVC() {
	if cameraManager == nil {
		return
	}
	cameraManager.ReinitUVC(config.UsbDevices.UVC)
}
