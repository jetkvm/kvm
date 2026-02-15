// RDP RPC Handlers
//
// This file implements JSON-RPC handlers for RDP server configuration:
//   - getRDPState: Query current RDP server status
//   - setRDPEnabled: Enable/disable RDP server
//   - setRDPPort: Configure listening port
//   - setRDPTLS: Enable/disable TLS encryption
//   - setRDPMaxConnections: Set maximum concurrent connections
//   - setRDPVideoEnabled: Enable/disable H.264 video via RDPGFX
//   - setRDPAudioEnabled: Enable/disable audio output to client
//   - setRDPMicEnabled: Enable/disable microphone input from client
//   - setRDPCameraEnabled: Enable/disable webcam redirection from client
//   - setRDPClipboardEnabled: Enable/disable clipboard-as-keystrokes
//   - setRDPPasteDelayMs: Set clipboard paste keystroke delay
//   - setRDPTargetOS: Set target OS for clipboard encoding
//   - setRDPClipboardMode: Set clipboard mode (text, base64-markers, base64-script)
//
// Configuration is persisted to disk and changes take effect immediately.

package kvm

import (
	"fmt"

	"github.com/jetkvm/kvm/internal/rdp"
)

const (
	// RDP max connections range
	minRDPConnections = 1

	// RDP paste delay range in milliseconds (same as VNC)
	minRDPPasteDelayMs = 0
	maxRDPPasteDelayMs = 50
)

// RDPState represents the current RDP server state for the UI.
type RDPState struct {
	Enabled          bool   `json:"enabled"`
	Running          bool   `json:"running"`
	Port             int    `json:"port"`
	ConnectionCount  int    `json:"connectionCount"`
	TLSEnabled       bool   `json:"tlsEnabled"`
	MaxConnections   int    `json:"maxConnections"`
	VideoEnabled     bool   `json:"videoEnabled"`
	AudioEnabled     bool   `json:"audioEnabled"`
	MicEnabled       bool   `json:"micEnabled"`
	CameraEnabled           bool `json:"cameraEnabled"`
	CameraTranscodeEnabled  bool `json:"cameraTranscodeEnabled"`
	UDPEnabled       bool   `json:"udpEnabled"`
	ClipboardEnabled bool   `json:"clipboardEnabled"`
	PasteDelayMs     int    `json:"pasteDelayMs"`
	TargetOS         string `json:"targetOS"`
	ClipboardMode    string `json:"clipboardMode"`
	Username         string `json:"username"`
	Domain           string `json:"domain"`
	// File transfer settings
	FileTransferEnabled    bool   `json:"fileTransferEnabled"`
	FileTransferMethod     string `json:"fileTransferMethod"`
	FileTransferMaxMB      int    `json:"fileTransferMaxMB"`
	FileTransferTTLSec     int    `json:"fileTransferTTLSec"`
	FileTransferCleanupSec int    `json:"fileTransferCleanupSec"`
	NetworkCmdWindows      string `json:"networkCmdWindows"`
	NetworkCmdLinux      string `json:"networkCmdLinux"`
	NetworkCmdMacOS      string `json:"networkCmdMacOS"`
	Base64CmdWindows     string `json:"base64CmdWindows"`
	Base64CmdLinux       string `json:"base64CmdLinux"`
	Base64CmdMacOS       string `json:"base64CmdMacOS"`
	// RD Gateway settings
	GatewayEnabled bool `json:"gatewayEnabled"`
	GatewayUDPPort int  `json:"gatewayUDPPort"`
}

func restartRDPServerIfRunning() error {
	server := GetRDPServer()
	if server == nil {
		return nil
	}
	if server.IsRunning() {
		if err := server.Stop(); err != nil {
			return fmt.Errorf("failed to stop RDP server: %w", err)
		}
		if err := server.Start(); err != nil {
			rdpLogger.Error().Err(err).Msg("RDP server stopped but failed to restart - server is now DOWN")
			return fmt.Errorf("RDP server failed to restart (server is now stopped): %w", err)
		}
	}
	return nil
}

func rpcGetRDPState() (RDPState, error) {
	server := GetRDPServer()
	running := false
	connCount := 0
	if server != nil {
		running = server.IsRunning()
		connCount = server.GetConnectionCount()
	}
	cfg := loadCfg()
	return RDPState{
		Enabled:          cfg.RDPEnabled,
		Running:          running,
		Port:             cfg.RDPPort,
		ConnectionCount:  connCount,
		TLSEnabled:       cfg.RDPUseTLS,
		MaxConnections:   cfg.RDPMaxConnections,
		VideoEnabled:     cfg.RDPVideoEnabled,
		AudioEnabled:     cfg.RDPAudioEnabled,
		MicEnabled:       cfg.RDPMicEnabled,
		CameraEnabled:          cfg.RDPCameraEnabled,
		CameraTranscodeEnabled: cfg.RDPCameraTranscodeEnabled,
		UDPEnabled:       cfg.RDPUDPEnabled == nil || *cfg.RDPUDPEnabled,
		ClipboardEnabled: cfg.RDPClipboardEnabled,
		PasteDelayMs:     cfg.RDPPasteDelayMs,
		TargetOS:         cfg.RDPTargetOS,
		ClipboardMode:    cfg.RDPClipboardMode,
		Username:         cfg.RDPUsername,
		Domain:           cfg.RDPDomain,
		// File transfer settings
		FileTransferEnabled:    cfg.RDPFileTransferEnabled,
		FileTransferMethod:     cfg.RDPFileTransferMethod,
		FileTransferMaxMB:      cfg.RDPFileTransferMaxMB,
		FileTransferTTLSec:     cfg.RDPFileTransferTTLSec,
		FileTransferCleanupSec: cfg.RDPFileTransferCleanupSec,
		NetworkCmdWindows:      cfg.RDPNetworkCmdWindows,
		NetworkCmdLinux:     cfg.RDPNetworkCmdLinux,
		NetworkCmdMacOS:     cfg.RDPNetworkCmdMacOS,
		Base64CmdWindows:    cfg.RDPBase64CmdWindows,
		Base64CmdLinux:      cfg.RDPBase64CmdLinux,
		Base64CmdMacOS:      cfg.RDPBase64CmdMacOS,
		// RD Gateway settings
		GatewayEnabled: cfg.RDPGatewayEnabled == nil || *cfg.RDPGatewayEnabled,
		GatewayUDPPort: func() int {
			if cfg.RDPGatewayUDPPort > 0 {
				return cfg.RDPGatewayUDPPort
			}
			return 3391
		}(),
	}, nil
}

func rpcSetRDPEnabled(enabled bool) error {
	if err := updateAndSaveConfig(func(cfg *Config) {
		cfg.RDPEnabled = enabled
	}); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	server := GetRDPServer()
	if server == nil {
		return nil
	}

	if enabled {
		if !server.IsRunning() {
			if err := server.Start(); err != nil {
				return fmt.Errorf("failed to start RDP server: %w", err)
			}
		}
	} else {
		if server.IsRunning() {
			if err := server.Stop(); err != nil {
				return fmt.Errorf("failed to stop RDP server: %w", err)
			}
		}
	}

	return nil
}

func rpcSetRDPPort(port int) error {
	if port < minPort || port > maxPort {
		return fmt.Errorf("invalid port number: %d (must be %d-%d)", port, minPort, maxPort)
	}

	if err := updateAndSaveConfig(func(cfg *Config) {
		cfg.RDPPort = port
	}); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	server := GetRDPServer()
	if server != nil {
		server.SetPort(port)
	}

	return restartRDPServerIfRunning()
}

func rpcSetRDPTLS(enabled bool) error {
	if err := updateAndSaveConfig(func(cfg *Config) {
		cfg.RDPUseTLS = enabled
	}); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	return restartRDPServerIfRunning()
}

// rpcSetRDPMaxConnections sets the maximum concurrent RDP connections.
// Valid range: 1-10 (hardware limit defined in rdp server)
func rpcSetRDPMaxConnections(max int) error {
	if max < minRDPConnections || max > rdp.MaxConnections {
		return fmt.Errorf("invalid max connections: %d (must be %d-%d)", max, minRDPConnections, rdp.MaxConnections)
	}

	if err := updateAndSaveConfig(func(cfg *Config) {
		cfg.RDPMaxConnections = max
	}); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	return nil
}

// rpcSetRDPVideoEnabled enables or disables H.264 video via RDPGFX.
func rpcSetRDPVideoEnabled(enabled bool) error {
	if err := updateAndSaveConfig(func(cfg *Config) {
		cfg.RDPVideoEnabled = enabled
	}); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	return nil
}

// rpcSetRDPAudioEnabled enables or disables audio output to the client.
func rpcSetRDPAudioEnabled(enabled bool) error {
	if err := updateAndSaveConfig(func(cfg *Config) {
		cfg.RDPAudioEnabled = enabled
	}); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	return nil
}

// rpcSetRDPMicEnabled enables or disables microphone input from the client.
func rpcSetRDPMicEnabled(enabled bool) error {
	if err := updateAndSaveConfig(func(cfg *Config) {
		cfg.RDPMicEnabled = enabled
	}); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	return nil
}

// rpcSetRDPCameraEnabled enables or disables webcam redirection from the client.
func rpcSetRDPCameraEnabled(enabled bool) error {
	if err := updateAndSaveConfig(func(cfg *Config) {
		cfg.RDPCameraEnabled = enabled
	}); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	return nil
}

// rpcSetRDPCameraTranscodeEnabled enables or disables H.264→MJPEG software transcoding.
// WARNING: This is a BETA feature with HIGH CPU usage (~80-100% on Cortex-A7).
// Only enable if your RDP client only sends H.264 and you need MJPEG output.
func rpcSetRDPCameraTranscodeEnabled(enabled bool) error {
	if err := updateAndSaveConfig(func(cfg *Config) {
		cfg.RDPCameraTranscodeEnabled = enabled
	}); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	// Reset the failed flag when config changes, allowing a retry
	if enabled {
		cameraTranscodeInitFailed.Store(false)
	} else {
		// When disabling, also shutdown any running transcoder
		shutdownCameraTranscoder()
	}

	return nil
}

// rpcSetRDPUDPEnabled enables or disables UDP transport for RDP.
func rpcSetRDPUDPEnabled(enabled bool) error {
	if err := updateAndSaveConfig(func(cfg *Config) {
		cfg.RDPUDPEnabled = &enabled
	}); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	return nil
}

// rpcSetRDPClipboardEnabled enables or disables clipboard-as-keystrokes.
// When disabled, clipboard text from RDP clients is ignored.
func rpcSetRDPClipboardEnabled(enabled bool) error {
	if err := updateAndSaveConfig(func(cfg *Config) {
		cfg.RDPClipboardEnabled = enabled
	}); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	return nil
}

// rpcSetRDPPasteDelayMs sets the clipboard paste delay per keystroke in milliseconds.
// Valid range: 0-50 (0 = fastest, relies on USB polling; higher = slower but more compatible)
func rpcSetRDPPasteDelayMs(delayMs int) error {
	if delayMs < minRDPPasteDelayMs || delayMs > maxRDPPasteDelayMs {
		return fmt.Errorf("invalid paste delay: %d (must be %d-%d ms)", delayMs, minRDPPasteDelayMs, maxRDPPasteDelayMs)
	}

	if err := updateAndSaveConfig(func(cfg *Config) {
		cfg.RDPPasteDelayMs = delayMs
	}); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	return nil
}

// rpcSetRDPTargetOS sets the target OS for clipboard encoding.
// Valid values: "windows", "macos", "linux"
func rpcSetRDPTargetOS(targetOS string) error {
	switch targetOS {
	case "windows", "macos", "linux":
		// Valid
	default:
		return fmt.Errorf("invalid target OS: %s (must be windows, macos, or linux)", targetOS)
	}

	if err := updateAndSaveConfig(func(cfg *Config) {
		cfg.RDPTargetOS = targetOS
	}); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	return nil
}

// rpcSetRDPClipboardMode sets the clipboard mode for handling non-ASCII content.
// Valid values: "text" (skip non-typeable), "base64-markers" (wrap in markers), "base64-script" (OS script)
func rpcSetRDPClipboardMode(mode string) error {
	switch mode {
	case "text", "base64-markers", "base64-script":
		// Valid
	default:
		return fmt.Errorf("invalid clipboard mode: %s (must be text, base64-markers, or base64-script)", mode)
	}

	if err := updateAndSaveConfig(func(cfg *Config) {
		cfg.RDPClipboardMode = mode
	}); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	return nil
}

// rpcSetRDPUsername sets the expected username for RDP authentication.
// If empty, any username is accepted (only password is validated).
func rpcSetRDPUsername(username string) error {
	// Validate username length (allow empty for "any username" mode)
	if len(username) > 256 {
		return fmt.Errorf("username too long: max 256 characters")
	}

	if err := updateAndSaveConfig(func(cfg *Config) {
		cfg.RDPUsername = username
	}); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	return nil
}

// rpcSetRDPDomain sets the expected domain for RDP authentication.
// If empty, any domain is accepted (only username and password are validated).
func rpcSetRDPDomain(domain string) error {
	// Validate domain length (allow empty for "any domain" mode)
	if len(domain) > 256 {
		return fmt.Errorf("domain too long: max 256 characters")
	}

	if err := updateAndSaveConfig(func(cfg *Config) {
		cfg.RDPDomain = domain
	}); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	return nil
}

// File Transfer RPC Handlers

// rpcSetRDPFileTransferEnabled enables or disables file transfer via clipboard.
func rpcSetRDPFileTransferEnabled(enabled bool) error {
	if err := updateAndSaveConfig(func(cfg *Config) {
		cfg.RDPFileTransferEnabled = enabled
	}); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	return nil
}

// rpcSetRDPFileTransferMethod sets the file transfer method.
// Valid values: "auto", "network", "base64", "usb"
func rpcSetRDPFileTransferMethod(method string) error {
	switch method {
	case "auto", "network", "base64", "usb":
		// Valid
	default:
		return fmt.Errorf("invalid file transfer method: %s (must be auto, network, base64, or usb)", method)
	}

	if err := updateAndSaveConfig(func(cfg *Config) {
		cfg.RDPFileTransferMethod = method
	}); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	return nil
}

// rpcSetRDPFileTransferMaxMB sets the maximum file size for transfer in megabytes.
func rpcSetRDPFileTransferMaxMB(maxMB int) error {
	if maxMB < 1 || maxMB > 1000 {
		return fmt.Errorf("invalid max file size: %d MB (must be 1-1000)", maxMB)
	}

	if err := updateAndSaveConfig(func(cfg *Config) {
		cfg.RDPFileTransferMaxMB = maxMB
	}); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	return nil
}

// rpcSetRDPFileTransferTTLSec sets the file expiry time in seconds.
func rpcSetRDPFileTransferTTLSec(ttlSec int) error {
	if ttlSec < 30 || ttlSec > 3600 {
		return fmt.Errorf("invalid TTL: %d seconds (must be 30-3600)", ttlSec)
	}

	if err := updateAndSaveConfig(func(cfg *Config) {
		cfg.RDPFileTransferTTLSec = ttlSec
	}); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	// Update the clipboard store with new TTL
	GetClipboardStore().Configure(ttlSec, 0)

	return nil
}

// rpcSetRDPFileTransferCleanupSec sets the cleanup interval in seconds.
func rpcSetRDPFileTransferCleanupSec(cleanupSec int) error {
	if cleanupSec < 10 || cleanupSec > 600 {
		return fmt.Errorf("invalid cleanup interval: %d seconds (must be 10-600)", cleanupSec)
	}

	if err := updateAndSaveConfig(func(cfg *Config) {
		cfg.RDPFileTransferCleanupSec = cleanupSec
	}); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	// Update the clipboard store with new cleanup interval
	GetClipboardStore().Configure(0, cleanupSec)

	return nil
}

// rpcSetRDPNetworkCmdWindows sets the custom network download command for Windows.
func rpcSetRDPNetworkCmdWindows(cmd string) error {
	if len(cmd) > 1024 {
		return fmt.Errorf("command too long: max 1024 characters")
	}

	if err := updateAndSaveConfig(func(cfg *Config) {
		cfg.RDPNetworkCmdWindows = cmd
	}); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	return nil
}

// rpcSetRDPNetworkCmdLinux sets the custom network download command for Linux.
func rpcSetRDPNetworkCmdLinux(cmd string) error {
	if len(cmd) > 1024 {
		return fmt.Errorf("command too long: max 1024 characters")
	}

	if err := updateAndSaveConfig(func(cfg *Config) {
		cfg.RDPNetworkCmdLinux = cmd
	}); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	return nil
}

// rpcSetRDPNetworkCmdMacOS sets the custom network download command for macOS.
func rpcSetRDPNetworkCmdMacOS(cmd string) error {
	if len(cmd) > 1024 {
		return fmt.Errorf("command too long: max 1024 characters")
	}

	if err := updateAndSaveConfig(func(cfg *Config) {
		cfg.RDPNetworkCmdMacOS = cmd
	}); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	return nil
}

// rpcSetRDPBase64CmdWindows sets the custom base64 decode command for Windows.
func rpcSetRDPBase64CmdWindows(cmd string) error {
	if len(cmd) > 1024 {
		return fmt.Errorf("command too long: max 1024 characters")
	}

	if err := updateAndSaveConfig(func(cfg *Config) {
		cfg.RDPBase64CmdWindows = cmd
	}); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	return nil
}

// rpcSetRDPBase64CmdLinux sets the custom base64 decode command for Linux.
func rpcSetRDPBase64CmdLinux(cmd string) error {
	if len(cmd) > 1024 {
		return fmt.Errorf("command too long: max 1024 characters")
	}

	if err := updateAndSaveConfig(func(cfg *Config) {
		cfg.RDPBase64CmdLinux = cmd
	}); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	return nil
}

// rpcSetRDPBase64CmdMacOS sets the custom base64 decode command for macOS.
func rpcSetRDPBase64CmdMacOS(cmd string) error {
	if len(cmd) > 1024 {
		return fmt.Errorf("command too long: max 1024 characters")
	}

	if err := updateAndSaveConfig(func(cfg *Config) {
		cfg.RDPBase64CmdMacOS = cmd
	}); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	return nil
}

// RD Gateway RPC Handlers

// rpcSetRDPGatewayEnabled enables or disables the RD Gateway on HTTPS.
func rpcSetRDPGatewayEnabled(enabled bool) error {
	if err := updateAndSaveConfig(func(cfg *Config) {
		cfg.RDPGatewayEnabled = &enabled
	}); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	return nil
}

// rpcSetRDPGatewayUDPPort sets the UDP port for ShortPath discovery.
func rpcSetRDPGatewayUDPPort(port int) error {
	if port < minPort || port > maxPort {
		return fmt.Errorf("invalid port number: %d (must be %d-%d)", port, minPort, maxPort)
	}

	if err := updateAndSaveConfig(func(cfg *Config) {
		cfg.RDPGatewayUDPPort = port
	}); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	return nil
}
