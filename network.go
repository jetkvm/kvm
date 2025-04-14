package kvm

import (
	"os"

	"github.com/jetkvm/kvm/internal/network"
	"github.com/jetkvm/kvm/internal/udhcpc"
)

const (
	NetIfName = "eth0"
)

var (
	networkState *network.NetworkInterfaceState
)

func initNetwork() {
	ensureConfigLoaded()

	networkState = network.NewNetworkInterfaceState(&network.NetworkInterfaceOptions{
		InterfaceName: NetIfName,
		NetworkConfig: config.NetworkConfig,
		Logger:        networkLogger,
		OnStateChange: func(state *network.NetworkInterfaceState) {
			waitCtrlAndRequestDisplayUpdate(true)
		},
		OnInitialCheck: func(state *network.NetworkInterfaceState) {
			waitCtrlAndRequestDisplayUpdate(true)
		},
		OnDhcpLeaseChange: func(lease *udhcpc.Lease) {
			waitCtrlAndRequestDisplayUpdate(true)
		},
	})

	err := networkState.Run()
	if err != nil {
		networkLogger.Error().Err(err).Msg("failed to run network state")
		os.Exit(1)
	}
}
