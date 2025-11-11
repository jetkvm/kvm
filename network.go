package kvm

import (
	"context"
	"fmt"
	"net"
	"reflect"

	"github.com/jetkvm/kvm/internal/confparser"
	"github.com/jetkvm/kvm/internal/lldp"
	"github.com/jetkvm/kvm/internal/mdns"
	"github.com/jetkvm/kvm/internal/network/types"
	"github.com/jetkvm/kvm/pkg/nmlite"
)

const (
	NetIfName = "eth0"
)

var (
	networkManager *nmlite.NetworkManager
	lldpService    *lldp.LLDP
)

type RpcNetworkSettings struct {
	types.NetworkConfig
}

func (s *RpcNetworkSettings) ToNetworkConfig() *types.NetworkConfig {
	return &s.NetworkConfig
}

type PostRebootAction struct {
	HealthCheck string `json:"healthCheck"`
	RedirectTo  string `json:"redirectTo"`
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

	// update the LLDP advertise options
	if lldpService != nil {
		_ = lldpService.SetAdvertiseOptions(getLLDPAdvertiseOptions(&state))
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

func getLLDPAdvertiseOptions(state *types.InterfaceState) *lldp.AdvertiseOptions {
	a := &lldp.AdvertiseOptions{
		SysDescription:      toLLDPSysDescription(),
		SysCapabilities:     []string{"other", "router", "wlanap"},
		EnabledCapabilities: []string{"other"},
	}
	if state == nil {
		return a
	}

	a.SysName = state.Hostname
	ip4String := state.IPv4Address
	if ip4String != "" {
		ip4 := net.ParseIP(ip4String)
		a.IPv4Address = &ip4
	}
	ip6String := state.IPv6Address
	if ip6String != "" {
		ip6 := net.ParseIP(ip6String)
		a.IPv6Address = &ip6
	}
	networkLogger.Info().Interface("advertiseOptions", a).Msg("LLDP advertise options")
	return a
}

func initNetwork() error {
	ensureConfigLoaded()

	// validate the config, if it's invalid, revert to the default config and save the backup
	validateNetworkConfig()

	nc := config.NetworkConfig

	nm := nmlite.NewNetworkManager(context.Background(), networkLogger)
	networkLogger.Info().Interface("networkConfig", nc).Str("hostname", nc.Hostname.String).Str("domain", nc.Domain.String).Msg("initializing network manager")

	if err := setHostname(nm, nc.Hostname.String, nc.Domain.String); err != nil {
		networkLogger.Warn().Err(err).Msg("failed to set hostname")
	}

	nm.SetOnInterfaceStateChange(networkStateChanged)

	if err := nm.AddInterface(NetIfName, nc); err != nil {
		return fmt.Errorf("failed to add interface: %w", err)
	}

	if err := nm.CleanUpLegacyDHCPClients(); err != nil {
		networkLogger.Warn().Err(err).Msg("failed to clean up legacy DHCP clients")
	}

	networkManager = nm

	ifState, err := nm.GetInterfaceState(NetIfName)
	if err != nil {
		networkLogger.Warn().Err(err).Msg("failed to get interface state, LLDP will use the default options")
	}

	advertiseOptions := getLLDPAdvertiseOptions(ifState)
	lldpService = lldp.NewLLDP(&lldp.Options{
		InterfaceName:    NetIfName,
		AdvertiseOptions: advertiseOptions,
		OnChange: func(neighbors []lldp.Neighbor) {
			// TODO: send deltas instead of the whole list
			writeJSONRPCEvent("lldpNeighbors", neighbors, currentSession)
		},
		Logger: networkLogger,
	})

	// this will start up the LLDP Tx and Rx as needed
	if err := lldpService.SetRxAndTx(nc.ShouldEnableLLDPReceive(), nc.ShouldEnableLLDPTransmit()); err != nil {
		networkLogger.Error().Err(err).Msg("failed to initialize LLDP RX and TX")
	}

	return nil
}

func toLLDPSysDescription() string {
	systemVersion, appVersion, err := GetLocalVersion()
	if err == nil {
		return fmt.Sprintf("JetKVM (app: %s)", GetBuiltAppVersion())
	}

	return fmt.Sprintf("JetKVM (app: %s, system: %s)", appVersion.String(), systemVersion.String())
}

func updateLLDPOptions(nc *types.NetworkConfig, ifState *types.InterfaceState) {
	if lldpService == nil {
		return
	}

	if err := lldpService.SetRxAndTx(nc.ShouldEnableLLDPReceive(), nc.ShouldEnableLLDPTransmit()); err != nil {
		networkLogger.Error().Err(err).Msg("failed to update LLDP RX and TX")
	}

	if ifState == nil {
		newIfState, err := networkManager.GetInterfaceState(NetIfName)
		if err != nil {
			networkLogger.Warn().Err(err).Msg("failed to get interface state, LLDP will use the default options")
			return
		}
		ifState = newIfState
	}

	advertiseOptions := getLLDPAdvertiseOptions(ifState)
	if err := lldpService.SetAdvertiseOptions(advertiseOptions); err != nil {
		networkLogger.Error().Err(err).Msg("failed to set LLDP advertise options")
	}
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

func shouldRebootForNetworkChange(oldConfig, newConfig *types.NetworkConfig) (rebootRequired bool, postRebootAction *PostRebootAction) {
	rebootReasons := []string{}
	defer func() {
		if len(rebootReasons) > 0 {
			networkLogger.Info().Strs("reasons", rebootReasons).Msg("reboot required")
		}
	}()
	oldDhcpClient := oldConfig.DHCPClient.String

	l := networkLogger.With().
		Interface("old", oldConfig).
		Interface("new", newConfig).
		Logger()

	// DHCP client change always requires reboot
	newDhcpClient := newConfig.DHCPClient.String
	if newDhcpClient != oldDhcpClient {
		rebootRequired = true
		rebootReasons = append(rebootReasons, fmt.Sprintf("DHCP client changed from %s to %s", oldDhcpClient, newDhcpClient))
		return rebootRequired, postRebootAction
	}

	oldIPv4Mode := oldConfig.IPv4Mode.String
	newIPv4Mode := newConfig.IPv4Mode.String

	// IPv4 mode change requires reboot
	if newIPv4Mode != oldIPv4Mode {
		rebootRequired = true
		rebootReasons = append(rebootReasons, fmt.Sprintf("IPv4 mode changed from %s to %s", oldIPv4Mode, newIPv4Mode))

		if newIPv4Mode == "static" && oldIPv4Mode != "static" {
			postRebootAction = &PostRebootAction{
				HealthCheck: fmt.Sprintf("//%s/device/status", newConfig.IPv4Static.Address.String),
				RedirectTo:  fmt.Sprintf("//%s", newConfig.IPv4Static.Address.String),
			}
			l.Info().Interface("postRebootAction", postRebootAction).Msg("IPv4 mode changed to static, reboot required")
		}

		return rebootRequired, postRebootAction
	}

	// IPv4 static config changes require reboot
	// but if it's not activated, don't care about the changes
	if !reflect.DeepEqual(oldConfig.IPv4Static, newConfig.IPv4Static) && newIPv4Mode == "static" {
		rebootRequired = true
		// TODO: do not restart if it's just the DNS servers that changed
		rebootReasons = append(rebootReasons, "IPv4 static config changed")

		// Handle IP change for redirect (only if both are not nil and IP changed)
		if newConfig.IPv4Static != nil && oldConfig.IPv4Static != nil &&
			newConfig.IPv4Static.Address.String != oldConfig.IPv4Static.Address.String {
			postRebootAction = &PostRebootAction{
				HealthCheck: fmt.Sprintf("//%s/device/status", newConfig.IPv4Static.Address.String),
				RedirectTo:  fmt.Sprintf("//%s", newConfig.IPv4Static.Address.String),
			}
		}

		return rebootRequired, postRebootAction
	}

	// IPv6 mode change requires reboot when using udhcpc
	oldIPv6Mode := oldConfig.IPv6Mode.String
	newIPv6Mode := newConfig.IPv6Mode.String
	if newConfig.IPv6Mode.String != oldConfig.IPv6Mode.String && oldDhcpClient == "udhcpc" {
		rebootRequired = true
		rebootReasons = append(rebootReasons, fmt.Sprintf("IPv6 mode changed from %s to %s when using udhcpc", oldIPv6Mode, newIPv6Mode))
	}

	return rebootRequired, postRebootAction
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

	// TODO: do not restart everything if it's just the LLDP mode that changed

	// Check if reboot is needed
	rebootRequired, postRebootAction := shouldRebootForNetworkChange(config.NetworkConfig, netConfig)

	// If reboot required, send willReboot event before applying network config
	if rebootRequired {
		l.Info().Msg("Sending willReboot event before applying network config")
		writeJSONRPCEvent("willReboot", postRebootAction, currentSession)
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

	// update the LLDP advertise options
	updateLLDPOptions(newConfig, nil)

	l.Debug().Msg("saving new config")
	if err := SaveConfig(); err != nil {
		return nil, err
	}

	if rebootRequired {
		l.Info().Msg("Rebooting due to network changes")
		if err := hwReboot(true, postRebootAction, 0); err != nil {
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

	return rpcReboot(true)
}

func rpcGetLLDPNeighbors() []lldp.Neighbor {
	if lldpService == nil {
		return []lldp.Neighbor{}
	}
	return lldpService.GetNeighbors()
}
