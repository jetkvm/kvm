//go:build arm && linux

package lldp

import (
	"net"
	"syscall"
)

func toRawSockaddr(mac net.HardwareAddr) (sockaddr syscall.RawSockaddr) {
	for i, n := range mac {
		sockaddr.Data[i] = uint8(n)
	}
	return
}
