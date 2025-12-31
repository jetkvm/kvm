package kvm

// initUVC initializes UVC streaming for camera passthrough.
func initUVC() {
	mgr := cameraManagerPtr.Load()
	if mgr == nil {
		uvcLog.Debug().Msg("UVC init skipped: camera manager not initialized")
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
		uvcLog.Debug().Msg("UVC reinit skipped: camera manager not initialized")
		return
	}
	mgr.ReinitUVC(config.UsbDevices.UVC)
}
