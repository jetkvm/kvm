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

// stopUVC stops the UVC event loop and closes the UVC device handle.
// This MUST be called before USB gadget reconfiguration to prevent kernel hangs.
// The UVC video device is destroyed during gadget unbind, so holding a file handle
// can cause the kernel to block indefinitely.
func stopUVC() {
	mgr := cameraManagerPtr.Load()
	if mgr == nil {
		uvcLog.Debug().Msg("UVC stop skipped: camera manager not initialized")
		return
	}
	mgr.StopUVC()
	uvcLog.Debug().Msg("UVC stopped for USB reconfiguration")
}
