//go:build !arm && linux

package lldp

import (
	"net"
	"syscall"
)

func toRawSockaddr(mac net.HardwareAddr) (sockaddr syscall.RawSockaddr) {
	for i, n := range mac {
		sockaddr.Data[i] = int8(n)
	}
	return
}
