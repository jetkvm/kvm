package usbgadget

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

var uvcConfig = gadgetConfigItem{
	order:  3500,
	device: "uvc.usb0",
	path:   []string{"functions", "uvc.usb0"},
	attrs: gadgetAttributes{
		"streaming_bulk":      "0",    // isochronous mode
		"streaming_maxpacket": "3072", // high-bandwidth (3x1024)
	},
}

type UVCFormat struct {
	Width         int
	Height        int
	FrameInterval int // 100ns units (333333=30fps, 166666=60fps)
}

var (
	UVCFormat480p30  = UVCFormat{640, 480, 333333}   // 30fps
	UVCFormat720p30  = UVCFormat{1280, 720, 333333}  // 30fps
	UVCFormat720p60  = UVCFormat{1280, 720, 166666}  // 60fps
	UVCFormat1080p30 = UVCFormat{1920, 1080, 333333} // 30fps (preferred)
	UVCFormat360p30  = UVCFormat{640, 360, 333333}   // 30fps
)

// SetupUVCFunction creates the directory structure required for UVC gadget.
func (u *UsbGadget) SetupUVCFunction(formats []UVCFormat) error {
	if !u.enabledDevices.UVC {
		return nil
	}

	funcPath := filepath.Join(u.kvmGadgetPath, "functions", "uvc.usb0")
	if _, err := os.Stat(funcPath); os.IsNotExist(err) {
		return fmt.Errorf("UVC function directory does not exist: %s", funcPath)
	}

	// Skip if already configured
	if _, err := os.Lstat(filepath.Join(funcPath, "streaming", "header", "h", "m")); err == nil {
		return nil
	}

	if len(formats) == 0 {
		formats = []UVCFormat{UVCFormat1080p30}
	}

	streamingPath := filepath.Join(funcPath, "streaming")
	mjpegPath := filepath.Join(streamingPath, "mjpeg", "m")
	if err := os.MkdirAll(mjpegPath, 0755); err != nil {
		return fmt.Errorf("failed to create mjpeg directory: %w", err)
	}

	for i, format := range formats {
		frameName := fmt.Sprintf("%dp", format.Height)
		if i > 0 {
			frameName = fmt.Sprintf("%dp_%d", format.Height, i)
		}

		framePath := filepath.Join(mjpegPath, frameName)
		if err := os.MkdirAll(framePath, 0755); err != nil {
			return fmt.Errorf("failed to create frame directory: %w", err)
		}

		frameBufferSize := format.Width * format.Height * 2
		fps := 10000000 / format.FrameInterval
		minBitRate := format.Width * format.Height * fps * 2
		maxBitRate := format.Width * format.Height * fps * 8

		params := map[string]int{
			"wWidth":                   format.Width,
			"wHeight":                  format.Height,
			"dwDefaultFrameInterval":   format.FrameInterval,
			"dwMaxVideoFrameBufferSize": frameBufferSize,
			"dwMinBitRate":             minBitRate,
			"dwMaxBitRate":             maxBitRate,
			"dwFrameInterval":          format.FrameInterval,
		}
		for name, value := range params {
			if err := writeFile(filepath.Join(framePath, name), fmt.Sprintf("%d", value)); err != nil {
				return fmt.Errorf("failed to write %s: %w", name, err)
			}
		}
	}

	// Create streaming header and link mjpeg format
	headerPath := filepath.Join(streamingPath, "header", "h")
	if err := os.MkdirAll(headerPath, 0755); err != nil {
		return fmt.Errorf("failed to create streaming header directory: %w", err)
	}
	if err := createConfigFSSymlink(filepath.Join(streamingPath, "mjpeg", "m"), filepath.Join(headerPath, "m")); err != nil {
		return fmt.Errorf("failed to create mjpeg symlink: %w", err)
	}

	// Link header to streaming class descriptors (FS/HS only, not SS)
	for _, speed := range []string{"fs", "hs"} {
		classPath := filepath.Join(streamingPath, "class", speed)
		if err := os.MkdirAll(classPath, 0755); err != nil {
			return fmt.Errorf("failed to create class/%s directory: %w", speed, err)
		}
		if err := createConfigFSSymlink(headerPath, filepath.Join(classPath, "h")); err != nil {
			return fmt.Errorf("failed to create header symlink in class/%s: %w", speed, err)
		}
	}

	// Setup control header
	controlPath := filepath.Join(funcPath, "control")
	controlHeaderPath := filepath.Join(controlPath, "header", "h")
	if err := os.MkdirAll(controlHeaderPath, 0755); err != nil {
		return fmt.Errorf("failed to create control header directory: %w", err)
	}
	controlClassPath := filepath.Join(controlPath, "class", "fs")
	if err := os.MkdirAll(controlClassPath, 0755); err != nil {
		return fmt.Errorf("failed to create control class/fs directory: %w", err)
	}
	if err := createConfigFSSymlink(controlHeaderPath, filepath.Join(controlClassPath, "h")); err != nil {
		return fmt.Errorf("failed to create control header symlink: %w", err)
	}

	u.log.Info().Int("formats", len(formats)).Msg("UVC function configured")
	return nil
}

// GetUVCVideoDevice returns the video device path for UVC gadget.
func (u *UsbGadget) GetUVCVideoDevice() (string, error) {
	const maxRetries = 10
	const retryDelay = 200 * time.Millisecond

	for retry := 0; retry < maxRetries; retry++ {
		if retry > 0 {
			time.Sleep(retryDelay)
		}

		videoDevices, err := filepath.Glob("/dev/video*")
		if err != nil {
			continue
		}

		for _, dev := range videoDevices {
			namePath := filepath.Join("/sys/class/video4linux", filepath.Base(dev), "name")
			nameBytes, err := os.ReadFile(namePath)
			if err != nil {
				continue
			}

			name := string(nameBytes)
			if strings.Contains(name, "gadget") || strings.Contains(name, "dwc3") || strings.Contains(name, "g_uvc") {
				return dev, nil
			}
		}
	}

	return "", fmt.Errorf("no UVC video device found")
}

func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0644)
}

func createConfigFSSymlink(target, linkPath string) error {
	_ = os.Remove(linkPath)
	cmd := exec.Command("ln", "-sf", target, linkPath)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ln -sf failed: %w: %s", err, string(output))
	}
	return nil
}
