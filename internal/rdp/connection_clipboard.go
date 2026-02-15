package rdp

// Clipboard channel wiring: CLIPRDR (MS-RDPECLIP).
// Handles text clipboard and file transfer via network/base64 methods.

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/jetkvm/kvm/internal/rdp/channels"
)

// TargetOS represents the target operating system for command generation.
type TargetOS string

const (
	TargetOSWindows TargetOS = "windows"
	TargetOSLinux   TargetOS = "linux"
	TargetOSMacOS   TargetOS = "macos"
)

// MaxClipboardReassemblySize is the maximum size for clipboard PDU reassembly.
// This prevents memory exhaustion from malicious clients sending huge totalLength values.
// 100MB should be sufficient for even very large file transfers.
const MaxClipboardReassemblySize = 100 * 1024 * 1024

// handleClipboardWithReassembly handles clipboard PDUs with fragmentation support.
// Per MS-RDPBCGR 2.2.6.1, large virtual channel PDUs are split across multiple packets.
// Only the first fragment contains the CLIPRDR PDU header; subsequent fragments are raw data.
func (c *Connection) handleClipboardWithReassembly(payload []byte, totalLength uint32, flags uint32) {
	isFirst := (flags & channelFlagFirst) != 0
	isLast := (flags & channelFlagLast) != 0

	c.server.deps.Logger.Debug().
		Uint32("totalLength", totalLength).
		Uint32("flags", flags).
		Bool("first", isFirst).
		Bool("last", isLast).
		Int("payloadLen", len(payload)).
		Msg("RDP: clipboard VC PDU fragment")

	// Fast path: single-fragment PDU (both FIRST and LAST set, or small PDU)
	if isFirst && isLast {
		c.handleClipboard(payload)
		return
	}

	// Handle fragmented PDU reassembly
	var completePDU []byte

	c.clipboardReassembly.mu.Lock()
	if isFirst {
		// Validate size to prevent memory exhaustion from malicious clients
		if totalLength > MaxClipboardReassemblySize {
			// Clear any existing buffer state before rejecting
			c.clipboardReassembly.buffer = nil
			c.clipboardReassembly.totalLength = 0
			c.clipboardReassembly.mu.Unlock()
			c.server.deps.Logger.Warn().
				Uint32("totalLength", totalLength).
				Uint32("maxAllowed", MaxClipboardReassemblySize).
				Msg("RDP: clipboard reassembly size exceeds maximum, dropping")
			return
		}

		// Start new reassembly buffer
		// Pre-allocate to expected total size to avoid reallocations
		c.clipboardReassembly.buffer = make([]byte, 0, totalLength)
		c.clipboardReassembly.totalLength = totalLength
		c.clipboardReassembly.buffer = append(c.clipboardReassembly.buffer, payload...)

		c.server.deps.Logger.Debug().
			Uint32("totalLength", totalLength).
			Int("firstChunkLen", len(payload)).
			Msg("RDP: clipboard reassembly started")
		c.clipboardReassembly.mu.Unlock()
		return
	}

	// Middle or last fragment - append to buffer
	if c.clipboardReassembly.buffer == nil {
		c.clipboardReassembly.mu.Unlock()
		c.server.deps.Logger.Warn().
			Bool("isLast", isLast).
			Int("payloadLen", len(payload)).
			Msg("RDP: clipboard fragment received without FIRST flag, discarding")
		return
	}

	c.clipboardReassembly.buffer = append(c.clipboardReassembly.buffer, payload...)

	c.server.deps.Logger.Debug().
		Int("bufferLen", len(c.clipboardReassembly.buffer)).
		Uint32("expectedLen", c.clipboardReassembly.totalLength).
		Bool("isLast", isLast).
		Msg("RDP: clipboard fragment appended")

	if isLast {
		// Reassembly complete - take ownership of the buffer
		completePDU = c.clipboardReassembly.buffer
		c.clipboardReassembly.buffer = nil
		c.clipboardReassembly.totalLength = 0
	}
	c.clipboardReassembly.mu.Unlock()

	// Process complete PDU outside lock to avoid holding it during HandlePDU
	if completePDU != nil {
		c.handleClipboard(completePDU)
	}
}

// handleClipboard handles clipboard channel asynchronously to prevent blocking video/input.
func (c *Connection) handleClipboard(data []byte) {
	if c.clipboardChannel == nil {
		return
	}

	// Check if this is a format list PDU (CB_FORMAT_LIST).
	// If so, clear clipboard state SYNCHRONOUSLY to prevent race conditions
	// with key event handling that checks clipboardText and pendingFiles.
	if len(data) >= 2 {
		msgType := binary.LittleEndian.Uint16(data[0:2])
		if msgType == channels.CBFormatList {
			c.clipboardChannel.ClearClipboardText()
			c.clearPendingFiles()
			c.targetCopied.Store(false) // Client has new content, supersedes target copy
		}
	}

	// Make a copy since the buffer may be reused
	dataCopy := make([]byte, len(data))
	copy(dataCopy, data)

	// Process asynchronously to avoid blocking the main message loop
	go func() {
		defer func() {
			if r := recover(); r != nil {
				c.server.deps.Logger.Error().
					Interface("panic", r).
					Msg("RDP: clipboard handler panicked")
			}
		}()

		if err := c.clipboardChannel.HandlePDU(dataCopy); err != nil {
			c.server.deps.Logger.Debug().Err(err).Msg("RDP: cliprdr error")
		}
	}()
}

func (c *Connection) initClipboardChannel() {
	c.clipboardChannel = channels.NewClipboardChannel(func(data []byte) error {
		return c.sendClipboardData(data)
	})

	// Configure file transfer if enabled
	if c.server.deps.Config.GetRDPFileTransferEnabled() {
		c.clipboardChannel.EnableFileTransfer(true)
		c.clipboardChannel.SetFileTransferCallback(c.onFileTransferComplete)
	}

	// Set up logging at Debug level to avoid log noise
	c.clipboardChannel.SetLogger(func(format string, args ...any) {
		c.server.deps.Logger.Debug().Msgf(format, args...)
	})

	if err := c.clipboardChannel.Start(); err != nil {
		c.server.deps.Logger.Warn().Err(err).Msg("failed to start cliprdr")
	}
}

// onFileTransferComplete is called when clipboard file transfer completes.
// Files are stored locally, waiting for the user to paste (Ctrl+V).
func (c *Connection) onFileTransferComplete(files []*channels.ClipboardFile) {
	if len(files) == 0 {
		return
	}

	c.server.deps.Logger.Debug().Int("count", len(files)).Msg("CLIPRDR: file transfer complete")

	// Clean up any previous pending files before storing new ones
	c.clearPendingFiles()

	// Store new files for later paste - don't type anything yet
	c.pendingFilesMu.Lock()
	c.pendingFiles = files
	c.pendingFilesMu.Unlock()
}

// handleFilePaste is called when user presses Ctrl+V with pending files.
// Returns true if files were pasted (caller should suppress the V key).
func (c *Connection) handleFilePaste() bool {
	c.pendingFilesMu.Lock()
	files := c.pendingFiles
	c.pendingFiles = nil
	c.pendingFilesMu.Unlock()

	if len(files) == 0 {
		return false
	}

	c.server.deps.Logger.Debug().Int("count", len(files)).Msg("CLIPRDR: pasting files")

	method := c.server.deps.Config.GetRDPFileTransferMethod()
	if method == "" || method == "auto" {
		method = "network"
	}

	switch method {
	case "network":
		c.transferFilesViaNetwork(files)
	case "base64":
		c.transferFilesViaBase64(files)
	case "usb":
		c.transferFilesViaUSB(files)
	default:
		c.server.deps.Logger.Warn().Str("method", method).Msg("CLIPRDR: unknown transfer method")
	}

	return true
}

// HasPendingFiles returns true if there are files waiting to be pasted.
func (c *Connection) HasPendingFiles() bool {
	c.pendingFilesMu.Lock()
	defer c.pendingFilesMu.Unlock()
	return len(c.pendingFiles) > 0
}

// clearPendingFiles removes any pending clipboard files and cleans up their temp files.
func (c *Connection) clearPendingFiles() {
	c.pendingFilesMu.Lock()
	files := c.pendingFiles
	c.pendingFiles = nil
	c.pendingFilesMu.Unlock()

	for _, f := range files {
		if f.TempPath != "" {
			if err := os.Remove(f.TempPath); err != nil && !os.IsNotExist(err) {
				c.server.deps.Logger.Debug().Err(err).Str("path", f.TempPath).Msg("CLIPRDR: failed to remove temp file")
			}
		}
	}
}

// transferFilesViaNetwork serves files via the main HTTPS server (port 443) and types download commands.
func (c *Connection) transferFilesViaNetwork(files []*channels.ClipboardFile) {
	// Check if clipboard store is available
	if c.server.deps.ClipboardStore == nil {
		c.server.deps.Logger.Error().Msg("CLIPRDR: clipboard store not available")
		return
	}

	// Get JetKVM's IP address from the connection
	serverIP := c.getLocalIP()
	if serverIP == "" {
		c.server.deps.Logger.Error().Msg("CLIPRDR: could not determine server IP")
		return
	}

	// Use port 443 (main HTTPS server) and always HTTPS when TLS is enabled
	port := 443
	scheme := "http"
	if c.server.deps.TLSEnabled {
		scheme = "https"
	}

	targetOS := c.getTargetOS()
	customTemplate := c.getNetworkCmdTemplate(targetOS)

	// Process each file
	for _, f := range files {
		if !f.Complete {
			c.server.deps.Logger.Warn().Str("name", f.Descriptor.FileName).Msg("CLIPRDR: skipping incomplete file")
			continue
		}

		// Add file to the shared clipboard store
		token, err := c.server.deps.ClipboardStore.AddFile(f.TempPath, f.Descriptor.FileName)
		if err != nil {
			c.server.deps.Logger.Error().Err(err).Str("name", f.Descriptor.FileName).Msg("CLIPRDR: failed to add file to store")
			continue
		}

		// Generate download command (includes TLS flags for self-signed certs)
		cmd := generateDownloadCommand(targetOS, scheme, serverIP, port, token, f.Descriptor.FileName, customTemplate)

		c.server.deps.Logger.Debug().
			Str("name", f.Descriptor.FileName).
			Uint64("size", f.Descriptor.FileSize()).
			Msg("CLIPRDR: typing download command")

		// Type the command
		if err := c.server.deps.HID.KeyboardMacro(cmd); err != nil {
			c.server.deps.Logger.Error().Err(err).Msg("CLIPRDR: failed to type download command")
		}
	}
}

// generateDownloadCommand creates a download command for the target OS.
func generateDownloadCommand(targetOS TargetOS, scheme, serverIP string, port int, token, fileName, customTemplate string) string {
	// Build URL - omit port for standard ports
	var url string
	if (scheme == "https" && port == 443) || (scheme == "http" && port == 80) {
		url = scheme + "://" + serverIP + "/c/" + token
	} else {
		url = scheme + "://" + serverIP + ":" + fmt.Sprintf("%d", port) + "/c/" + token
	}

	escapedName := escapeFileName(fileName, targetOS)

	// Use custom template if provided
	if customTemplate != "" {
		cmd := strings.ReplaceAll(customTemplate, "{url}", url)
		cmd = strings.ReplaceAll(cmd, "{filename}", escapedName)
		return cmd
	}

	// Default templates
	insecureFlag := ""
	if scheme == "https" {
		insecureFlag = "-k " // Skip certificate validation for self-signed
	}

	switch targetOS {
	case TargetOSWindows:
		if scheme == "https" {
			return fmt.Sprintf("[Net.ServicePointManager]::SecurityProtocol=[Net.SecurityProtocolType]::Tls12;iwr %s -OutFile %s -SkipCertificateCheck", url, escapedName)
		}
		return fmt.Sprintf("iwr %s -OutFile %s", url, escapedName)

	case TargetOSLinux, TargetOSMacOS:
		return fmt.Sprintf("curl %s-o %s %s", insecureFlag, escapedName, url)

	default:
		return fmt.Sprintf("curl %s-o %s %s", insecureFlag, escapedName, url)
	}
}

// escapeFileName escapes a filename for use in shell commands.
func escapeFileName(name string, targetOS TargetOS) string {
	name = filepath.Base(name)

	if targetOS == TargetOSWindows {
		if strings.ContainsAny(name, " '\"") {
			return "'" + strings.ReplaceAll(name, "'", "''") + "'"
		}
		return name
	}

	if strings.ContainsAny(name, " '\"$`\\!") {
		return "'" + strings.ReplaceAll(name, "'", "'\\''") + "'"
	}
	return name
}

// transferFilesViaBase64 encodes small files as base64 and types decode scripts.
func (c *Connection) transferFilesViaBase64(files []*channels.ClipboardFile) {
	// Base64 mode has stricter size limits due to typing speed
	maxSize := int64(100 * 1024) // 100KB default for base64 (about 17 min at 100 chars/sec)
	cfgMax := int64(c.server.deps.Config.GetRDPFileTransferMaxMB()) * 1024 * 1024
	if cfgMax > 0 && cfgMax < maxSize {
		maxSize = cfgMax
	}

	targetOS := c.getTargetOS()

	for _, f := range files {
		if !f.Complete {
			continue
		}

		if int64(f.Descriptor.FileSize()) > maxSize {
			c.server.deps.Logger.Warn().
				Str("name", f.Descriptor.FileName).
				Uint64("size", f.Descriptor.FileSize()).
				Int64("maxSize", maxSize).
				Msg("CLIPRDR: file too large for base64 transfer")
			continue
		}

		// Read file
		data, err := os.ReadFile(f.TempPath)
		if err != nil {
			c.server.deps.Logger.Error().Err(err).Str("name", f.Descriptor.FileName).Msg("CLIPRDR: failed to read file")
			continue
		}

		// Compress with gzip
		var compressed bytes.Buffer
		gz := gzip.NewWriter(&compressed)
		if _, err := gz.Write(data); err != nil {
			gz.Close()
			c.server.deps.Logger.Error().Err(err).Str("name", f.Descriptor.FileName).Msg("CLIPRDR: failed to compress file")
			continue
		}
		gz.Close()

		// Base64 encode
		encoded := base64.StdEncoding.EncodeToString(compressed.Bytes())

		// Generate decode script
		outputName := sanitizeOutputFileName(f.Descriptor.FileName)
		script := c.generateDecodeScript(targetOS, encoded, outputName)

		c.server.deps.Logger.Debug().
			Str("name", f.Descriptor.FileName).
			Uint64("size", f.Descriptor.FileSize()).
			Msg("CLIPRDR: typing base64 decode script")

		// Type the script
		if err := c.server.deps.HID.KeyboardMacro(script); err != nil {
			c.server.deps.Logger.Error().Err(err).Msg("CLIPRDR: failed to type decode script")
		}
	}
}

// transferFilesViaUSB copies files to USB mass storage and opens the drive on target.
func (c *Connection) transferFilesViaUSB(files []*channels.ClipboardFile) {
	// Check if USB storage provider is available
	if c.server.deps.USBStorage == nil {
		c.server.deps.Logger.Warn().Msg("CLIPRDR: USB storage provider not available")
		// Fall back to network method
		c.transferFilesViaNetwork(files)
		return
	}

	// Check if USB mass storage is available (not already in use)
	if !c.server.deps.USBStorage.IsAvailable() {
		c.server.deps.Logger.Warn().Msg("CLIPRDR: USB mass storage already in use, falling back to network")
		c.transferFilesViaNetwork(files)
		return
	}

	imagesFolder := c.server.deps.USBStorage.GetImagesFolder()
	targetOS := c.getTargetOS()

	// For USB transfer, we need to create a disk image containing the files.
	// For simplicity, we copy files to the images folder and mount them individually.
	// Note: This is a simplified implementation - ideally we'd create a FAT32 image.
	for _, f := range files {
		if !f.Complete {
			c.server.deps.Logger.Warn().Str("name", f.Descriptor.FileName).Msg("CLIPRDR: skipping incomplete file")
			continue
		}

		// Sanitize filename
		safeName := sanitizeOutputFileName(f.Descriptor.FileName)

		// Copy file to images folder
		destPath := filepath.Join(imagesFolder, safeName)
		if err := copyFile(f.TempPath, destPath); err != nil {
			c.server.deps.Logger.Error().Err(err).Str("name", f.Descriptor.FileName).Msg("CLIPRDR: failed to copy file to USB storage")
			continue
		}

		// Mount the file as USB mass storage
		if err := c.server.deps.USBStorage.MountFile(safeName); err != nil {
			c.server.deps.Logger.Error().Err(err).Str("name", safeName).Msg("CLIPRDR: failed to mount file as USB storage")
			// Clean up the copied file
			if rmErr := os.Remove(destPath); rmErr != nil {
				c.server.deps.Logger.Debug().Err(rmErr).Str("path", destPath).Msg("CLIPRDR: failed to remove USB file copy")
			}
			continue
		}

		c.server.deps.Logger.Debug().
			Str("name", f.Descriptor.FileName).
			Uint64("size", f.Descriptor.FileSize()).
			Msg("CLIPRDR: file mounted as USB storage")

		// Type command to open the USB drive
		cmd := c.generateOpenUSBCommand(targetOS)
		if cmd != "" {
			c.server.deps.Logger.Debug().Msg("CLIPRDR: typing command to open USB drive")
			if err := c.server.deps.HID.KeyboardMacro(cmd); err != nil {
				c.server.deps.Logger.Error().Err(err).Msg("CLIPRDR: failed to type USB open command")
			}
		}

		// Only mount one file at a time for USB
		break
	}
}

// generateOpenUSBCommand generates a command to open the USB drive on the target OS.
func (c *Connection) generateOpenUSBCommand(targetOS TargetOS) string {
	switch targetOS {
	case TargetOSWindows:
		// Open File Explorer - the USB drive is typically the last drive letter
		// This opens "This PC" where the user can see the USB drive
		return "explorer.exe shell:MyComputerFolder"
	case TargetOSLinux:
		// On Linux, USB drives are typically auto-mounted to /media or /run/media
		// xdg-open will open the file manager
		return "xdg-open /media 2>/dev/null || xdg-open /run/media 2>/dev/null || true"
	case TargetOSMacOS:
		// On macOS, USB drives are mounted to /Volumes
		return "open /Volumes"
	default:
		return ""
	}
}

// copyFile copies a file from src to dst with fsync for durability.
func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	if _, err = io.Copy(dstFile, srcFile); err != nil {
		return err
	}

	// Ensure data is flushed to disk before returning
	return dstFile.Sync()
}

// generateDecodeScript generates an OS-specific script to decode base64+gzip content.
func (c *Connection) generateDecodeScript(targetOS TargetOS, encoded, outputName string) string {
	// Check for custom template
	customTemplate := c.getBase64CmdTemplate(targetOS)
	if customTemplate != "" {
		cmd := strings.ReplaceAll(customTemplate, "{data}", encoded)
		cmd = strings.ReplaceAll(cmd, "{filename}", outputName)
		return cmd
	}

	// Default templates
	switch targetOS {
	case TargetOSWindows:
		// PowerShell one-liner: decode base64, decompress gzip, write to file
		return "powershell -c \"$m=[IO.MemoryStream]::new();$g=[IO.Compression.GZipStream]::new([IO.MemoryStream][Convert]::FromBase64String('" + encoded + "'),'Decompress');$g.CopyTo($m);[IO.File]::WriteAllBytes('" + outputName + "',$m.ToArray())\""

	case TargetOSLinux:
		// bash: echo base64 | base64 -d | gunzip > file
		return "echo '" + encoded + "' | base64 -d | gunzip > " + outputName

	case TargetOSMacOS:
		// macOS uses -D flag for decode (vs -d on Linux)
		return "echo '" + encoded + "' | base64 -D | gunzip > " + outputName

	default:
		// Default to Linux syntax
		return "echo '" + encoded + "' | base64 -d | gunzip > " + outputName
	}
}

// getNetworkCmdTemplate returns the custom network download command template for the OS.
func (c *Connection) getNetworkCmdTemplate(targetOS TargetOS) string {
	switch targetOS {
	case TargetOSWindows:
		return c.server.deps.Config.GetRDPNetworkCmdWindows()
	case TargetOSLinux:
		return c.server.deps.Config.GetRDPNetworkCmdLinux()
	case TargetOSMacOS:
		return c.server.deps.Config.GetRDPNetworkCmdMacOS()
	default:
		return ""
	}
}

// getBase64CmdTemplate returns the custom base64 decode command template for the OS.
func (c *Connection) getBase64CmdTemplate(targetOS TargetOS) string {
	switch targetOS {
	case TargetOSWindows:
		return c.server.deps.Config.GetRDPBase64CmdWindows()
	case TargetOSLinux:
		return c.server.deps.Config.GetRDPBase64CmdLinux()
	case TargetOSMacOS:
		return c.server.deps.Config.GetRDPBase64CmdMacOS()
	default:
		return ""
	}
}

// sanitizeOutputFileName creates a safe filename for the target OS.
func sanitizeOutputFileName(name string) string {
	// Get base name only
	name = filepath.Base(name)

	// Remove problematic characters
	name = strings.ReplaceAll(name, "'", "")
	name = strings.ReplaceAll(name, "\"", "")
	name = strings.ReplaceAll(name, "`", "")
	name = strings.ReplaceAll(name, "$", "")
	name = strings.ReplaceAll(name, "\\", "")
	name = strings.ReplaceAll(name, " ", "_")

	// Limit length
	if len(name) > 50 {
		ext := filepath.Ext(name)
		base := name[:50-len(ext)]
		name = base + ext
	}

	if name == "" {
		name = "file.bin"
	}

	return name
}

// getLocalIP returns the JetKVM's IP address for the current connection.
func (c *Connection) getLocalIP() string {
	if c.conn == nil {
		return ""
	}

	addr := c.conn.LocalAddr()
	if addr == nil {
		return ""
	}

	// Handle both TCP and potential other connection types
	switch a := addr.(type) {
	case *net.TCPAddr:
		return a.IP.String()
	default:
		// Try to parse from string (format: "ip:port")
		host, _, err := net.SplitHostPort(addr.String())
		if err != nil {
			return ""
		}
		return host
	}
}

// getTargetOS returns the configured target OS for command generation.
func (c *Connection) getTargetOS() TargetOS {
	os := c.server.deps.Config.GetRDPTargetOS()
	switch strings.ToLower(os) {
	case "windows":
		return TargetOSWindows
	case "linux":
		return TargetOSLinux
	case "macos":
		return TargetOSMacOS
	default:
		return TargetOSWindows // Default to Windows
	}
}

func (c *Connection) sendClipboardData(data []byte) error {
	if c.cliprdrdID == 0 {
		return nil
	}
	return c.sendStaticChannelDataHotPath(c.cliprdrdID, data)
}
