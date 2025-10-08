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
}

func (l *Link) Refresh() error {
	linkName := l.Link.Attrs().Name
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

// Attrs returns the attributes of the link
func (l *Link) Attrs() *netlink.LinkAttrs {
	return l.Link.Attrs()
}

func (l *Link) AddrList(family int) ([]netlink.Addr, error) {
	return netlink.AddrList(l, family)
}

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
