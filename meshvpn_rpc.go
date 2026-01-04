package kvm

import (
	"context"
	"errors"
	"fmt"

	"github.com/jetkvm/kvm/internal/meshvpn"
)

var errMeshVPNNotInitialized = errors.New("mesh VPN not initialized")

// RPC types use camelCase JSON tags vs snake_case in internal types.

type RpcMeshVPNConfig struct {
	ActiveProvider string                      `json:"activeProvider,omitempty"`
	Tailscale      *RpcTailscaleProviderConfig `json:"tailscale,omitempty"`
	ZeroTier       *RpcZeroTierProviderConfig  `json:"zerotier,omitempty"`
}

type RpcTailscaleProviderConfig struct {
	Enabled                bool   `json:"enabled"`
	ControlServer          string `json:"controlServer,omitempty"`
	AuthKey                string `json:"authKey,omitempty"`
	ExitNode               string `json:"exitNode,omitempty"`
	ExitNodeAllowLANAccess bool   `json:"exitNodeAllowLanAccess,omitempty"`
	AdvertiseExitNode      bool   `json:"advertiseExitNode,omitempty"`
	TUNMode                string `json:"tunMode,omitempty"`
}

type RpcZeroTierProviderConfig struct {
	Enabled   bool   `json:"enabled"`
	NetworkID string `json:"networkId,omitempty"`
}

type RpcMeshVPNConnectParams struct {
	Provider      string `json:"provider,omitempty"`
	ControlServer string `json:"controlServer,omitempty"`
	AuthKey       string `json:"authKey,omitempty"`
}

type RpcMeshVPNConnectResult struct {
	Success bool   `json:"success"`
	AuthURL string `json:"authUrl,omitempty"`
}

type RpcMeshVPNSetExitNodeParams struct {
	Hostname string `json:"hostname"`
	AllowLAN bool   `json:"allowLan"`
}

type RpcMeshVPNUpdateParams struct {
	Provider      string `json:"provider,omitempty"`
	TargetVersion string `json:"targetVersion,omitempty"`
}

type RpcMeshVPNSetTUNModeParams struct {
	Provider string `json:"provider,omitempty"`
	Mode     string `json:"mode"`
}

type RpcMeshVPNGetTUNModeResult struct {
	Mode string `json:"mode"`
}

type RpcMeshVPNSetAdvertiseExitNodeParams struct {
	Provider  string `json:"provider,omitempty"`
	Advertise bool   `json:"advertise"`
}

func rpcGetMeshVPNProviders() ([]meshvpn.ProviderInfo, error) {
	manager := getMeshVPNManager()
	if manager == nil {
		return nil, errMeshVPNNotInitialized
	}

	return manager.ListProviders(), nil
}

func rpcGetMeshVPNStatus(params struct {
	Provider string `json:"provider,omitempty"`
}) (*meshvpn.ProviderStatus, error) {
	manager := getMeshVPNManager()
	if manager == nil {
		return nil, errMeshVPNNotInitialized
	}

	ctx := context.Background()
	var status *meshvpn.ProviderStatus
	var err error

	if params.Provider != "" {
		status, err = manager.GetProviderStatus(ctx, params.Provider)
	} else {
		status, err = manager.GetStatus(ctx)
	}

	if err != nil {
		if errors.Is(err, meshvpn.ErrNoActiveProvider) {
			return &meshvpn.ProviderStatus{
				State:     meshvpn.StateNotInstalled,
				Installed: false,
				Running:   false,
			}, nil
		}
		return nil, err
	}

	return status, nil
}

func rpcGetMeshVPNConfig() (*RpcMeshVPNConfig, error) {
	manager := getMeshVPNManager()
	if manager == nil {
		return nil, errMeshVPNNotInitialized
	}

	cfg := manager.GetConfig()
	if cfg == nil {
		return &RpcMeshVPNConfig{}, nil
	}

	result := &RpcMeshVPNConfig{
		ActiveProvider: cfg.ActiveProvider,
	}

	if cfg.Tailscale != nil {
		result.Tailscale = &RpcTailscaleProviderConfig{
			Enabled:                cfg.Tailscale.Enabled,
			ControlServer:          cfg.Tailscale.ControlServer,
			AuthKey:                cfg.Tailscale.AuthKey,
			ExitNode:               cfg.Tailscale.ExitNode,
			ExitNodeAllowLANAccess: cfg.Tailscale.ExitNodeAllowLANAccess,
			AdvertiseExitNode:      cfg.Tailscale.AdvertiseExitNode,
			TUNMode:                string(cfg.Tailscale.TUNMode),
		}
	}

	if cfg.ZeroTier != nil {
		result.ZeroTier = &RpcZeroTierProviderConfig{
			Enabled:   cfg.ZeroTier.Enabled,
			NetworkID: cfg.ZeroTier.NetworkID,
		}
	}

	return result, nil
}

func rpcSetMeshVPNConfig(params struct {
	Config RpcMeshVPNConfig `json:"config"`
}) (*RpcMeshVPNConfig, error) {
	manager := getMeshVPNManager()
	if manager == nil {
		return nil, errMeshVPNNotInitialized
	}

	cfg := &meshvpn.Config{
		ActiveProvider: params.Config.ActiveProvider,
	}

	if params.Config.Tailscale != nil {
		tunMode, err := meshvpn.ParseTUNMode(params.Config.Tailscale.TUNMode)
		if err != nil {
			return nil, err
		}
		cfg.Tailscale = &meshvpn.TailscaleConfig{
			Enabled:                params.Config.Tailscale.Enabled,
			ControlServer:          params.Config.Tailscale.ControlServer,
			AuthKey:                params.Config.Tailscale.AuthKey,
			ExitNode:               params.Config.Tailscale.ExitNode,
			ExitNodeAllowLANAccess: params.Config.Tailscale.ExitNodeAllowLANAccess,
			AdvertiseExitNode:      params.Config.Tailscale.AdvertiseExitNode,
			TUNMode:                tunMode,
		}
	}

	if params.Config.ZeroTier != nil {
		cfg.ZeroTier = &meshvpn.ZeroTierConfig{
			Enabled:   params.Config.ZeroTier.Enabled,
			NetworkID: params.Config.ZeroTier.NetworkID,
		}
	}

	if err := manager.SetConfig(cfg); err != nil {
		return nil, err
	}

	return rpcGetMeshVPNConfig()
}

func rpcMeshVPNInstall(provider string) (bool, error) {
	manager := getMeshVPNManager()
	if manager == nil {
		return false, errMeshVPNNotInitialized
	}

	ctx := context.Background()

	// Install with progress reporting via RPC events
	err := manager.Install(ctx, provider, func(progress float64) {
		if currentSession != nil {
			writeJSONRPCEvent("meshVPNInstallProgress", map[string]interface{}{
				"provider": provider,
				"progress": progress,
			}, currentSession)
		}
	})

	if err != nil {
		return false, err
	}

	return true, nil
}

func rpcMeshVPNUninstall(provider string) (bool, error) {
	manager := getMeshVPNManager()
	if manager == nil {
		return false, errMeshVPNNotInitialized
	}

	ctx := context.Background()
	if err := manager.Uninstall(ctx, provider); err != nil {
		return false, err
	}

	// Disable auto-start after uninstall
	if err := manager.DisableProvider(provider); err != nil {
		logger.Warn().Err(err).Msg("failed to disable provider config after uninstall")
	}

	return true, nil
}

func rpcMeshVPNConnect(params RpcMeshVPNConnectParams) (*RpcMeshVPNConnectResult, error) {
	manager := getMeshVPNManager()
	if manager == nil {
		return nil, errMeshVPNNotInitialized
	}

	providerName := params.Provider
	if providerName == "" {
		return nil, fmt.Errorf("provider name is required")
	}

	ctx := context.Background()
	result, err := manager.ConnectProvider(ctx, providerName, meshvpn.ConnectOptions{
		ControlServer: params.ControlServer,
		AuthKey:       params.AuthKey,
	})

	if err != nil {
		return nil, err
	}

	// Enable the provider in config for auto-start on next boot
	if err := manager.EnableProvider(providerName, params.ControlServer, params.AuthKey); err != nil {
		logger.Warn().Err(err).Msg("failed to save VPN config for auto-start")
	}

	// Start status monitoring for this provider
	manager.StartProviderStatusMonitor(ctx, providerName)

	// Restart mDNS for ZeroTier to advertise on the new virtual interface
	if providerName == "zerotier" {
		restartMdns()
	}

	logger.Info().
		Str("provider", providerName).
		Str("authUrl", result.AuthURL).
		Msg("meshVPNConnect returning result")

	return &RpcMeshVPNConnectResult{
		Success: true,
		AuthURL: result.AuthURL,
	}, nil
}

func rpcMeshVPNDisconnect(params struct {
	Provider string `json:"provider"`
}) (*meshvpn.ProviderStatus, error) {
	manager := getMeshVPNManager()
	if manager == nil {
		return nil, errMeshVPNNotInitialized
	}

	providerName := params.Provider
	if providerName == "" {
		// For backward compatibility, use first running provider
		if provider := manager.GetActiveProvider(); provider != nil {
			providerName = provider.Name()
		}
	}

	if providerName == "" {
		return nil, meshvpn.ErrNoActiveProvider
	}

	logger.Info().Str("provider", providerName).Msg("meshVPNDisconnect called")

	ctx := context.Background()
	err := manager.DisconnectProvider(ctx, providerName)
	if err != nil {
		logger.Error().Err(err).Msg("meshVPNDisconnect failed")
		return nil, err
	}

	// Disable auto-start on reboot (but keep network config like network ID)
	if err := manager.DisableProvider(providerName); err != nil {
		logger.Warn().Err(err).Msg("failed to disable VPN provider in config")
	}

	// Stop status monitoring for this provider
	manager.StopProviderStatusMonitor(providerName)

	// Return the new status directly to ensure UI updates
	status, err := manager.GetProviderStatus(ctx, providerName)
	if err != nil {
		logger.Info().Msg("meshVPNDisconnect returning fallback stopped status")
		return &meshvpn.ProviderStatus{
			State:     meshvpn.StateStopped,
			Installed: true,
			Running:   false,
		}, nil
	}

	logger.Info().Str("state", string(status.State)).Msg("meshVPNDisconnect returning status")
	return status, nil
}

func rpcMeshVPNLogout(params struct {
	Provider string `json:"provider"`
}) (bool, error) {
	manager := getMeshVPNManager()
	if manager == nil {
		return false, errMeshVPNNotInitialized
	}

	providerName := params.Provider
	if providerName == "" {
		// For backward compatibility, use first running provider
		if provider := manager.GetActiveProvider(); provider != nil {
			providerName = provider.Name()
		}
	}

	if providerName == "" {
		return false, meshvpn.ErrNoActiveProvider
	}

	ctx := context.Background()
	err := manager.LogoutProvider(ctx, providerName)
	if err != nil {
		return false, err
	}

	// Disable auto-start on logout
	if err := manager.DisableProvider(providerName); err != nil {
		logger.Warn().Err(err).Msg("failed to disable VPN provider in config")
	}

	// Stop status monitoring for this provider
	manager.StopProviderStatusMonitor(providerName)

	return true, nil
}

func rpcMeshVPNGetExitNodes() ([]meshvpn.ExitNode, error) {
	manager := getMeshVPNManager()
	if manager == nil {
		return nil, errMeshVPNNotInitialized
	}

	ctx := context.Background()
	return manager.ListExitNodes(ctx)
}

func rpcMeshVPNSetExitNode(params RpcMeshVPNSetExitNodeParams) (bool, error) {
	manager := getMeshVPNManager()
	if manager == nil {
		return false, errMeshVPNNotInitialized
	}

	ctx := context.Background()
	err := manager.SetExitNode(ctx, params.Hostname, params.AllowLAN)
	if err != nil {
		return false, err
	}

	return true, nil
}

func rpcMeshVPNClearExitNode() (bool, error) {
	manager := getMeshVPNManager()
	if manager == nil {
		return false, errMeshVPNNotInitialized
	}

	ctx := context.Background()
	err := manager.ClearExitNode(ctx)
	if err != nil {
		return false, err
	}

	return true, nil
}

func rpcMeshVPNGetVersionInfo(params struct {
	Provider string `json:"provider,omitempty"`
}) (*meshvpn.VersionInfo, error) {
	manager := getMeshVPNManager()
	if manager == nil {
		return nil, errMeshVPNNotInitialized
	}

	ctx := context.Background()
	return manager.GetVersionInfo(ctx, params.Provider)
}

func rpcMeshVPNUpdate(params RpcMeshVPNUpdateParams) (bool, error) {
	manager := getMeshVPNManager()
	if manager == nil {
		return false, errMeshVPNNotInitialized
	}

	ctx := context.Background()

	// Report update progress via RPC events
	err := manager.Update(ctx, params.Provider, params.TargetVersion, func(progress float64) {
		if currentSession != nil {
			writeJSONRPCEvent("meshVPNUpdateProgress", map[string]interface{}{
				"provider": params.Provider,
				"progress": progress,
			}, currentSession)
		}
	})

	if err != nil {
		return false, err
	}

	return true, nil
}

func rpcMeshVPNGetTUNMode(params struct {
	Provider string `json:"provider,omitempty"`
}) (*RpcMeshVPNGetTUNModeResult, error) {
	manager := getMeshVPNManager()
	if manager == nil {
		return nil, errMeshVPNNotInitialized
	}

	mode, err := manager.GetTUNMode(params.Provider)
	if err != nil {
		return nil, err
	}

	return &RpcMeshVPNGetTUNModeResult{
		Mode: string(mode),
	}, nil
}

func rpcMeshVPNSetTUNMode(params RpcMeshVPNSetTUNModeParams) (bool, error) {
	manager := getMeshVPNManager()
	if manager == nil {
		return false, errMeshVPNNotInitialized
	}

	tunMode, err := meshvpn.ParseTUNMode(params.Mode)
	if err != nil {
		return false, err
	}

	ctx := context.Background()
	if err := manager.SetTUNMode(ctx, params.Provider, tunMode); err != nil {
		return false, err
	}

	// Persist TUN mode to config
	cfg := manager.GetConfig()
	if cfg == nil {
		cfg = &meshvpn.Config{}
	}
	if cfg.Tailscale == nil {
		cfg.Tailscale = &meshvpn.TailscaleConfig{}
	}
	cfg.Tailscale.TUNMode = tunMode
	if err := manager.SetConfig(cfg); err != nil {
		logger.Warn().Err(err).Msg("failed to persist TUN mode to config")
	}

	return true, nil
}

func rpcMeshVPNSetAdvertiseExitNode(params RpcMeshVPNSetAdvertiseExitNodeParams) (bool, error) {
	manager := getMeshVPNManager()
	if manager == nil {
		return false, errMeshVPNNotInitialized
	}

	ctx := context.Background()
	if err := manager.SetAdvertiseExitNode(ctx, params.Provider, params.Advertise); err != nil {
		return false, err
	}

	cfg := manager.GetConfig()
	if cfg == nil {
		cfg = &meshvpn.Config{}
	}
	if cfg.Tailscale == nil {
		cfg.Tailscale = &meshvpn.TailscaleConfig{}
	}
	cfg.Tailscale.AdvertiseExitNode = params.Advertise
	if err := manager.SetConfig(cfg); err != nil {
		return false, fmt.Errorf("failed to persist advertise exit node to config: %w", err)
	}

	return true, nil
}
