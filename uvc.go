package kvm

// initUVC initializes UVC streaming for camera passthrough.
func initUVC() {
	mgr := cameraManagerPtr.Load()
	if mgr == nil {
		return
	}
	if err := mgr.InitUVC(config.UsbDevices.UVC); err != nil {
		uvcLog.Warn().Err(err).Msg("UVC initialization failed (camera passthrough unavailable)")
	}
}

// reinitUVC reinitializes UVC if needed.
func reinitUVC() {
	mgr := cameraManagerPtr.Load()
	if mgr == nil {
		return
	}
	mgr.ReinitUVC(config.UsbDevices.UVC)
}
