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

func getMdnsOptions() *mdns.MDNSOptions {
	if networkManager == nil {
		return nil
	}

	var ipv4, ipv6 bool
	switch config.NetworkConfig.MDNSMode.String {
	case "auto":
		ipv4 = true
		ipv6 = true
	case "ipv4_only":
		ipv4 = true
	case "ipv6_only":
		ipv6 = true
	}

	return &mdns.MDNSOptions{
		LocalNames: []string{
			networkManager.Hostname(),
			networkManager.FQDN(),
		},
		ListenOptions: &mdns.MDNSListenOptions{
			IPv4: ipv4,
			IPv6: ipv6,
		},
	}
}

func restartMdns() {
	if mDNS == nil {
		return
	}

	options := getMdnsOptions()
	if options == nil {
		return
	}

	if err := mDNS.SetOptions(options); err != nil {
		networkLogger.Error().Err(err).Msg("failed to restart mDNS")
	}
}

func triggerTimeSyncOnNetworkStateChange() {
	if timeSync == nil {
		return
	}

	// set the NTP servers from the network manager
	if networkManager != nil {
		ntpServers := make([]string, len(networkManager.NTPServers()))
		for i, server := range networkManager.NTPServers() {
			ntpServers[i] = server.String()
		}
		networkLogger.Info().Strs("ntpServers", ntpServers).Msg("setting NTP servers from network manager")
		timeSync.SetDhcpNtpAddresses(ntpServers)
	}

	// sync time
	go func() {
		if err := timeSync.Sync(); err != nil {
			networkLogger.Error().Err(err).Msg("failed to sync time after network state change")
		}
	}()
}

func networkStateChanged(_ string, state types.InterfaceState) {
	// do not block the main thread
	go waitCtrlAndRequestDisplayUpdate(true, "network_state_changed")

	if currentSession != nil {
		writeJSONRPCEvent("networkState", state.ToRpcInterfaceState(), currentSession)
	}

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
	if err := SaveBackupConfig(); err != nil {
		networkLogger.Error().Err(err).Msg("failed to save backup config")
	}

	// do not use a pointer to the default config
	// it has been already changed during LoadConfig
	config.NetworkConfig = &(types.NetworkConfig{})
	if err := SaveConfig(); err != nil {
		networkLogger.Error().Err(err).Msg("failed to save config")
	}
}

func initNetwork() error {
	ensureConfigLoaded()

	// validate the config, if it's invalid, revert to the default config and save the backup
	validateNetworkConfig()

	nc := config.NetworkConfig

	nm := nmlite.NewNetworkManager(context.Background(), networkLogger)
	networkLogger.Info().Interface("networkConfig", nc).Str("hostname", nc.Hostname.String).Str("domain", nc.Domain.String).Msg("initializing network manager")
	_ = setHostname(nm, nc.Hostname.String, nc.Domain.String)
	nm.SetOnInterfaceStateChange(networkStateChanged)
	if err := nm.AddInterface(NetIfName, nc); err != nil {
		return fmt.Errorf("failed to add interface: %w", err)
	}

	networkManager = nm

	return nil
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

func rpcGetNetworkState() *types.RpcInterfaceState {
	state, _ := networkManager.GetInterfaceState(NetIfName)
	return state.ToRpcInterfaceState()
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

	var rebootRequired bool
	if netConfig.DHCPClient.String != config.NetworkConfig.DHCPClient.String {
		rebootRequired = true
	}

	_ = setHostname(networkManager, netConfig.Hostname.String, netConfig.Domain.String)

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

	if rebootRequired {
		if err := rpcReboot(false); err != nil {
			return nil, err
		}
	}

	return toRpcNetworkSettings(newConfig), nil
}

func rpcRenewDHCPLease() error {
	return networkManager.RenewDHCPLease(NetIfName)
}

func rpcToggleDHCPClient() error {
	switch config.NetworkConfig.DHCPClient.String {
	case "jetdhcpc":
		config.NetworkConfig.DHCPClient.String = "udhcpc"
	case "udhcpc":
		config.NetworkConfig.DHCPClient.String = "jetdhcpc"
	}

	if err := SaveConfig(); err != nil {
		return err
	}

	return rpcReboot(false)
}
