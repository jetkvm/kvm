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

var mDNSConn *mdns.Conn

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
		fmt.Println("network state changed")
		//restart MDNS
		startMDNS()
		networkState = newState
		requestDisplayUpdate()
	}
}

func startMDNS() error {
	//If server was previously running, stop it
	if mDNSConn != nil {
		fmt.Printf("Stopping mDNS server\n")
		err := mDNSConn.Close()
		if err != nil {
			fmt.Printf("failed to stop mDNS server: %v\n", err)
		}
	}

	//Start a new server
	fmt.Printf("Starting mDNS server on jetkvm.local\n")
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

func init() {
	updates := make(chan netlink.LinkUpdate)
	done := make(chan struct{})

	if err := netlink.LinkSubscribe(updates, done); err != nil {
		fmt.Println("failed to subscribe to link updates: %v", err)
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
	err := startMDNS()
	if err != nil {
		fmt.Println("failed to run mDNS: %v", err)
	}
}
