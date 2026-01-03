package kvm

import (
	"context"
	"errors"
	"fmt"

	"github.com/jetkvm/kvm/internal/meshvpn"
)

var errMeshVPNNotInitialized = errors.New("mesh VPN not initialized")

// RPC types for mesh VPN
// Note: meshvpn.ProviderInfo, meshvpn.ProviderStatus, meshvpn.ExitNode, and
// meshvpn.VersionInfo are used directly for RPC since they have correct JSON tags.
// Config types below use camelCase for RPC vs snake_case in internal types.

// RpcMeshVPNConfig contains configuration for RPC
type RpcMeshVPNConfig struct {
	ActiveProvider string                      `json:"activeProvider,omitempty"`
	Tailscale      *RpcTailscaleProviderConfig `json:"tailscale,omitempty"`
}

// RpcTailscaleProviderConfig contains Tailscale-specific config for RPC
type RpcTailscaleProviderConfig struct {
	Enabled                bool   `json:"enabled"`
	ControlServer          string `json:"controlServer,omitempty"`
	AuthKey                string `json:"authKey,omitempty"`
	ExitNode               string `json:"exitNode,omitempty"`
	ExitNodeAllowLANAccess bool   `json:"exitNodeAllowLanAccess,omitempty"`
	AdvertiseExitNode      bool   `json:"advertiseExitNode,omitempty"`
	TUNMode                string `json:"tunMode,omitempty"`
}

// RpcMeshVPNConnectParams contains parameters for connect RPC
type RpcMeshVPNConnectParams struct {
	Provider      string `json:"provider,omitempty"`
	ControlServer string `json:"controlServer,omitempty"`
	AuthKey       string `json:"authKey,omitempty"`
}

// RpcMeshVPNConnectResult contains result of connect RPC
type RpcMeshVPNConnectResult struct {
	Success bool   `json:"success"`
	AuthURL string `json:"authUrl,omitempty"`
}

// RpcMeshVPNSetExitNodeParams contains parameters for set exit node RPC
type RpcMeshVPNSetExitNodeParams struct {
	Hostname string `json:"hostname"`
	AllowLAN bool   `json:"allowLan"`
}

// RpcMeshVPNUpdateParams contains parameters for update RPC
type RpcMeshVPNUpdateParams struct {
	Provider      string `json:"provider,omitempty"`
	TargetVersion string `json:"targetVersion,omitempty"` // Empty means latest
}

// RpcMeshVPNSetTUNModeParams contains parameters for set TUN mode RPC
type RpcMeshVPNSetTUNModeParams struct {
	Provider string `json:"provider,omitempty"`
	Mode     string `json:"mode"` // "userspace" or "kernel"
}

// RpcMeshVPNGetTUNModeResult contains the result of get TUN mode RPC
type RpcMeshVPNGetTUNModeResult struct {
	Mode string `json:"mode"`
}

// RpcMeshVPNSetAdvertiseExitNodeParams contains parameters for set advertise exit node RPC
type RpcMeshVPNSetAdvertiseExitNodeParams struct {
	Provider  string `json:"provider,omitempty"`
	Advertise bool   `json:"advertise"`
}

// RPC Handler functions

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
		// If no active provider, return a default status instead of error
		if err == meshvpn.ErrNoActiveProvider {
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
		cfg.Tailscale = &meshvpn.TailscaleConfig{
			Enabled:                params.Config.Tailscale.Enabled,
			ControlServer:          params.Config.Tailscale.ControlServer,
			AuthKey:                params.Config.Tailscale.AuthKey,
			ExitNode:               params.Config.Tailscale.ExitNode,
			ExitNodeAllowLANAccess: params.Config.Tailscale.ExitNodeAllowLANAccess,
			AdvertiseExitNode:      params.Config.Tailscale.AdvertiseExitNode,
			TUNMode:                meshvpn.TUNMode(params.Config.Tailscale.TUNMode),
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
	err := manager.Uninstall(ctx, provider)
	if err != nil {
		return false, err
	}

	return true, nil
}

func rpcMeshVPNConnect(params RpcMeshVPNConnectParams) (*RpcMeshVPNConnectResult, error) {
	manager := getMeshVPNManager()
	if manager == nil {
		return nil, errMeshVPNNotInitialized
	}

	// Set active provider if specified
	providerName := params.Provider
	if providerName != "" {
		if err := manager.SetActiveProvider(providerName); err != nil {
			return nil, err
		}
	} else {
		// Get current active provider name
		if provider := manager.GetActiveProvider(); provider != nil {
			providerName = provider.Name()
		}
	}

	ctx := context.Background()
	result, err := manager.Connect(ctx, meshvpn.ConnectOptions{
		ControlServer: params.ControlServer,
		AuthKey:       params.AuthKey,
	})

	if err != nil {
		return nil, err
	}

	// Enable the provider in config for auto-start on next boot
	if providerName != "" {
		if err := manager.EnableProvider(providerName, params.ControlServer, params.AuthKey); err != nil {
			logger.Warn().Err(err).Msg("failed to save VPN config for auto-start")
		}
	}

	// Start status monitoring
	manager.StartStatusMonitor(ctx)

	logger.Info().
		Str("authUrl", result.AuthURL).
		Msg("meshVPNConnect returning result")

	return &RpcMeshVPNConnectResult{
		Success: true,
		AuthURL: result.AuthURL,
	}, nil
}

func rpcMeshVPNDisconnect() (bool, error) {
	manager := getMeshVPNManager()
	if manager == nil {
		return false, errMeshVPNNotInitialized
	}

	ctx := context.Background()
	err := manager.Disconnect(ctx)
	if err != nil {
		return false, err
	}

	return true, nil
}

func rpcMeshVPNLogout() (bool, error) {
	manager := getMeshVPNManager()
	if manager == nil {
		return false, errMeshVPNNotInitialized
	}

	// Get provider name before logout
	var providerName string
	if provider := manager.GetActiveProvider(); provider != nil {
		providerName = provider.Name()
	}

	ctx := context.Background()
	err := manager.Logout(ctx)
	if err != nil {
		return false, err
	}

	// Disable auto-start on logout
	if providerName != "" {
		if err := manager.DisableProvider(providerName); err != nil {
			logger.Warn().Err(err).Msg("failed to disable VPN provider in config")
		}
	}

	// Stop status monitoring
	manager.StopStatusMonitor()

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

	ctx := context.Background()
	err := manager.SetTUNMode(ctx, params.Provider, meshvpn.TUNMode(params.Mode))
	if err != nil {
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
	cfg.Tailscale.TUNMode = meshvpn.TUNMode(params.Mode)
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
