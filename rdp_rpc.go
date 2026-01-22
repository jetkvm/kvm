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
	CameraEnabled    bool   `json:"cameraEnabled"`
	ClipboardEnabled bool   `json:"clipboardEnabled"`
	PasteDelayMs     int    `json:"pasteDelayMs"`
	TargetOS         string `json:"targetOS"`
	ClipboardMode    string `json:"clipboardMode"`
	Username         string `json:"username"`
	Domain           string `json:"domain"`
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
	return RDPState{
		Enabled:          config.RDPEnabled,
		Running:          running,
		Port:             config.RDPPort,
		ConnectionCount:  connCount,
		TLSEnabled:       config.RDPUseTLS,
		MaxConnections:   config.RDPMaxConnections,
		VideoEnabled:     config.RDPVideoEnabled,
		AudioEnabled:     config.RDPAudioEnabled,
		MicEnabled:       config.RDPMicEnabled,
		CameraEnabled:    config.RDPCameraEnabled,
		ClipboardEnabled: config.RDPClipboardEnabled,
		PasteDelayMs:     config.RDPPasteDelayMs,
		TargetOS:         config.RDPTargetOS,
		ClipboardMode:    config.RDPClipboardMode,
		Username:         config.RDPUsername,
		Domain:           config.RDPDomain,
	}, nil
}

func rpcSetRDPEnabled(enabled bool) error {
	oldValue := config.RDPEnabled
	config.RDPEnabled = enabled

	if err := SaveConfig(); err != nil {
		config.RDPEnabled = oldValue // Rollback on failure
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

	oldPort := config.RDPPort
	config.RDPPort = port

	server := GetRDPServer()
	if server != nil {
		server.SetPort(port)
	}

	if err := SaveConfig(); err != nil {
		config.RDPPort = oldPort // Rollback on failure
		if server != nil {
			server.SetPort(oldPort)
		}
		return fmt.Errorf("failed to save config: %w", err)
	}

	return restartRDPServerIfRunning()
}

func rpcSetRDPTLS(enabled bool) error {
	oldValue := config.RDPUseTLS
	config.RDPUseTLS = enabled

	if err := SaveConfig(); err != nil {
		config.RDPUseTLS = oldValue // Rollback on failure
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

	oldMax := config.RDPMaxConnections
	config.RDPMaxConnections = max

	if err := SaveConfig(); err != nil {
		config.RDPMaxConnections = oldMax
		return fmt.Errorf("failed to save config: %w", err)
	}

	return nil
}

// rpcSetRDPVideoEnabled enables or disables H.264 video via RDPGFX.
func rpcSetRDPVideoEnabled(enabled bool) error {
	oldValue := config.RDPVideoEnabled
	config.RDPVideoEnabled = enabled

	if err := SaveConfig(); err != nil {
		config.RDPVideoEnabled = oldValue
		return fmt.Errorf("failed to save config: %w", err)
	}

	return nil
}

// rpcSetRDPAudioEnabled enables or disables audio output to the client.
func rpcSetRDPAudioEnabled(enabled bool) error {
	oldValue := config.RDPAudioEnabled
	config.RDPAudioEnabled = enabled

	if err := SaveConfig(); err != nil {
		config.RDPAudioEnabled = oldValue
		return fmt.Errorf("failed to save config: %w", err)
	}

	return nil
}

// rpcSetRDPMicEnabled enables or disables microphone input from the client.
func rpcSetRDPMicEnabled(enabled bool) error {
	oldValue := config.RDPMicEnabled
	config.RDPMicEnabled = enabled

	if err := SaveConfig(); err != nil {
		config.RDPMicEnabled = oldValue
		return fmt.Errorf("failed to save config: %w", err)
	}

	return nil
}

// rpcSetRDPCameraEnabled enables or disables webcam redirection from the client.
func rpcSetRDPCameraEnabled(enabled bool) error {
	oldValue := config.RDPCameraEnabled
	config.RDPCameraEnabled = enabled

	if err := SaveConfig(); err != nil {
		config.RDPCameraEnabled = oldValue
		return fmt.Errorf("failed to save config: %w", err)
	}

	return nil
}

// rpcSetRDPClipboardEnabled enables or disables clipboard-as-keystrokes.
// When disabled, clipboard text from RDP clients is ignored.
func rpcSetRDPClipboardEnabled(enabled bool) error {
	oldValue := config.RDPClipboardEnabled
	config.RDPClipboardEnabled = enabled

	if err := SaveConfig(); err != nil {
		config.RDPClipboardEnabled = oldValue
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

	oldDelay := config.RDPPasteDelayMs
	config.RDPPasteDelayMs = delayMs

	if err := SaveConfig(); err != nil {
		config.RDPPasteDelayMs = oldDelay
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

	oldValue := config.RDPTargetOS
	config.RDPTargetOS = targetOS

	if err := SaveConfig(); err != nil {
		config.RDPTargetOS = oldValue
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

	oldValue := config.RDPClipboardMode
	config.RDPClipboardMode = mode

	if err := SaveConfig(); err != nil {
		config.RDPClipboardMode = oldValue
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

	oldValue := config.RDPUsername
	config.RDPUsername = username

	if err := SaveConfig(); err != nil {
		config.RDPUsername = oldValue
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

	oldValue := config.RDPDomain
	config.RDPDomain = domain

	if err := SaveConfig(); err != nil {
		config.RDPDomain = oldValue
		return fmt.Errorf("failed to save config: %w", err)
	}

	return nil
}
