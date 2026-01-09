package kvm

import (
	"context"

	"github.com/jetkvm/kvm/internal/meshvpn"
	"github.com/jetkvm/kvm/internal/meshvpn/tailscale"
	"github.com/jetkvm/kvm/internal/meshvpn/zerotier"
)

var (
	meshVPNManager  *meshvpn.Manager
	meshVPNRegistry *meshvpn.Registry
)

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
		HTTPClient: meshvpn.NewDefaultHTTPClient(),
	})
	meshVPNRegistry.Register(tailscaleProvider)

	zerotierProvider := zerotier.NewProvider(zerotier.ProviderConfig{
		Version:    zerotier.DefaultVersion,
		HTTPClient: meshvpn.NewDefaultHTTPClient(),
	})
	meshVPNRegistry.Register(zerotierProvider)

	meshVPNManager = meshvpn.NewManager(meshvpn.ManagerConfig{
		Config:   vpnConfig,
		Registry: meshVPNRegistry,
		OnStatusChange: func(status meshvpn.ProviderStatus) {
			logger.Info().
				Str("state", string(status.State)).
				Bool("hasSession", currentSession != nil).
				Msg("meshVPN status change")
			if currentSession != nil {
				writeJSONRPCEvent("meshVPNState", status, currentSession)
			}
		},
		OnConfigChange: func(cfg *meshvpn.Config) {
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
