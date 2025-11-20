package kvm

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"reflect"
	"time"

	"github.com/jetkvm/kvm/internal/confparser"
	"github.com/jetkvm/kvm/internal/mdns"
	"github.com/jetkvm/kvm/internal/network/types"
	"github.com/jetkvm/kvm/internal/ota"
	"github.com/jetkvm/kvm/pkg/myip"
	"github.com/jetkvm/kvm/pkg/nmlite"
	"github.com/jetkvm/kvm/pkg/nmlite/link"
)

const (
	NetIfName = "eth0"
)

var (
	networkManager *nmlite.NetworkManager
	publicIPState  *myip.PublicIPState
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

func setPublicIPReadyState(ipv4Ready, ipv6Ready bool) {
	if publicIPState == nil {
		return
	}
	publicIPState.SetIPv4AndIPv6(ipv4Ready, ipv6Ready)
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

	setPublicIPReadyState(state.IPv4Ready, state.IPv6Ready)

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
	_ = nm.CleanUpLegacyDHCPClients()

	networkManager = nm

	return nil
}

func initPublicIPState() {
	// the feature will be only enabled if the cloud has been adopted
	// due to privacy reasons

	// but it will be initialized anyway to avoid nil pointer dereferences
	ps := myip.NewPublicIPState(&myip.PublicIPStateConfig{
		Logger:             networkLogger,
		CloudflareEndpoint: config.CloudURL,
		APIEndpoint:        "",
		IPv4:               false,
		IPv6:               false,
		HttpClientGetter: func(family int) *http.Client {
			transport := http.DefaultTransport.(*http.Transport).Clone()
			transport.Proxy = config.NetworkConfig.GetTransportProxyFunc()
			transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
				netType := network
				switch family {
				case link.AfInet:
					netType = "tcp4"
				case link.AfInet6:
					netType = "tcp6"
				}
				return (&net.Dialer{}).DialContext(ctx, netType, addr)
			}

			return &http.Client{
				Transport: transport,
				Timeout:   30 * time.Second,
			}
		},
	})
	publicIPState = ps
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

	if newConfig.Hostname.String != oldConfig.Hostname.String {
		rebootRequired = true
		l.Info().Msg("Hostname changed, reboot required")
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

func rpcGetPublicIPAddresses(refresh bool) ([]myip.PublicIP, error) {
	if publicIPState == nil {
		return nil, fmt.Errorf("public IP state not initialized")
	}

	if refresh {
		if err := publicIPState.ForceUpdate(); err != nil {
			return nil, err
		}
	}

	return publicIPState.GetAddresses(), nil
}

func rpcCheckPublicIPAddresses() error {
	if publicIPState == nil {
		return fmt.Errorf("public IP state not initialized")
	}

	return publicIPState.ForceUpdate()
}
