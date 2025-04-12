package kvm

import (
	"net"

	"github.com/pion/mdns/v2"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

var mDNSConn *mdns.Conn

func startMDNS() error {
	// If server was previously running, stop it
	if mDNSConn != nil {
		logger.Info().Msg("stopping mDNS server")
		err := mDNSConn.Close()
		if err != nil {
			logger.Warn().Err(err).Msg("failed to stop mDNS server")
		}
	}

	// Start a new server
	hostname := "jetkvm.local"

	scopedLogger := logger.With().Str("hostname", hostname).Logger()
	scopedLogger.Info().Msg("starting mDNS server")

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
		LocalNames:    []string{hostname}, //TODO: make it configurable
		LoggerFactory: defaultLoggerFactory,
	})
	if err != nil {
		scopedLogger.Warn().Err(err).Msg("failed to start mDNS server")
		mDNSConn = nil
		return err
	}
	//defer server.Close()
	return nil
}
