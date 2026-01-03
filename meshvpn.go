package kvm

import (
	"context"

	"github.com/jetkvm/kvm/internal/meshvpn"
	"github.com/jetkvm/kvm/internal/meshvpn/tailscale"
)

var (
	meshVPNManager  *meshvpn.Manager
	meshVPNRegistry *meshvpn.Registry
)

// initMeshVPN initializes the mesh VPN subsystem
func initMeshVPN() {
	logger.Info().Msg("initializing mesh VPN")

	meshVPNRegistry = meshvpn.NewRegistry()

	var vpnConfig *meshvpn.Config
	if config != nil && config.MeshVPNConfig != nil {
		vpnConfig = config.MeshVPNConfig
	}

	tunMode := meshvpn.TUNModeUserspace
	if vpnConfig != nil && vpnConfig.Tailscale != nil && vpnConfig.Tailscale.TUNMode != "" {
		tunMode = vpnConfig.Tailscale.TUNMode
	}

	tailscaleProvider := tailscale.NewProvider(tailscale.ProviderConfig{
		Version:    tailscale.DefaultVersion,
		TUNMode:    tunMode,
		HTTPClient: tailscale.NewDefaultHTTPClient(),
	})
	meshVPNRegistry.Register(tailscaleProvider)

	meshVPNManager = meshvpn.NewManager(meshvpn.ManagerConfig{
		Config:   vpnConfig,
		Registry: meshVPNRegistry,
		OnStatusChange: func(status meshvpn.ProviderStatus) {
			// Emit status change to connected clients
			if currentSession != nil {
				writeJSONRPCEvent("meshVPNState", status, currentSession)
			}
		},
		OnConfigChange: func(cfg *meshvpn.Config) {
			// Update main config
			config.MeshVPNConfig = cfg
		},
		SaveConfig: func(cfg *meshvpn.Config) error {
			config.MeshVPNConfig = cfg
			return SaveConfig()
		},
	})

	logger.Info().
		Int("providers", meshVPNRegistry.Count()).
		Msg("mesh VPN initialized")

	// Auto-start the provider if it was previously enabled
	go func() {
		ctx := context.Background()
		if err := meshVPNManager.AutoStart(ctx); err != nil {
			logger.Warn().Err(err).Msg("mesh VPN auto-start failed")
		}
	}()
}

func getMeshVPNManager() *meshvpn.Manager {
	return meshVPNManager
}
