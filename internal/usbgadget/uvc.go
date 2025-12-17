package usbgadget

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// UVC gadget configuration
// UVC (USB Video Class) allows JetKVM to appear as a USB webcam to the managed PC
//
// Transfer mode note:
// UVC supports both isochronous and bulk transfer modes. We use BULK mode
// (streaming_bulk=1) to avoid isochronous endpoint conflicts with UAC1 audio.
// This requires kernel patches to f_uvc.c for proper bulk endpoint initialization.
//
// With bulk mode, UVC can coexist with UAC1 audio since they use different
// endpoint types (bulk vs isochronous), avoiding dwc3 endpoint competition.

var uvcConfig = gadgetConfigItem{
	order:  3500,
	device: "uvc.usb0",
	path:   []string{"functions", "uvc.usb0"},
	// NOTE: configPath is intentionally omitted - the symlink must be created AFTER
	// SetupUVCFunction() configures the streaming settings, otherwise the kernel
	// returns "device or resource busy" because the function is already active.
	// The symlink is created manually in configureUsbGadget() after UVC setup.
	//
	// Transfer mode: Use BULK mode to avoid isochronous endpoint conflict with UAC1.
	// Requires kernel patch to f_uvc.c for bulk endpoint wMaxPacketSize initialization.
	attrs: gadgetAttributes{
		"streaming_bulk":      "0",    // Use isochronous transfer mode
		"streaming_maxpacket": "3072", // Max packet size for HS isochronous (3x1024)
	},
}

// UVCFormat represents a video format configuration
type UVCFormat struct {
	Width         int
	Height        int
	FrameInterval int // in 100ns units (e.g., 333333 = 30fps, 166666 = 60fps)
}

// Common UVC formats
var (
	UVCFormat720p30  = UVCFormat{Width: 1280, Height: 720, FrameInterval: 333333}  // 30 fps
	UVCFormat720p60  = UVCFormat{Width: 1280, Height: 720, FrameInterval: 166666}  // 60 fps
	UVCFormat1080p30 = UVCFormat{Width: 1920, Height: 1080, FrameInterval: 333333} // 30 fps
	UVCFormat360p30  = UVCFormat{Width: 640, Height: 360, FrameInterval: 333333}   // 30 fps
)

// SetupUVCFunction creates the complex directory structure required for UVC gadget
// This must be called after the function directory exists but before binding to UDC
func (u *UsbGadget) SetupUVCFunction(formats []UVCFormat) error {
	if !u.enabledDevices.UVC {
		return nil
	}

	funcPath := filepath.Join(u.kvmGadgetPath, "functions", "uvc.usb0")

	// Check if function directory exists
	if _, err := os.Stat(funcPath); os.IsNotExist(err) {
		return fmt.Errorf("UVC function directory does not exist: %s", funcPath)
	}

	// Check if UVC is already configured by looking for the header symlink
	// If it exists, skip setup to avoid "device or resource busy" errors during reconfiguration
	headerSymlink := filepath.Join(funcPath, "streaming", "header", "h", "m")
	if _, err := os.Lstat(headerSymlink); err == nil {
		u.log.Debug().Msg("UVC function already configured, skipping setup")
		return nil
	}

	// Use default format if none specified
	if len(formats) == 0 {
		formats = []UVCFormat{UVCFormat720p30}
	}

	// Setup streaming configuration
	streamingPath := filepath.Join(funcPath, "streaming")

	// Create MJPEG format directory
	mjpegPath := filepath.Join(streamingPath, "mjpeg", "m")
	if err := os.MkdirAll(mjpegPath, 0755); err != nil {
		return fmt.Errorf("failed to create mjpeg directory: %w", err)
	}

	// Configure each format/resolution
	for i, format := range formats {
		frameName := fmt.Sprintf("%dp", format.Height)
		if i > 0 {
			frameName = fmt.Sprintf("%dp_%d", format.Height, i)
		}

		framePath := filepath.Join(mjpegPath, frameName)
		if err := os.MkdirAll(framePath, 0755); err != nil {
			return fmt.Errorf("failed to create frame directory: %w", err)
		}

		// Calculate frame buffer size and bitrates
		// For MJPEG, frame buffer size is typically width * height * 2 (YUV422)
		// but for compressed MJPEG we use a smaller estimate
		frameBufferSize := format.Width * format.Height * 2
		// Bitrate calculation: pixels * bytes_per_pixel * fps * 8 bits
		// For MJPEG at ~10:1 compression ratio
		fps := 10000000 / format.FrameInterval // Convert 100ns units to fps
		minBitRate := format.Width * format.Height * fps / 10 * 8
		maxBitRate := format.Width * format.Height * fps * 8

		// Write frame parameters
		if err := writeFile(filepath.Join(framePath, "wWidth"), fmt.Sprintf("%d", format.Width)); err != nil {
			return fmt.Errorf("failed to write wWidth: %w", err)
		}
		if err := writeFile(filepath.Join(framePath, "wHeight"), fmt.Sprintf("%d", format.Height)); err != nil {
			return fmt.Errorf("failed to write wHeight: %w", err)
		}
		if err := writeFile(filepath.Join(framePath, "dwDefaultFrameInterval"), fmt.Sprintf("%d", format.FrameInterval)); err != nil {
			return fmt.Errorf("failed to write dwDefaultFrameInterval: %w", err)
		}
		// Write the missing parameters that hosts need for proper enumeration
		if err := writeFile(filepath.Join(framePath, "dwMaxVideoFrameBufferSize"), fmt.Sprintf("%d", frameBufferSize)); err != nil {
			return fmt.Errorf("failed to write dwMaxVideoFrameBufferSize: %w", err)
		}
		if err := writeFile(filepath.Join(framePath, "dwMinBitRate"), fmt.Sprintf("%d", minBitRate)); err != nil {
			return fmt.Errorf("failed to write dwMinBitRate: %w", err)
		}
		if err := writeFile(filepath.Join(framePath, "dwMaxBitRate"), fmt.Sprintf("%d", maxBitRate)); err != nil {
			return fmt.Errorf("failed to write dwMaxBitRate: %w", err)
		}
		// Write frame interval (can be multiple values, one per line)
		// This tells the host what frame rates are supported
		if err := writeFile(filepath.Join(framePath, "dwFrameInterval"), fmt.Sprintf("%d", format.FrameInterval)); err != nil {
			return fmt.Errorf("failed to write dwFrameInterval: %w", err)
		}
	}

	// Create and configure streaming header
	headerPath := filepath.Join(streamingPath, "header", "h")
	if err := os.MkdirAll(headerPath, 0755); err != nil {
		return fmt.Errorf("failed to create streaming header directory: %w", err)
	}

	// Link mjpeg format to header
	// configfs requires absolute paths for symlink targets
	mjpegLink := filepath.Join(headerPath, "m")
	mjpegTarget := filepath.Join(streamingPath, "mjpeg", "m")
	if err := createConfigFSSymlink(mjpegTarget, mjpegLink); err != nil {
		return fmt.Errorf("failed to create mjpeg symlink in header: %w", err)
	}

	// Link header to streaming class descriptors (FS and HS only)
	// Note: SS (SuperSpeed) links are intentionally omitted as they can cause
	// enumeration failures on USB 2.0 connections. The test script only uses FS/HS.
	// configfs requires absolute paths for symlink targets
	for _, speed := range []string{"fs", "hs"} {
		classPath := filepath.Join(streamingPath, "class", speed)
		if err := os.MkdirAll(classPath, 0755); err != nil {
			return fmt.Errorf("failed to create streaming class/%s directory: %w", speed, err)
		}

		headerLink := filepath.Join(classPath, "h")
		headerTarget := filepath.Join(streamingPath, "header", "h")
		if err := createConfigFSSymlink(headerTarget, headerLink); err != nil {
			return fmt.Errorf("failed to create header symlink in class/%s: %w", speed, err)
		}
	}

	// Setup control configuration
	controlPath := filepath.Join(funcPath, "control")
	controlHeaderPath := filepath.Join(controlPath, "header", "h")
	if err := os.MkdirAll(controlHeaderPath, 0755); err != nil {
		return fmt.Errorf("failed to create control header directory: %w", err)
	}

	// Link control header to class descriptor
	// configfs requires absolute paths for symlink targets
	controlClassPath := filepath.Join(controlPath, "class", "fs")
	if err := os.MkdirAll(controlClassPath, 0755); err != nil {
		return fmt.Errorf("failed to create control class/fs directory: %w", err)
	}

	controlHeaderLink := filepath.Join(controlClassPath, "h")
	controlHeaderTarget := filepath.Join(controlPath, "header", "h")
	if err := createConfigFSSymlink(controlHeaderTarget, controlHeaderLink); err != nil {
		return fmt.Errorf("failed to create control header symlink: %w", err)
	}

	u.log.Info().
		Int("formats", len(formats)).
		Str("path", funcPath).
		Msg("UVC function configured")

	return nil
}

// GetUVCVideoDevice returns the video device path for UVC gadget
// This is the device that uvc-gadget userspace helper should use
func (u *UsbGadget) GetUVCVideoDevice() (string, error) {
	// UVC gadget creates a video device after the UDC is bound.
	// The device may take a moment to appear, so we retry a few times.
	const maxRetries = 10
	const retryDelay = 200 // milliseconds

	for retry := 0; retry < maxRetries; retry++ {
		if retry > 0 {
			// Wait before retrying
			sleepMs(retryDelay)
		}

		// First try to find by device name - check sysfs for gadget-related names
		videoDevices, err := filepath.Glob("/dev/video*")
		if err != nil {
			continue
		}

		for _, dev := range videoDevices {
			baseName := filepath.Base(dev)
			namePath := filepath.Join("/sys/class/video4linux", baseName, "name")

			nameBytes, err := os.ReadFile(namePath)
			if err != nil {
				continue
			}

			name := string(nameBytes)
			// UVC gadget device typically has "gadget" or "dwc3" in the name
			if contains(name, "gadget") || contains(name, "dwc3") || contains(name, "g_uvc") {
				return dev, nil
			}
		}
	}

	return "", fmt.Errorf("no UVC video device found after %d retries", maxRetries)
}

// sleepMs sleeps for the given number of milliseconds
func sleepMs(ms int) {
	time.Sleep(time.Duration(ms) * time.Millisecond)
}

// helper to check if string contains substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsAt(s, substr, 0))
}

func containsAt(s, substr string, start int) bool {
	for i := start; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// writeFile is a helper to write content to a file
func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0644)
}

// createConfigFSSymlink creates a symlink in configfs using ln command
// This is required because configfs symlinks are special and can't be created
// with the standard symlink() syscall - they must use the ln command.
func createConfigFSSymlink(target, linkPath string) error {
	// Remove existing symlink if present
	_ = os.Remove(linkPath)

	// Use ln -s to create the symlink - configfs requires this approach
	cmd := exec.Command("ln", "-sf", target, linkPath)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ln -sf %s %s failed: %w: %s", target, linkPath, err, string(output))
	}
	return nil
}
