package kvm

import (
	"context"
	"fmt"

	"github.com/jetkvm/kvm/internal/confparser"
	"github.com/jetkvm/kvm/internal/mdns"
	"github.com/jetkvm/kvm/internal/network/types"
	"github.com/jetkvm/kvm/pkg/nmlite"
)

const (
	NetIfName = "eth0"
)

var (
	networkManager *nmlite.NetworkManager
)

type RpcNetworkSettings struct {
	types.NetworkConfig
}

func (s *RpcNetworkSettings) ToNetworkConfig() *types.NetworkConfig {
	return &s.NetworkConfig
}

func toRpcNetworkSettings(config *types.NetworkConfig) *RpcNetworkSettings {
	return &RpcNetworkSettings{
		NetworkConfig: *config,
	}
}

func restartMdns() {
	if mDNS == nil {
		return
	}

	_ = mDNS.SetListenOptions(&mdns.MDNSListenOptions{
		IPv4: config.NetworkConfig.MDNSMode.String != "disabled",
		IPv6: config.NetworkConfig.MDNSMode.String != "disabled",
	})
	_ = mDNS.SetLocalNames([]string{
		networkManager.GetHostname(),
		networkManager.GetFQDN(),
	}, true)
}

func triggerTimeSyncOnNetworkStateChange() {
	if timeSync == nil {
		return
	}

	// set the NTP servers from the network manager
	if networkManager != nil {
		timeSync.SetDhcpNtpAddresses(networkManager.NTPServerStrings())
	}

	// sync time
	if err := timeSync.Sync(); err != nil {
		networkLogger.Error().Err(err).Msg("failed to sync time after network state change")
	}
}

func networkStateChanged(_ string, state types.InterfaceState) {
	// do not block the main thread
	go waitCtrlAndRequestDisplayUpdate(true, "network_state_changed")

	if state.Online {
		networkLogger.Info().Msg("network state changed to online, triggering time sync")
		triggerTimeSyncOnNetworkStateChange()
	}

	// always restart mDNS when the network state changes
	if mDNS != nil {
		restartMdns()
	}
}

func validateNetworkConfig() {
	err := confparser.SetDefaultsAndValidate(config.NetworkConfig)
	if err == nil {
		return
	}

	networkLogger.Error().Err(err).Msg("failed to validate config, reverting to default config")
	SaveBackupConfig()

	// do not use a pointer to the default config
	// it has been already changed during LoadConfig
	config.NetworkConfig = &(types.NetworkConfig{})
	SaveConfig()
}

func initNetwork() error {
	ensureConfigLoaded()

	// validate the config, if it's invalid, revert to the default config and save the backup
	validateNetworkConfig()

	networkManager = nmlite.NewNetworkManager(context.Background(), networkLogger)
	networkManager.SetOnInterfaceStateChange(networkStateChanged)
	if err := networkManager.AddInterface(NetIfName, config.NetworkConfig); err != nil {
		return fmt.Errorf("failed to add interface: %w", err)
	}

	return nil
}

func rpcGetNetworkState() *types.InterfaceState {
	state, _ := networkManager.GetInterfaceState(NetIfName)
	return state
}

func rpcGetNetworkSettings() *RpcNetworkSettings {
	return toRpcNetworkSettings(config.NetworkConfig)
}

func rpcSetNetworkSettings(settings RpcNetworkSettings) (*RpcNetworkSettings, error) {
	netConfig := settings.ToNetworkConfig()

	l := networkLogger.With().
		Str("interface", NetIfName).
		Interface("newConfig", netConfig).
		Logger()

	l.Debug().Msg("setting new config")

	s := networkManager.SetInterfaceConfig(NetIfName, netConfig)
	if s != nil {
		return nil, s
	}
	l.Debug().Msg("new config applied")

	newConfig, err := networkManager.GetInterfaceConfig(NetIfName)
	if err != nil {
		return nil, err
	}
	config.NetworkConfig = newConfig

	l.Debug().Msg("saving new config")
	if err := SaveConfig(); err != nil {
		return nil, err
	}

	return toRpcNetworkSettings(newConfig), nil
}

func rpcRenewDHCPLease() error {
	return networkManager.RenewDHCPLease(NetIfName)
}
