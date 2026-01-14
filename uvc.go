package kvm

func initUVC() {
	mgr := cameraManagerPtr.Load()
	if mgr == nil {
		return
	}
	if err := mgr.InitUVC(config.UsbDevices.UVC); err != nil {
		uvcLog.Warn().Err(err).Msg("UVC initialization failed")
	}
}

func reinitUVC() {
	mgr := cameraManagerPtr.Load()
	if mgr == nil {
		return
	}
	mgr.ReinitUVC(config.UsbDevices.UVC)
}

// stopUVC must be called before USB gadget reconfiguration to prevent kernel hangs.
func stopUVC() {
	mgr := cameraManagerPtr.Load()
	if mgr == nil {
		return
	}
	mgr.StopUVC()
}
