package kvm

import (
	"fmt"

	"github.com/jetkvm/kvm/internal/network"
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
	go waitCtrlAndRequestDisplayUpdate(true)

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

	if state == nil {
		if err == nil {
			return fmt.Errorf("failed to create NetworkInterfaceState")
		}
		return err
	}

	if err := state.Run(); err != nil {
		return err
	}

	networkState = state

	return nil
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
