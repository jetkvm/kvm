package kvm

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"os/exec"

	"github.com/hashicorp/go-envparse"
	"github.com/pion/mdns/v2"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"

	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netlink/nl"
)

var mDNSConn *mdns.Conn

var networkState NetworkState

type NetworkState struct {
	Up   bool
	IPv4 string
	IPv6 string
	MAC  string

	checked bool
}

type LocalIpInfo struct {
	IPv4 string
	IPv6 string
	MAC  string
}

const (
	NetIfName     = "eth0"
	DHCPLeaseFile = "/run/udhcpc.%s.info"
)

// setDhcpClientState sends signals to udhcpc to change it's current mode
// of operation. Setting active to true will force udhcpc to renew the DHCP lease.
// Setting active to false will put udhcpc into idle mode.
func setDhcpClientState(active bool) {
	var signal string
	if active {
		signal = "-SIGUSR1"
	} else {
		signal = "-SIGUSR2"
	}

	cmd := exec.Command("/usr/bin/killall", signal, "udhcpc")
	if err := cmd.Run(); err != nil {
		logger.Warn().Err(err).Msg("network: setDhcpClientState: failed to change udhcpc state")
	}
}

func checkNetworkState() {
	iface, err := netlink.LinkByName(NetIfName)
	if err != nil {
		logger.Warn().Err(err).Str("interface", NetIfName).Msg("failed to get interface")
		return
	}

	newState := NetworkState{
		Up:  iface.Attrs().OperState == netlink.OperUp,
		MAC: iface.Attrs().HardwareAddr.String(),

		checked: true,
	}

	addrs, err := netlink.AddrList(iface, nl.FAMILY_ALL)
	if err != nil {
		logger.Warn().Err(err).Str("interface", NetIfName).Msg("failed to get addresses")
	}

	// If the link is going down, put udhcpc into idle mode.
	// If the link is coming back up, activate udhcpc and force it to renew the lease.
	if newState.Up != networkState.Up {
		setDhcpClientState(newState.Up)
	}

	for _, addr := range addrs {
		if addr.IP.To4() != nil {
			if !newState.Up && networkState.Up {
				// If the network is going down, remove all IPv4 addresses from the interface.
				logger.Info().Str("address", addr.IP.String()).Msg("network: state transitioned to down, removing IPv4 address")
				err := netlink.AddrDel(iface, &addr)
				if err != nil {
					logger.Warn().Err(err).Str("address", addr.IP.String()).Msg("network: failed to delete address")
				}

				newState.IPv4 = "..."
			} else {
				newState.IPv4 = addr.IP.String()
			}
		} else if addr.IP.To16() != nil && newState.IPv6 == "" {
			newState.IPv6 = addr.IP.String()
		}
	}

	if newState != networkState {
		logger.Info().Msg("network state changed")
		// restart MDNS
		_ = startMDNS()
		networkState = newState
		requestDisplayUpdate()
	}
}

func startMDNS() error {
	// If server was previously running, stop it
	if mDNSConn != nil {
		logger.Info().Msg("Stopping mDNS server")
		err := mDNSConn.Close()
		if err != nil {
			logger.Warn().Err(err).Msg("failed to stop mDNS server")
		}
	}

	// Start a new server
	logger.Info().Msg("Starting mDNS server on jetkvm.local")
	addr4, err := net.ResolveUDPAddr("udp4", mdns.DefaultAddressIPv4)
	if err != nil {
		return err
	}

	addr6, err := net.ResolveUDPAddr("udp6", mdns.DefaultAddressIPv6)
	if err != nil {
		return err
	}

	l4, err := net.ListenUDP("udp4", addr4)
	if err != nil {
		return err
	}

	l6, err := net.ListenUDP("udp6", addr6)
	if err != nil {
		return err
	}

	mDNSConn, err = mdns.Server(ipv4.NewPacketConn(l4), ipv6.NewPacketConn(l6), &mdns.Config{
		LocalNames: []string{"jetkvm.local"}, //TODO: make it configurable
	})
	if err != nil {
		mDNSConn = nil
		return err
	}
	//defer server.Close()
	return nil
}

func getNTPServersFromDHCPInfo() ([]string, error) {
	buf, err := os.ReadFile(fmt.Sprintf(DHCPLeaseFile, NetIfName))
	if err != nil {
		// do not return error if file does not exist
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to load udhcpc info: %w", err)
	}

	// parse udhcpc info
	env, err := envparse.Parse(bytes.NewReader(buf))
	if err != nil {
		return nil, fmt.Errorf("failed to parse udhcpc info: %w", err)
	}

	val, ok := env["ntpsrv"]
	if !ok {
		return nil, nil
	}

	var servers []string

	for _, server := range strings.Fields(val) {
		if net.ParseIP(server) == nil {
			logger.Info().Str("server", server).Msg("invalid NTP server IP, ignoring")
		}
		servers = append(servers, server)
	}

	return servers, nil
}

func init() {
	ensureConfigLoaded()

	updates := make(chan netlink.LinkUpdate)
	done := make(chan struct{})

	if err := netlink.LinkSubscribe(updates, done); err != nil {
		logger.Warn().Err(err).Msg("failed to subscribe to link updates")
		return
	}

	go func() {
		waitCtrlClientConnected()
		checkNetworkState()
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case update := <-updates:
				if update.Link.Attrs().Name == NetIfName {
					logger.Info().Interface("update", update).Msg("link update")
					checkNetworkState()
				}
			case <-ticker.C:
				checkNetworkState()
			case <-done:
				return
			}
		}
	}()
	err := startMDNS()
	if err != nil {
		logger.Warn().Err(err).Msg("failed to run mDNS")
	}
}
