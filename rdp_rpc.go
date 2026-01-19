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
)

// RDPState represents the current RDP server state for the UI.
type RDPState struct {
	Enabled         bool `json:"enabled"`
	Running         bool `json:"running"`
	Port            int  `json:"port"`
	ConnectionCount int  `json:"connectionCount"`
	TLSEnabled      bool `json:"tlsEnabled"`
	MaxConnections  int  `json:"maxConnections"`
	VideoEnabled    bool `json:"videoEnabled"`
	AudioEnabled    bool `json:"audioEnabled"`
	MicEnabled      bool `json:"micEnabled"`
	CameraEnabled   bool `json:"cameraEnabled"`
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
		Enabled:         config.RDPEnabled,
		Running:         running,
		Port:            config.RDPPort,
		ConnectionCount: connCount,
		TLSEnabled:      config.RDPUseTLS,
		MaxConnections:  config.RDPMaxConnections,
		VideoEnabled:    config.RDPVideoEnabled,
		AudioEnabled:    config.RDPAudioEnabled,
		MicEnabled:      config.RDPMicEnabled,
		CameraEnabled:   config.RDPCameraEnabled,
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
