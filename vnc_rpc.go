// VNC RPC Handlers
//
// This file implements JSON-RPC handlers for VNC server configuration:
//   - getVNCState: Query current VNC server status
//   - setVNCEnabled: Enable/disable VNC server
//   - setVNCPort: Configure listening port
//   - setVNCQuality: Set JPEG compression quality
//   - setVNCPassword: Set VNC authentication password
//   - setVNCTLS: Enable/disable TLS encryption
//   - setVNCPasteDelayMs: Set clipboard paste keystroke delay
//   - setVNCMaxConnections: Set maximum concurrent connections
//   - setVNCClipboardEnabled: Enable/disable clipboard typing
//
// Configuration is persisted to disk and changes take effect immediately.

package kvm

import (
	"fmt"

	"github.com/jetkvm/kvm/internal/vnc"
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

	// VNC max connections range (used for validation only; vnc.MaxConnections in vnc_server.go is hardware limit)
	minVNCConnections = 1

	// VNC paste delay range in milliseconds (0 = fastest, relies on USB polling; 50 = very slow)
	minPasteDelayMs = 0
	maxPasteDelayMs = 50
)

type VNCState struct {
	Enabled          bool `json:"enabled"`
	Running          bool `json:"running"`
	Port             int  `json:"port"`
	Quality          int  `json:"quality"`
	ConnectionCount  int  `json:"connectionCount"`
	TLSEnabled       bool `json:"tlsEnabled"`
	PasteDelayMs     int  `json:"pasteDelayMs"`
	MaxConnections   int  `json:"maxConnections"`
	ClipboardEnabled bool `json:"clipboardEnabled"`
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
	cfg := loadCfg()
	server := GetVNCServer()
	return VNCState{
		Enabled:          cfg.VNCEnabled,
		Running:          server.IsRunning(),
		Port:             cfg.VNCPort,
		Quality:          cfg.VNCQuality,
		ConnectionCount:  server.GetConnectionCount(),
		TLSEnabled:       cfg.VNCUseTLS,
		PasteDelayMs:     cfg.VNCPasteDelayMs,
		MaxConnections:   cfg.VNCMaxConnections,
		ClipboardEnabled: cfg.VNCClipboardEnabled,
	}, nil
}

func rpcSetVNCEnabled(enabled bool) error {
	if err := updateAndSaveConfig(func(cfg *Config) {
		cfg.VNCEnabled = enabled
	}); err != nil {
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

	if err := updateAndSaveConfig(func(cfg *Config) {
		cfg.VNCPort = port
	}); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	GetVNCServer().SetPort(port)

	return restartVNCServerIfRunning()
}

func rpcSetVNCQuality(quality int) error {
	if quality < minJPEGQuality || quality > maxJPEGQuality {
		return fmt.Errorf("invalid quality value: %d (must be %d-%d)", quality, minJPEGQuality, maxJPEGQuality)
	}

	if err := updateAndSaveConfig(func(cfg *Config) {
		cfg.VNCQuality = quality
	}); err != nil {
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

	if err := updateAndSaveConfig(func(cfg *Config) {
		cfg.LocalAuthPassword = password
	}); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	return nil
}

func rpcSetVNCTLS(enabled bool) error {
	if err := updateAndSaveConfig(func(cfg *Config) {
		cfg.VNCUseTLS = enabled
	}); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	GetVNCServer().SetTLSEnabled(enabled)

	return restartVNCServerIfRunning()
}

// rpcSetVNCPasteDelayMs sets the clipboard paste delay per keystroke in milliseconds.
// Valid range: 0-50 (0 = fastest, relies on USB polling; higher = slower but more compatible)
func rpcSetVNCPasteDelayMs(delayMs int) error {
	if delayMs < minPasteDelayMs || delayMs > maxPasteDelayMs {
		return fmt.Errorf("invalid paste delay: %d (must be %d-%d ms)", delayMs, minPasteDelayMs, maxPasteDelayMs)
	}

	if err := updateAndSaveConfig(func(cfg *Config) {
		cfg.VNCPasteDelayMs = delayMs
	}); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	return nil
}

// rpcSetVNCMaxConnections sets the maximum concurrent VNC connections.
// Valid range: 1-10 (hardware limit defined in vnc_server.go)
func rpcSetVNCMaxConnections(max int) error {
	if max < minVNCConnections || max > vnc.MaxConnections {
		return fmt.Errorf("invalid max connections: %d (must be %d-%d)", max, minVNCConnections, vnc.MaxConnections)
	}

	if err := updateAndSaveConfig(func(cfg *Config) {
		cfg.VNCMaxConnections = max
	}); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	return nil
}

// rpcSetVNCClipboardEnabled enables or disables clipboard-as-keystrokes.
// When disabled, clipboard text from VNC clients is ignored.
func rpcSetVNCClipboardEnabled(enabled bool) error {
	if err := updateAndSaveConfig(func(cfg *Config) {
		cfg.VNCClipboardEnabled = enabled
	}); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	return nil
}
