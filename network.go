package kvm

import (
	"fmt"
	"github.com/pion/mdns/v2"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
	"net"
	"time"

	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netlink/nl"
)

var networkState struct {
	Up   bool
	IPv4 string
	IPv6 string
	MAC  string
}

type LocalIpInfo struct {
	IPv4 string
	IPv6 string
	MAC  string
}

func checkLoopback() {
	iface, err := netlink.LinkByName("lo")	
	if err != nil {
		fmt.Printf("failed to get lo interface: %v\n", err)
		return
	}

	if iface.Attrs().OperState != netlink.OperUp {
		fmt.Printf("lo is not up, bringing it up\n")
		err = netlink.LinkSetUp(iface)
		if err != nil {
			fmt.Printf("failed to bring lo up: %v\n", err)
			return
		}
	}
}

func checkNetworkState() {
	iface, err := netlink.LinkByName("eth0")
	if err != nil {
		fmt.Printf("failed to get eth0 interface: %v\n", err)
		return
	}

	newState := struct {
		Up   bool
		IPv4 string
		IPv6 string
		MAC  string
	}{
		Up:  iface.Attrs().OperState == netlink.OperUp,
		MAC: iface.Attrs().HardwareAddr.String(),
	}

	addrs, err := netlink.AddrList(iface, nl.FAMILY_ALL)
	if err != nil {
		fmt.Printf("failed to get addresses for eth0: %v\n", err)
	}

	for _, addr := range addrs {
		if addr.IP.To4() != nil {
			newState.IPv4 = addr.IP.String()
		} else if addr.IP.To16() != nil && newState.IPv6 == "" {
			newState.IPv6 = addr.IP.String()
		}
	}

	if newState != networkState {
		networkState = newState
		fmt.Println("network state changed")
		requestDisplayUpdate()
	}
}

func startMDNS() error {
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

	_, err = mdns.Server(ipv4.NewPacketConn(l4), ipv6.NewPacketConn(l6), &mdns.Config{
		LocalNames: []string{"jetkvm.local"}, //TODO: make it configurable
	})
	if err != nil {
		return err
	}
	//defer server.Close()
	return nil
}

func init() {
	updates := make(chan netlink.LinkUpdate)
	done := make(chan struct{})

	if err := netlink.LinkSubscribe(updates, done); err != nil {
		fmt.Println("failed to subscribe to link updates: %v", err)
		return
	}

	//Start loopback before launching network task
	checkLoopback()

	go func() {
		waitCtrlClientConnected()
		checkNetworkState()
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case update := <-updates:
				if update.Link.Attrs().Name == "eth0" {
					fmt.Printf("link update: %+v\n", update)
					checkNetworkState()
				}
			case <-ticker.C:
				checkNetworkState()
			case <-done:
				return
			}
		}
	}()
	fmt.Println("Starting mDNS server")
	err := startMDNS()
	if err != nil {
		fmt.Println("failed to run mDNS: %v", err)
	}
}
