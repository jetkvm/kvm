package kvm

// initUVC initializes UVC streaming for camera passthrough.
func initUVC() {
	if cameraManager == nil {
		return
	}
	if err := cameraManager.InitUVC(config.UsbDevices.UVC); err != nil {
		uvcLog.Warn().Err(err).Msg("UVC initialization failed (camera passthrough unavailable)")
	}
}

// reinitUVC reinitializes UVC if needed.
func reinitUVC() {
	if cameraManager == nil {
		return
	}
	cameraManager.ReinitUVC(config.UsbDevices.UVC)
}
