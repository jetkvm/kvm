// VNC RPC Handlers
//
// This file implements JSON-RPC handlers for VNC server configuration:
//   - getVNCState: Query current VNC server status
//   - setVNCEnabled: Enable/disable VNC server
//   - setVNCPort: Configure listening port
//   - setVNCQuality: Set JPEG compression quality
//   - setVNCPassword: Set VNC authentication password
//   - setVNCTLS: Enable/disable TLS encryption
//
// Configuration is persisted to disk and changes take effect immediately.

package kvm

import (
	"fmt"
)

const (
	// Port range for TCP ports (IANA assigned range)
	minPort = 1
	maxPort = 65535

	// JPEG quality range (1-99, 100 would be lossless which JPEG doesn't support well)
	minJPEGQuality = 1
	maxJPEGQuality = 99

	// VNC password max length (DES key limitation)
	vncPasswordMaxLength = 8

	// VNC max connections range (used for validation only; maxVNCConnections in vnc_server.go is hardware limit)
	minVNCConnections = 1
)

type VNCState struct {
	Enabled          bool   `json:"enabled"`
	Running          bool   `json:"running"`
	Port             int    `json:"port"`
	Quality          int    `json:"quality"`
	ConnectionCount  int    `json:"connectionCount"`
	TLSEnabled       bool   `json:"tlsEnabled"`
	PasteSpeed       string `json:"pasteSpeed"`
	MaxConnections   int    `json:"maxConnections"`
	ClipboardEnabled bool   `json:"clipboardEnabled"`
}

func restartVNCServerIfRunning() error {
	server := GetVNCServer()
	if server.IsRunning() {
		if err := server.Stop(); err != nil {
			return fmt.Errorf("failed to stop VNC server: %w", err)
		}
		if err := server.Start(); err != nil {
			// Server stopped successfully but failed to restart - critical state
			vncLogger.Error().Err(err).Msg("VNC server stopped but failed to restart - server is now DOWN")
			return fmt.Errorf("VNC server failed to restart (server is now stopped): %w", err)
		}
	}
	return nil
}

func rpcGetVNCState() (VNCState, error) {
	server := GetVNCServer()
	return VNCState{
		Enabled:          config.VNCEnabled,
		Running:          server.IsRunning(),
		Port:             config.VNCPort,
		Quality:          config.VNCQuality,
		ConnectionCount:  server.GetConnectionCount(),
		TLSEnabled:       config.VNCUseTLS,
		PasteSpeed:       config.VNCPasteSpeed,
		MaxConnections:   config.VNCMaxConnections,
		ClipboardEnabled: config.VNCClipboardEnabled,
	}, nil
}

func rpcSetVNCEnabled(enabled bool) error {
	oldValue := config.VNCEnabled
	config.VNCEnabled = enabled

	if err := SaveConfig(); err != nil {
		config.VNCEnabled = oldValue // Rollback on failure
		return fmt.Errorf("failed to save config: %w", err)
	}

	server := GetVNCServer()
	if enabled {
		if !server.IsRunning() {
			if err := server.Start(); err != nil {
				return fmt.Errorf("failed to start VNC server: %w", err)
			}
		}
	} else {
		if server.IsRunning() {
			if err := server.Stop(); err != nil {
				return fmt.Errorf("failed to stop VNC server: %w", err)
			}
		}
	}

	return nil
}

func rpcSetVNCPort(port int) error {
	if port < minPort || port > maxPort {
		return fmt.Errorf("invalid port number: %d (must be %d-%d)", port, minPort, maxPort)
	}

	oldPort := config.VNCPort
	config.VNCPort = port
	GetVNCServer().SetPort(port)

	if err := SaveConfig(); err != nil {
		config.VNCPort = oldPort // Rollback on failure
		GetVNCServer().SetPort(oldPort)
		return fmt.Errorf("failed to save config: %w", err)
	}

	return restartVNCServerIfRunning()
}

func rpcSetVNCQuality(quality int) error {
	if quality < minJPEGQuality || quality > maxJPEGQuality {
		return fmt.Errorf("invalid quality value: %d (must be %d-%d)", quality, minJPEGQuality, maxJPEGQuality)
	}

	oldQuality := config.VNCQuality
	config.VNCQuality = quality

	if err := SaveConfig(); err != nil {
		config.VNCQuality = oldQuality // Rollback on failure
		return fmt.Errorf("failed to save config: %w", err)
	}

	return nil
}

// rpcSetVNCPassword sets the VNC authentication password.
// VNC uses DES encryption which only supports 8-byte keys, so passwords are truncated.
func rpcSetVNCPassword(password string) error {
	if len(password) > vncPasswordMaxLength {
		vncLogger.Warn().Int("originalLen", len(password)).Int("maxLen", vncPasswordMaxLength).Msg("VNC password truncated (protocol limitation)")
		password = password[:vncPasswordMaxLength]
	}

	oldPassword := config.LocalAuthPassword
	config.LocalAuthPassword = password

	if err := SaveConfig(); err != nil {
		config.LocalAuthPassword = oldPassword // Rollback on failure
		return fmt.Errorf("failed to save config: %w", err)
	}

	return nil
}

func rpcSetVNCTLS(enabled bool) error {
	oldValue := config.VNCUseTLS
	config.VNCUseTLS = enabled
	GetVNCServer().SetTLSEnabled(enabled)

	if err := SaveConfig(); err != nil {
		config.VNCUseTLS = oldValue // Rollback on failure
		GetVNCServer().SetTLSEnabled(oldValue)
		return fmt.Errorf("failed to save config: %w", err)
	}

	return restartVNCServerIfRunning()
}

// rpcSetVNCPasteSpeed sets the clipboard paste typing speed.
// Valid values: "fast" (1ms delays), "normal" (5ms), "slow" (15ms)
func rpcSetVNCPasteSpeed(speed string) error {
	switch speed {
	case "fast", "normal", "slow":
		// valid
	default:
		return fmt.Errorf("invalid paste speed: %q (must be fast, normal, or slow)", speed)
	}

	oldSpeed := config.VNCPasteSpeed
	config.VNCPasteSpeed = speed

	if err := SaveConfig(); err != nil {
		config.VNCPasteSpeed = oldSpeed
		return fmt.Errorf("failed to save config: %w", err)
	}

	return nil
}

// rpcSetVNCMaxConnections sets the maximum concurrent VNC connections.
// Valid range: 1-10 (hardware limit defined in vnc_server.go)
func rpcSetVNCMaxConnections(max int) error {
	if max < minVNCConnections || max > maxVNCConnections {
		return fmt.Errorf("invalid max connections: %d (must be %d-%d)", max, minVNCConnections, maxVNCConnections)
	}

	oldMax := config.VNCMaxConnections
	config.VNCMaxConnections = max

	if err := SaveConfig(); err != nil {
		config.VNCMaxConnections = oldMax
		return fmt.Errorf("failed to save config: %w", err)
	}

	return nil
}

// rpcSetVNCClipboardEnabled enables or disables clipboard-as-keystrokes.
// When disabled, clipboard text from VNC clients is ignored.
func rpcSetVNCClipboardEnabled(enabled bool) error {
	oldValue := config.VNCClipboardEnabled
	config.VNCClipboardEnabled = enabled

	if err := SaveConfig(); err != nil {
		config.VNCClipboardEnabled = oldValue
		return fmt.Errorf("failed to save config: %w", err)
	}

	return nil
}
