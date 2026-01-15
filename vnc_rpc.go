package kvm

import (
	"fmt"
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
	if port < 1 || port > 65535 {
		return fmt.Errorf("invalid port number: %d", port)
	}

	config.VNCPort = port
	GetVNCServer().SetPort(port)

	if err := SaveConfig(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	return restartVNCServerIfRunning()
}

func rpcSetVNCQuality(quality int) error {
	if quality < 1 || quality > 99 {
		return fmt.Errorf("invalid quality value: %d (must be 1-99)", quality)
	}

	config.VNCQuality = quality

	if err := SaveConfig(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	return nil
}

func rpcSetVNCPassword(password string) error {
	if len(password) > 8 {
		password = password[:8] // VNC auth only uses first 8 characters
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
