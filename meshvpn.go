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
	if cfg := loadCfg(); cfg != nil && cfg.MeshVPNConfig != nil {
		vpnConfig = cfg.MeshVPNConfig
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
			if s := currentSession.Load(); s != nil {
				writeJSONRPCEvent("meshVPNState", status, s)
			}
		},
		OnConfigChange: func(vpnCfg *meshvpn.Config) {
			_ = updateAndSaveConfig(func(cfg *Config) {
				cfg.MeshVPNConfig = vpnCfg
			})
		},
		SaveConfig: func(vpnCfg *meshvpn.Config) error {
			return updateAndSaveConfig(func(cfg *Config) {
				cfg.MeshVPNConfig = vpnCfg
			})
		},
	})

	logger.Info().Msg("mesh VPN initialized")

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
