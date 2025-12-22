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
		// Use bulk mode to avoid isochronous endpoint conflict with UAC.
		// Both UAC and UVC need isochronous IN endpoints, but the DWC3 can only
		// allocate one isochronous endpoint per direction. Bulk mode allows
		// UVC and UAC to coexist.
		// Throughput: Bulk has no guaranteed bandwidth but works well in practice.
		"streaming_bulk":      "1",    // bulk mode (avoids isoc endpoint conflict with UAC)
		"streaming_maxpacket": "1024", // max packet size for bulk
	},
}

// UVCFormat specifies the video format for UVC streaming
type UVCFormat struct {
	Width         int
	Height        int
	FrameInterval int // 100ns units (333333=30fps, 166666=60fps)
}

// UVCFormatType specifies the encoding type
type UVCFormatType int

const (
	UVCFormatTypeMJPEG UVCFormatType = iota
	UVCFormatTypeH264
)

// UVCFormatConfig includes format type along with resolution
type UVCFormatConfig struct {
	Format UVCFormat
	Type   UVCFormatType
}

var (
	// MJPEG formats (universal compatibility fallback)
	UVCFormatMJPEG_1080p30 = UVCFormatConfig{UVCFormat{1920, 1080, 333333}, UVCFormatTypeMJPEG}
	UVCFormatMJPEG_720p30  = UVCFormatConfig{UVCFormat{1280, 720, 333333}, UVCFormatTypeMJPEG}
	UVCFormatMJPEG_480p30  = UVCFormatConfig{UVCFormat{640, 480, 333333}, UVCFormatTypeMJPEG}

	// H.264 formats (uses framebased format for direct passthrough)
	UVCFormatH264_1080p60 = UVCFormatConfig{UVCFormat{1920, 1080, 166666}, UVCFormatTypeH264} // 60fps
	UVCFormatH264_1080p30 = UVCFormatConfig{UVCFormat{1920, 1080, 333333}, UVCFormatTypeH264}
	UVCFormatH264_720p60  = UVCFormatConfig{UVCFormat{1280, 720, 166666}, UVCFormatTypeH264} // 60fps
	UVCFormatH264_720p30  = UVCFormatConfig{UVCFormat{1280, 720, 333333}, UVCFormatTypeH264}
	UVCFormatH264_480p30  = UVCFormatConfig{UVCFormat{640, 480, 333333}, UVCFormatTypeH264}
)

// SetupUVCFunction creates the directory structure required for UVC gadget.
// Supports both MJPEG (universal compatibility) and H.264 (direct passthrough) formats.
func (u *UsbGadget) SetupUVCFunction(formats []UVCFormatConfig) error {
	if !u.enabledDevices.UVC {
		return nil
	}

	funcPath := filepath.Join(u.kvmGadgetPath, "functions", "uvc.usb0")
	if _, err := os.Stat(funcPath); os.IsNotExist(err) {
		return fmt.Errorf("UVC function directory does not exist: %s", funcPath)
	}

	if len(formats) == 0 {
		// Default: MJPEG for compatibility, H.264 for performance
		formats = []UVCFormatConfig{UVCFormatMJPEG_1080p30, UVCFormatH264_1080p30}
	}

	streamingPath := filepath.Join(funcPath, "streaming")
	headerPath := filepath.Join(streamingPath, "header", "h")

	// Skip if already configured (check for header directory with any format link)
	if entries, err := os.ReadDir(headerPath); err == nil && len(entries) > 0 {
		return nil
	}

	// Create header directory
	if err := os.MkdirAll(headerPath, 0755); err != nil {
		return fmt.Errorf("failed to create streaming header directory: %w", err)
	}

	// Group formats by type
	mjpegFormats := make([]UVCFormat, 0)
	h264Formats := make([]UVCFormat, 0)
	for _, fc := range formats {
		switch fc.Type {
		case UVCFormatTypeMJPEG:
			mjpegFormats = append(mjpegFormats, fc.Format)
		case UVCFormatTypeH264:
			h264Formats = append(h264Formats, fc.Format)
		}
	}

	// Setup MJPEG format (universal compatibility)
	if len(mjpegFormats) > 0 {
		if err := u.setupMJPEGFormat(streamingPath, headerPath, mjpegFormats); err != nil {
			return fmt.Errorf("failed to setup MJPEG format: %w", err)
		}
		u.log.Info().Int("mjpeg_formats", len(mjpegFormats)).Msg("UVC MJPEG format configured")
	}

	// Setup H.264 format (framebased for direct passthrough)
	if len(h264Formats) > 0 {
		if err := u.setupH264Format(streamingPath, headerPath, h264Formats); err != nil {
			return fmt.Errorf("failed to setup H.264 format: %w", err)
		}
		u.log.Info().Int("h264_formats", len(h264Formats)).Msg("UVC H.264 format configured")
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

	return nil
}

// setupMJPEGFormat configures MJPEG format in the UVC streaming interface.
func (u *UsbGadget) setupMJPEGFormat(streamingPath, headerPath string, formats []UVCFormat) error {
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
			return fmt.Errorf("failed to create MJPEG frame directory: %w", err)
		}

		fps := 10000000 / format.FrameInterval
		pixels := format.Width * format.Height
		// MJPEG bitrates: ~10-30 Mbps for 1080p
		minBitRate := pixels * fps / 20
		maxBitRate := pixels * fps / 5

		params := map[string]int{
			"wWidth":                    format.Width,
			"wHeight":                   format.Height,
			"dwDefaultFrameInterval":    format.FrameInterval,
			"dwMinBitRate":              minBitRate,
			"dwMaxBitRate":              maxBitRate,
			"dwFrameInterval":           format.FrameInterval,
			"dwMaxVideoFrameBufferSize": pixels * 2, // Conservative estimate
		}
		for name, value := range params {
			if err := writeFile(filepath.Join(framePath, name), fmt.Sprintf("%d", value)); err != nil {
				return fmt.Errorf("failed to write MJPEG %s: %w", name, err)
			}
		}
	}

	// Link MJPEG format to header
	if err := createConfigFSSymlink(mjpegPath, filepath.Join(headerPath, "m")); err != nil {
		return fmt.Errorf("failed to create MJPEG symlink: %w", err)
	}

	return nil
}

// setupH264Format configures H.264 framebased format in the UVC streaming interface.
func (u *UsbGadget) setupH264Format(streamingPath, headerPath string, formats []UVCFormat) error {
	h264Path := filepath.Join(streamingPath, "framebased", "h264")
	if err := os.MkdirAll(h264Path, 0755); err != nil {
		return fmt.Errorf("failed to create framebased/h264 directory: %w", err)
	}

	for i, format := range formats {
		frameName := fmt.Sprintf("%dp", format.Height)
		if i > 0 {
			frameName = fmt.Sprintf("%dp_%d", format.Height, i)
		}

		framePath := filepath.Join(h264Path, frameName)
		if err := os.MkdirAll(framePath, 0755); err != nil {
			return fmt.Errorf("failed to create H.264 frame directory: %w", err)
		}

		fps := 10000000 / format.FrameInterval
		pixels := format.Width * format.Height
		// H.264 bitrates: ~2-8 Mbps for 1080p
		minBitRate := pixels * fps / 100
		maxBitRate := pixels * fps / 25

		params := map[string]int{
			"wWidth":                 format.Width,
			"wHeight":                format.Height,
			"dwDefaultFrameInterval": format.FrameInterval,
			"dwMinBitRate":           minBitRate,
			"dwMaxBitRate":           maxBitRate,
			"dwFrameInterval":        format.FrameInterval,
			"dwBytesPerLine":         0, // Not applicable for compressed
		}
		for name, value := range params {
			if err := writeFile(filepath.Join(framePath, name), fmt.Sprintf("%d", value)); err != nil {
				return fmt.Errorf("failed to write H.264 %s: %w", name, err)
			}
		}
	}

	// Link H.264 format to header
	if err := createConfigFSSymlink(h264Path, filepath.Join(headerPath, "h264")); err != nil {
		return fmt.Errorf("failed to create H.264 symlink: %w", err)
	}

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
