// Package link provides a wrapper around netlink.Link and provides a singleton netlink manager.
package link

import (
	"errors"
	"fmt"
	"net"

	"github.com/jetkvm/kvm/internal/sync"

	"github.com/vishvananda/netlink"
)

var (
	ipv4DefaultRoute = net.IPNet{
		IP:   net.IPv4zero,
		Mask: net.CIDRMask(0, 0),
	}

	ipv6DefaultRoute = net.IPNet{
		IP:   net.IPv6zero,
		Mask: net.CIDRMask(0, 0),
	}

	// Singleton instance
	netlinkManagerInstance *NetlinkManager
	netlinkManagerOnce     sync.Once

	// ErrInterfaceUpTimeout is the error returned when the interface does not come up within the timeout
	ErrInterfaceUpTimeout = errors.New("timeout after waiting for an interface to come up")
	// ErrInterfaceUpCanceled is the error returned when the interface does not come up due to context cancellation
	ErrInterfaceUpCanceled = errors.New("context canceled while waiting for an interface to come up")
)

// Link is a wrapper around netlink.Link
type Link struct {
	netlink.Link
	mu sync.Mutex
}

// All lock actions should be done in external functions
// and the internal functions should not be called directly

func (l *Link) refresh() error {
	linkName := l.ifName()
	link, err := netlink.LinkByName(linkName)
	if err != nil {
		return err
	}
	if link == nil {
		return fmt.Errorf("link not found: %s", linkName)
	}
	l.Link = link
	return nil
}

func (l *Link) attrs() *netlink.LinkAttrs {
	return l.Link.Attrs()
}

func (l *Link) ifName() string {
	attrs := l.attrs()
	if attrs.Name == "" {
		return ""
	}
	return attrs.Name
}

// Refresh refreshes the link
func (l *Link) Refresh() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.refresh()
}

// Attrs returns the attributes of the link
func (l *Link) Attrs() *netlink.LinkAttrs {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.attrs()
}

// Interface returns the interface of the link
func (l *Link) Interface() *net.Interface {
	l.mu.Lock()
	defer l.mu.Unlock()

	ifname := l.ifName()
	if ifname == "" {
		return nil
	}
	iface, err := net.InterfaceByName(ifname)
	if err != nil {
		return nil
	}
	return iface
}

// HardwareAddr returns the hardware address of the link
func (l *Link) HardwareAddr() net.HardwareAddr {
	l.mu.Lock()
	defer l.mu.Unlock()

	attrs := l.attrs()
	if attrs.HardwareAddr == nil {
		return nil
	}
	return attrs.HardwareAddr
}

// AddrList returns the addresses of the link
func (l *Link) AddrList(family int) ([]netlink.Addr, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	return netlink.AddrList(l.Link, family)
}

func (l *Link) SetMTU(mtu int) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	return netlink.LinkSetMTU(l.Link, mtu)
}

// HasGlobalUnicastAddress returns true if the link has a global unicast address
func (l *Link) HasGlobalUnicastAddress() bool {
	addrs, err := l.AddrList(AfUnspec)
	if err != nil {
		return false
	}

	for _, addr := range addrs {
		if addr.IP.IsGlobalUnicast() {
			return true
		}
	}
	return false
}

// IsSame checks if the link is the same as another link
func (l *Link) IsSame(other *Link) bool {
	if l == nil || other == nil {
		return false
	}

	a := l.Attrs()
	b := other.Attrs()
	if a.OperState != b.OperState {
		return false
	}
	if a.Index != b.Index {
		return false
	}
	if a.MTU != b.MTU {
		return false
	}
	if a.HardwareAddr.String() != b.HardwareAddr.String() {
		return false
	}
	return true
}
