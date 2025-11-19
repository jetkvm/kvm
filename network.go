package kvm

import (
	"fmt"
	"reflect"

	"github.com/jetkvm/kvm/internal/network"
	"github.com/jetkvm/kvm/internal/ota"
	"github.com/jetkvm/kvm/internal/udhcpc"
)

const (
	NetIfName = "eth0"
)

var (
	networkState *network.NetworkInterfaceState
)

func networkStateChanged(isOnline bool) {
	// do not block the main thread
	go waitCtrlAndRequestDisplayUpdate(true, "network_state_changed")

	if timeSync != nil {
		if networkState != nil {
			timeSync.SetDhcpNtpAddresses(networkState.NtpAddressesString())
		}

		if err := timeSync.Sync(); err != nil {
			networkLogger.Error().Err(err).Msg("failed to sync time after network state change")
		}
	}

	// always restart mDNS when the network state changes
	if mDNS != nil {
		_ = mDNS.SetListenOptions(config.NetworkConfig.GetMDNSMode())
		_ = mDNS.SetLocalNames([]string{
			networkState.GetHostname(),
			networkState.GetFQDN(),
		}, true)
	}

	// if the network is now online, trigger an NTP sync if still needed
	if isOnline && timeSync != nil && (isTimeSyncNeeded() || !timeSync.IsSyncSuccess()) {
		if err := timeSync.Sync(); err != nil {
			logger.Warn().Str("error", err.Error()).Msg("unable to sync time on network state change")
		}
	}
}

func initNetwork() error {
	ensureConfigLoaded()

	state, err := network.NewNetworkInterfaceState(&network.NetworkInterfaceOptions{
		DefaultHostname: GetDefaultHostname(),
		InterfaceName:   NetIfName,
		NetworkConfig:   config.NetworkConfig,
		Logger:          networkLogger,
		OnStateChange: func(state *network.NetworkInterfaceState) {
			networkStateChanged(state.IsOnline())
		},
		OnInitialCheck: func(state *network.NetworkInterfaceState) {
			networkStateChanged(state.IsOnline())
		},
		OnDhcpLeaseChange: func(lease *udhcpc.Lease, state *network.NetworkInterfaceState) {
			networkStateChanged(state.IsOnline())

			if currentSession == nil {
				return
			}

			writeJSONRPCEvent("networkState", networkState.RpcGetNetworkState(), currentSession)
		},
		OnConfigChange: func(networkConfig *network.NetworkConfig) {
			config.NetworkConfig = networkConfig
			networkStateChanged(false)

			if mDNS != nil {
				_ = mDNS.SetListenOptions(networkConfig.GetMDNSMode())
				_ = mDNS.SetLocalNames([]string{
					networkState.GetHostname(),
					networkState.GetFQDN(),
				}, true)
			}
		},
	})
}

func setHostname(nm *nmlite.NetworkManager, hostname, domain string) error {
	if nm == nil {
		return nil
	}

	if hostname == "" {
		hostname = GetDefaultHostname()
	}

	return nm.SetHostname(hostname, domain)
}

func shouldRebootForNetworkChange(oldConfig, newConfig *types.NetworkConfig) (rebootRequired bool, postRebootAction *ota.PostRebootAction) {
	oldDhcpClient := oldConfig.DHCPClient.String

	l := networkLogger.With().
		Interface("old", oldConfig).
		Interface("new", newConfig).
		Logger()

	// DHCP client change always requires reboot
	if newConfig.DHCPClient.String != oldDhcpClient {
		rebootRequired = true
		l.Info().Msg("DHCP client changed, reboot required")
		return rebootRequired, postRebootAction
	}

	oldIPv4Mode := oldConfig.IPv4Mode.String
	newIPv4Mode := newConfig.IPv4Mode.String

	// IPv4 mode change requires reboot
	if newIPv4Mode != oldIPv4Mode {
		rebootRequired = true
		l.Info().Msg("IPv4 mode changed with udhcpc, reboot required")

		if newIPv4Mode == "static" && oldIPv4Mode != "static" {
			postRebootAction = &ota.PostRebootAction{
				HealthCheck: fmt.Sprintf("//%s/device/status", newConfig.IPv4Static.Address.String),
				RedirectTo:  fmt.Sprintf("//%s", newConfig.IPv4Static.Address.String),
			}
			l.Info().Interface("postRebootAction", postRebootAction).Msg("IPv4 mode changed to static, reboot required")
		}

		return rebootRequired, postRebootAction
	}

	// IPv4 static config changes require reboot
	if !reflect.DeepEqual(oldConfig.IPv4Static, newConfig.IPv4Static) {
		rebootRequired = true

		// Handle IP change for redirect (only if both are not nil and IP changed)
		if newConfig.IPv4Static != nil && oldConfig.IPv4Static != nil &&
			newConfig.IPv4Static.Address.String != oldConfig.IPv4Static.Address.String {
			postRebootAction = &ota.PostRebootAction{
				HealthCheck: fmt.Sprintf("//%s/device/status", newConfig.IPv4Static.Address.String),
				RedirectTo:  fmt.Sprintf("//%s", newConfig.IPv4Static.Address.String),
			}

			l.Info().Interface("postRebootAction", postRebootAction).Msg("IPv4 static config changed, reboot required")
		}

		return rebootRequired, postRebootAction
	}

	// IPv6 mode change requires reboot when using udhcpc
	if newConfig.IPv6Mode.String != oldConfig.IPv6Mode.String && oldDhcpClient == "udhcpc" {
		rebootRequired = true
		l.Info().Msg("IPv6 mode changed with udhcpc, reboot required")
	}

	return rebootRequired, postRebootAction
}

func rpcGetNetworkState() network.RpcNetworkState {
	return networkState.RpcGetNetworkState()
}

func rpcGetNetworkSettings() network.RpcNetworkSettings {
	return networkState.RpcGetNetworkSettings()
}

func rpcSetNetworkSettings(settings network.RpcNetworkSettings) (*network.RpcNetworkSettings, error) {
	s := networkState.RpcSetNetworkSettings(settings)
	if s != nil {
		return nil, s
	}

	if err := SaveConfig(); err != nil {
		return nil, err
	}

	return &network.RpcNetworkSettings{NetworkConfig: *config.NetworkConfig}, nil
}

func rpcRenewDHCPLease() error {
	return networkState.RpcRenewDHCPLease()
}
