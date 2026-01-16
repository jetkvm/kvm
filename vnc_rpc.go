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
)

type VNCState struct {
	Enabled         bool `json:"enabled"`
	Running         bool `json:"running"`
	Port            int  `json:"port"`
	Quality         int  `json:"quality"`
	ConnectionCount int  `json:"connectionCount"`
	TLSEnabled      bool `json:"tlsEnabled"`
}

func restartVNCServerIfRunning() error {
	server := GetVNCServer()
	if server.IsRunning() {
		if err := server.Stop(); err != nil {
			return fmt.Errorf("failed to stop VNC server: %w", err)
		}
		if err := server.Start(); err != nil {
			return fmt.Errorf("failed to start VNC server: %w", err)
		}
	}
	return nil
}

func rpcGetVNCState() (VNCState, error) {
	server := GetVNCServer()
	return VNCState{
		Enabled:         config.VNCEnabled,
		Running:         server.IsRunning(),
		Port:            config.VNCPort,
		Quality:         config.VNCQuality,
		ConnectionCount: server.GetConnectionCount(),
		TLSEnabled:      config.VNCUseTLS,
	}, nil
}

func rpcSetVNCEnabled(enabled bool) error {
	config.VNCEnabled = enabled

	if err := SaveConfig(); err != nil {
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

	config.VNCPort = port
	GetVNCServer().SetPort(port)

	if err := SaveConfig(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	return restartVNCServerIfRunning()
}

func rpcSetVNCQuality(quality int) error {
	if quality < minJPEGQuality || quality > maxJPEGQuality {
		return fmt.Errorf("invalid quality value: %d (must be %d-%d)", quality, minJPEGQuality, maxJPEGQuality)
	}

	config.VNCQuality = quality

	if err := SaveConfig(); err != nil {
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

	config.LocalAuthPassword = password

	if err := SaveConfig(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	return nil
}

func rpcSetVNCTLS(enabled bool) error {
	config.VNCUseTLS = enabled
	GetVNCServer().SetTLSEnabled(enabled)

	if err := SaveConfig(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	return restartVNCServerIfRunning()
}
