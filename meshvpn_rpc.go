package kvm

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jetkvm/kvm/internal/meshvpn"
)

var errMeshVPNNotInitialized = errors.New("mesh VPN not initialized")

// requireManager returns the MeshVPN manager or an error if not initialized.
func requireManager() (*meshvpn.Manager, error) {
	manager := getMeshVPNManager()
	if manager == nil {
		return nil, errMeshVPNNotInitialized
	}
	return manager, nil
}

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
	manager, err := requireManager()
	if err != nil {
		return nil, err
	}
	return manager.ListProviders(), nil
}

func rpcGetMeshVPNStatus(provider string) (*meshvpn.ProviderStatus, error) {
	logger.Info().Str("provider", provider).Msg("rpcGetMeshVPNStatus: starting")

	manager, err := requireManager()
	if err != nil {
		logger.Warn().Err(err).Str("provider", provider).Msg("rpcGetMeshVPNStatus: manager not initialized")
		return nil, err
	}

	// Use a timeout to prevent hanging when the CLI tries to connect to a non-existent daemon socket.
	// Tailscale CLI can hang indefinitely if tailscaled is not running.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var status *meshvpn.ProviderStatus
	providerName := provider

	logger.Info().Str("provider", providerName).Msg("rpcGetMeshVPNStatus: fetching provider status")

	if providerName != "" {
		status, err = manager.GetProviderStatus(ctx, providerName)
	} else {
		status, err = manager.GetStatus(ctx)
		// If we got status from the active provider, get its name
		if err == nil && status != nil {
			if provider := manager.GetActiveProvider(); provider != nil {
				providerName = provider.Name()
			}
		}
	}

	logger.Info().Str("provider", providerName).Err(err).Msg("rpcGetMeshVPNStatus: after GetProviderStatus")

	if err != nil {
		// Check if it was a timeout
		if ctx.Err() == context.DeadlineExceeded {
			logger.Warn().Str("provider", providerName).Msg("rpcGetMeshVPNStatus: timeout waiting for status")
			// Check if provider is installed to return accurate status
			installed := false
			if providerName != "" {
				if p, ok := manager.GetProvider(providerName); ok && p != nil {
					installed = p.IsInstalled()
				}
			}
			return &meshvpn.ProviderStatus{
				Provider:     providerName,
				State:        meshvpn.StateError,
				Installed:    installed,
				Running:      false,
				ErrorMessage: "Status request timed out - VPN service may be unresponsive",
			}, nil
		}
		if errors.Is(err, meshvpn.ErrNoActiveProvider) {
			logger.Info().Str("provider", providerName).Msg("rpcGetMeshVPNStatus: no active provider")
			return &meshvpn.ProviderStatus{
				Provider:  providerName,
				State:     meshvpn.StateNotInstalled,
				Installed: false,
				Running:   false,
			}, nil
		}
		logger.Warn().Err(err).Str("provider", providerName).Msg("rpcGetMeshVPNStatus: error getting status")
		return nil, err
	}

	// Ensure provider field is set
	if providerName != "" {
		status.Provider = providerName
	}

	logger.Info().Str("provider", providerName).Str("state", string(status.State)).Msg("rpcGetMeshVPNStatus: completed")
	return status, nil
}

func rpcGetMeshVPNConfig() (*RpcMeshVPNConfig, error) {
	manager, err := requireManager()
	if err != nil {
		return nil, err
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
	manager, err := requireManager()
	if err != nil {
		return nil, err
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
	manager, err := requireManager()
	if err != nil {
		return false, err
	}

	ctx := context.Background()

	// Install with progress reporting via RPC events
	installErr := manager.Install(ctx, provider, func(progress float64) {
		if currentSession != nil {
			writeJSONRPCEvent("meshVPNInstallProgress", map[string]interface{}{
				"provider": provider,
				"progress": progress,
			}, currentSession)
		}
	})

	if installErr != nil {
		return false, installErr
	}

	return true, nil
}

func rpcMeshVPNUninstall(provider string) (bool, error) {
	manager, err := requireManager()
	if err != nil {
		return false, err
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
	logger.Info().
		Str("provider", params.Provider).
		Str("controlServer", params.ControlServer).
		Bool("hasAuthKey", params.AuthKey != "").
		Msg("rpcMeshVPNConnect: starting")

	manager, err := requireManager()
	if err != nil {
		logger.Error().Err(err).Msg("rpcMeshVPNConnect: manager not initialized")
		return nil, err
	}

	providerName := params.Provider
	if providerName == "" {
		logger.Error().Msg("rpcMeshVPNConnect: provider name is empty")
		return nil, fmt.Errorf("provider name is required")
	}

	logger.Info().Str("provider", providerName).Msg("rpcMeshVPNConnect: calling ConnectProvider")

	ctx := context.Background()
	result, err := manager.ConnectProvider(ctx, providerName, meshvpn.ConnectOptions{
		ControlServer: params.ControlServer,
		AuthKey:       params.AuthKey,
	})

	if err != nil {
		logger.Error().Err(err).Str("provider", providerName).Msg("rpcMeshVPNConnect: ConnectProvider failed")
		return nil, err
	}

	logger.Info().
		Str("provider", providerName).
		Str("authUrl", result.AuthURL).
		Msg("rpcMeshVPNConnect: ConnectProvider succeeded")

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

	return &RpcMeshVPNConnectResult{
		Success: true,
		AuthURL: result.AuthURL,
	}, nil
}

func rpcMeshVPNDisconnect(provider string) (*meshvpn.ProviderStatus, error) {
	manager, err := requireManager()
	if err != nil {
		return nil, err
	}

	providerName := provider
	if providerName == "" {
		// For backward compatibility, use first running provider
		if provider := manager.GetActiveProvider(); provider != nil {
			providerName = provider.Name()
		}
	}

	if providerName == "" {
		return nil, meshvpn.ErrNoActiveProvider
	}

	ctx := context.Background()
	if err := manager.DisconnectProvider(ctx, providerName); err != nil {
		return nil, err
	}

	// Disable auto-start on reboot (but keep network config like network ID)
	if err := manager.DisableProvider(providerName); err != nil {
		logger.Warn().Err(err).Msg("failed to disable VPN provider in config")
	}

	// Stop status monitoring for this provider
	manager.StopProviderStatusMonitor(providerName)

	status, err := manager.GetProviderStatus(ctx, providerName)
	if err != nil {
		logger.Warn().Err(err).Msg("failed to get status after disconnect")
		return &meshvpn.ProviderStatus{
			Provider:     providerName,
			State:        meshvpn.StateStopped,
			Installed:    true,
			Running:      false,
			ErrorMessage: "disconnected but status unknown: " + err.Error(),
		}, nil
	}

	// Ensure provider field is set in the returned status
	status.Provider = providerName
	return status, nil
}

func rpcMeshVPNLogout(provider string) (bool, error) {
	manager, err := requireManager()
	if err != nil {
		return false, err
	}

	providerName := provider
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
	err = manager.LogoutProvider(ctx, providerName)
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

func rpcMeshVPNGetExitNodes(provider string) ([]meshvpn.ExitNode, error) {
	manager, err := requireManager()
	if err != nil {
		return nil, err
	}
	return manager.ListExitNodesForProvider(context.Background(), provider)
}

func rpcMeshVPNSetExitNode(params RpcMeshVPNSetExitNodeParams) (bool, error) {
	manager, err := requireManager()
	if err != nil {
		return false, err
	}

	if err := manager.SetExitNode(context.Background(), params.Hostname, params.AllowLAN); err != nil {
		return false, err
	}
	return true, nil
}

func rpcMeshVPNClearExitNode() (bool, error) {
	manager, err := requireManager()
	if err != nil {
		return false, err
	}

	if err := manager.ClearExitNode(context.Background()); err != nil {
		return false, err
	}
	return true, nil
}

func rpcMeshVPNGetVersionInfo(provider string) (*meshvpn.VersionInfo, error) {
	manager, err := requireManager()
	if err != nil {
		return nil, err
	}
	return manager.GetVersionInfo(context.Background(), provider)
}

func rpcMeshVPNUpdate(provider string) (bool, error) {
	manager, err := requireManager()
	if err != nil {
		return false, err
	}

	ctx := context.Background()

	// Report update progress via RPC events (targetVersion empty = latest)
	updateErr := manager.Update(ctx, provider, "", func(progress float64) {
		if currentSession != nil {
			writeJSONRPCEvent("meshVPNUpdateProgress", map[string]interface{}{
				"provider": provider,
				"progress": progress,
			}, currentSession)
		}
	})

	if updateErr != nil {
		return false, updateErr
	}
	return true, nil
}

func rpcMeshVPNGetTUNMode(provider string) (*RpcMeshVPNGetTUNModeResult, error) {
	manager, err := requireManager()
	if err != nil {
		return nil, err
	}

	mode, err := manager.GetTUNMode(provider)
	if err != nil {
		return nil, err
	}
	return &RpcMeshVPNGetTUNModeResult{Mode: string(mode)}, nil
}

func rpcMeshVPNSetTUNMode(params RpcMeshVPNSetTUNModeParams) (bool, error) {
	manager, err := requireManager()
	if err != nil {
		return false, err
	}

	tunMode, err := meshvpn.ParseTUNMode(params.Mode)
	if err != nil {
		return false, err
	}

	if err := manager.SetTUNMode(context.Background(), params.Provider, tunMode); err != nil {
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
		return false, fmt.Errorf("failed to persist TUN mode to config: %w", err)
	}

	return true, nil
}

func rpcMeshVPNSetAdvertiseExitNode(params RpcMeshVPNSetAdvertiseExitNodeParams) (bool, error) {
	manager, err := requireManager()
	if err != nil {
		return false, err
	}

	if err := manager.SetAdvertiseExitNode(context.Background(), params.Provider, params.Advertise); err != nil {
		return false, err
	}

	// Persist setting to config
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
