package usbgadget

import (
	"fmt"
	"os/exec"
	"time"
)

type gadgetConfigItem struct {
	order       uint
	device      string
	path        []string
	attrs       gadgetAttributes
	configAttrs gadgetAttributes
	configPath  []string
	reportDesc  []byte
}

type gadgetAttributes map[string]string

type gadgetConfigItemWithKey struct {
	key  string
	item gadgetConfigItem
}

type orderedGadgetConfigItems []gadgetConfigItemWithKey

var defaultGadgetConfig = map[string]gadgetConfigItem{
	"base": {
		order: 0,
		attrs: gadgetAttributes{
			"bcdUSB":    "0x0200", // USB 2.0
			"idVendor":  "0x1d6b", // The Linux Foundation
			"idProduct": "0x0104", // Multifunction Composite Gadget
			"bcdDevice": "0x0100", // USB2
		},
		configAttrs: gadgetAttributes{
			"MaxPower": "250", // in unit of 2mA
		},
	},
	"base_info": {
		order:      1,
		path:       []string{"strings", "0x409"},
		configPath: []string{"strings", "0x409"},
		attrs: gadgetAttributes{
			"serialnumber": "",
			"manufacturer": "JetKVM",
			"product":      "JetKVM USB Emulation Device",
		},
		configAttrs: gadgetAttributes{
			"configuration": "Config 1: HID",
		},
	},
	// keyboard HID
	"keyboard": keyboardConfig,
	// mouse HID
	"absolute_mouse": absoluteMouseConfig,
	// relative mouse HID
	"relative_mouse": relativeMouseConfig,
	// mass storage
	"mass_storage_base": massStorageBaseConfig,
	"mass_storage_lun0": massStorageLun0Config,
	// audio (UAC1 - USB Audio Class 1)
	// NOTE: configPath is intentionally omitted - symlink is created manually in
	// configureUsbGadget() after UVC setup completes.
	"audio": {
		order:  4000,
		device: "uac1.usb0",
		path:   []string{"functions", "uac1.usb0"},
		// configPath intentionally omitted - symlink created manually after UVC
		attrs: gadgetAttributes{
			// UAC1 terminology (from gadget's perspective):
			// - Playback (p_*) = Gadget sends audio TO host = Host sees MICROPHONE
			// - Capture (c_*) = Gadget receives audio FROM host = Host sees SPEAKER
			//
			// NOTE: Capture (c_chmask) is disabled because audio output from the managed PC
			// is received via HDMI, not USB. This saves 2 endpoints (IN+OUT) which is critical
			// for fitting all gadget functions within the RV1106 DWC3 endpoint limit.
			//
			// CRITICAL: p_chmask MUST match the audio playback channel count in audio.c
			// (playback_channels = 1). Mismatch causes ALSA format errors and no audio.
			"p_chmask":         "1",     // USB Microphone: mono (Browser → WebRTC → JetKVM → USB → Managed PC)
			"p_srate":          "48000", // 48kHz sample rate
			"p_ssize":          "2",     // 16-bit samples (2 bytes)
			"p_volume_present": "1",     // Enable volume control
			"c_chmask":         "0",     // USB Speaker: DISABLED - audio comes via HDMI instead
			"c_srate":          "48000", // 48kHz sample rate (unused when c_chmask=0)
			"c_ssize":          "2",     // 16-bit samples (unused when c_chmask=0)
			"c_volume_present": "0",     // Volume control disabled (unused when c_chmask=0)
		},
	},
	// UVC (USB Video Class) - webcam passthrough
	// Order 3500 = before audio (4000). UVC uses isochronous mode (streaming_bulk=0)
	// for GStreamer compatibility. UAC1 symlink is created before UVC per Rockchip docs.
	// UVC requires complex directory setup - handled by SetupUVCFunction()
	"uvc": uvcConfig,
}

func (u *UsbGadget) isGadgetConfigItemEnabled(itemKey string) bool {
	switch itemKey {
	case "absolute_mouse":
		return u.enabledDevices.AbsoluteMouse
	case "relative_mouse":
		return u.enabledDevices.RelativeMouse
	case "keyboard":
		return u.enabledDevices.Keyboard
	case "mass_storage_base":
		return u.enabledDevices.MassStorage
	case "mass_storage_lun0":
		return u.enabledDevices.MassStorage
	case "audio":
		return u.enabledDevices.Audio
	case "uvc":
		return u.enabledDevices.UVC
	default:
		return true
	}
}

func (u *UsbGadget) loadGadgetConfig() {
	if u.customConfig.isEmpty {
		u.log.Trace().Msg("using default gadget config")
		return
	}

	u.configMap["base"].attrs["idVendor"] = u.customConfig.VendorId
	u.configMap["base"].attrs["idProduct"] = u.customConfig.ProductId

	u.configMap["base_info"].attrs["serialnumber"] = u.customConfig.SerialNumber
	u.configMap["base_info"].attrs["manufacturer"] = u.customConfig.Manufacturer
	u.configMap["base_info"].attrs["product"] = u.customConfig.Product
}

func (u *UsbGadget) SetGadgetConfig(config *Config) {
	u.configLock.Lock()
	defer u.configLock.Unlock()

	if config == nil {
		return // nothing to do
	}

	u.customConfig = *config
	u.loadGadgetConfig()
}

func (u *UsbGadget) SetGadgetDevices(devices *Devices) {
	u.configLock.Lock()
	defer u.configLock.Unlock()

	if devices == nil {
		return // nothing to do
	}

	u.enabledDevices = *devices
}

func (u *UsbGadget) GetGadgetDevices() Devices {
	u.configLock.Lock()
	defer u.configLock.Unlock()

	return u.enabledDevices
}

// GetConfigPath returns the path to the config item.
func (u *UsbGadget) GetConfigPath(itemKey string) (string, error) {
	item, ok := u.configMap[itemKey]
	if !ok {
		return "", fmt.Errorf("config item %s not found", itemKey)
	}
	return joinPath(u.kvmGadgetPath, item.configPath), nil
}

// GetPath returns the path to the item.
func (u *UsbGadget) GetPath(itemKey string) (string, error) {
	item, ok := u.configMap[itemKey]
	if !ok {
		return "", fmt.Errorf("config item %s not found", itemKey)
	}
	return joinPath(u.kvmGadgetPath, item.path), nil
}

// OverrideGadgetConfig overrides the gadget config for the given item and attribute.
// It returns an error if the item is not found or the attribute is not found.
// It returns true if the attribute is overridden, false otherwise.
func (u *UsbGadget) OverrideGadgetConfig(itemKey string, itemAttr string, value string) (error, bool) {
	u.configLock.Lock()
	defer u.configLock.Unlock()

	// get it as a pointer
	_, ok := u.configMap[itemKey]
	if !ok {
		return fmt.Errorf("config item %s not found", itemKey), false
	}

	if u.configMap[itemKey].attrs[itemAttr] == value {
		return nil, false
	}

	u.configMap[itemKey].attrs[itemAttr] = value
	u.log.Info().Str("itemKey", itemKey).Str("itemAttr", itemAttr).Str("value", value).Msg("overriding gadget config")

	return nil, true
}

func mountConfigFS(path string) error {
	err := exec.Command("mount", "-t", "configfs", "none", path).Run()
	if err != nil {
		return fmt.Errorf("failed to mount configfs: %w", err)
	}
	return nil
}

func (u *UsbGadget) Init() error {
	u.configLock.Lock()
	defer u.configLock.Unlock()

	// Debug: Log enabledDevices at init time
	u.log.Info().
		Bool("keyboard", u.enabledDevices.Keyboard).
		Bool("abs_mouse", u.enabledDevices.AbsoluteMouse).
		Bool("rel_mouse", u.enabledDevices.RelativeMouse).
		Bool("mass_storage", u.enabledDevices.MassStorage).
		Bool("audio", u.enabledDevices.Audio).
		Bool("uvc", u.enabledDevices.UVC).
		Msg("UsbGadget.Init: enabledDevices at startup")

	u.loadGadgetConfig()

	udcs := getUdcs()
	if len(udcs) < 1 {
		return u.logWarn("no udc found, skipping USB stack init", nil)
	}

	u.udc = udcs[0]

	err := u.configureUsbGadget(false)
	if err != nil {
		return u.logError("unable to initialize USB stack", err)
	}

	// Pre-open HID files to reduce input latency
	u.PreOpenHidFiles()

	return nil
}

func (u *UsbGadget) UpdateGadgetConfig() error {
	u.configLock.Lock()
	defer u.configLock.Unlock()

	u.loadGadgetConfig()

	// Close HID files before reconfiguration to prevent "file already closed" errors
	u.CloseHidFiles()

	err := u.configureUsbGadget(true)
	if err != nil {
		return u.logError("unable to update gadget config", err)
	}

	// Reopen HID files after reconfiguration
	u.PreOpenHidFiles()

	return nil
}

func (u *UsbGadget) configureUsbGadget(resetUsb bool) error {
	// If resetting USB (reconfiguration), perform full cleanup first.
	// This is critical for UVC which creates video devices that need time to cleanup.
	// Without proper cleanup, the gadget can fail with -19 (ENODEV) on rebind.
	if resetUsb {
		u.log.Info().Msg("unbinding USB gadget for reconfiguration")
		if err := u.UnbindUDC(); err != nil {
			u.log.Debug().Err(err).Msg("unbind failed (may not have been bound)")
		}

		// Remove config symlinks to allow clean reconfiguration
		// This prevents stale state from causing -19 errors on rebind
		u.log.Debug().Msg("removing config symlinks for clean reconfiguration")
		u.removeConfigSymlinks()

		// Wait for UVC video device cleanup before reconfiguring
		// 500ms is needed for kernel kobject cleanup to complete
		time.Sleep(500 * time.Millisecond)
	}

	// First pass: create directories and function config (without UDC binding)
	// Note: UVC symlink is NOT created here - it must be created after UVC setup
	err := u.WithTransaction(func() error {
		u.tx.MountConfigFS()
		u.tx.CreateConfigPath()
		u.tx.WriteGadgetConfigFunctions()
		return nil
	})
	if err != nil {
		return err
	}

	// Setup Audio and UVC symlinks
	// CRITICAL: Per Rockchip documentation, Audio (UAC) symlink MUST be created BEFORE UVC
	// for composite devices to enumerate correctly. The symlink order determines descriptor
	// order in the USB configuration, and incorrect order causes Windows driver failures.
	// Reference: Rockchip_Trouble_Shooting_Linux4.19_USB_Gadget_UVC_CN.md
	u.log.Info().
		Bool("uvc", u.enabledDevices.UVC).
		Bool("audio", u.enabledDevices.Audio).
		Bool("keyboard", u.enabledDevices.Keyboard).
		Bool("mass_storage", u.enabledDevices.MassStorage).
		Msg("configureUsbGadget: enabledDevices state")

	// Create Audio symlink FIRST (before UVC)
	// Audio symlink is created manually (configPath omitted in gadgetConfigItem)
	if u.enabledDevices.Audio {
		audioConfigPath := joinPath(u.configC1Path, []string{"uac1.usb0"})
		audioFuncPath := joinPath(u.kvmGadgetPath, []string{"functions", "uac1.usb0"})
		if err := createConfigFSSymlink(audioFuncPath, audioConfigPath); err != nil {
			u.log.Warn().Err(err).Msg("failed to create audio config symlink")
		}
	}

	// Setup UVC function and create symlink AFTER Audio
	// UVC uses H.264 framebased format for direct passthrough streaming
	// This enables zero-copy H.264 streaming from both HDMI loopback and camera passthrough
	//
	// NOTE: Both MJPEG and H.264 are advertised because:
	// 1. Linux's uvcvideo driver won't enumerate devices with only H.264 framebased format
	// 2. MJPEG is required for the device to be visible on Linux hosts
	// 3. When host selects MJPEG, we transcode H.264→MJPEG in software
	// 4. When host selects H.264 (macOS/Windows), we pass through directly
	// H.264 is listed first to be preferred when the host supports it
	uvcSetupOK := false
	if u.enabledDevices.UVC {
		// Get ALL supported formats - no USB rebind needed when camera settings change
		formats := GetAllUVCFormats()
		u.log.Info().
			Int("formats", len(formats)).
			Msg("UVC: Configuring with all supported formats")

		if err := u.SetupUVCFunction(formats); err != nil {
			u.log.Warn().Err(err).Msg("failed to setup UVC function")
			// Continue without UVC - don't fail the entire gadget
		} else {
			// Create UVC symlink
			uvcConfigPath := joinPath(u.configC1Path, []string{"uvc.usb0"})
			uvcFuncPath := joinPath(u.kvmGadgetPath, []string{"functions", "uvc.usb0"})
			if err := createConfigFSSymlink(uvcFuncPath, uvcConfigPath); err != nil {
				u.log.Warn().Err(err).Msg("failed to create UVC config symlink")
			} else {
				uvcSetupOK = true
			}
		}
	}

	_ = uvcSetupOK // Currently unused, but kept for potential future use

	// Bind to UDC (single bind operation)
	// For initial setup (resetUsb=false), this is the first bind.
	// For reconfiguration (resetUsb=true), we already unbound above, so this is a clean bind.
	err = u.WithTransaction(func() error {
		u.tx.WriteUDC()
		return nil
	})
	if err != nil {
		return err
	}

	return nil
}
