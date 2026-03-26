package kvm

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"net"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	wolPackets = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "jetkvm_wol_sent_packets_total",
			Help: "Total number of Wake-on-LAN magic packets sent.",
		},
	)
	wolErrors = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "jetkvm_wol_sent_packet_errors_total",
			Help: "Total number of Wake-on-LAN magic packets errors.",
		},
	)
)

// SendWOLMagicPacket sends a Wake-on-LAN magic packet to the specified MAC address.
// broadcastIP optionally overrides the default 255.255.255.255 broadcast address.
func rpcSendWOLMagicPacket(macAddress string, broadcastIP string) error {
	// Parse the MAC address
	mac, err := net.ParseMAC(macAddress)
	if err != nil {
		wolErrors.Inc()
		return ErrorfL(wolLogger, "invalid MAC address", err)
	}

	// Determine broadcast address
	target := "255.255.255.255"
	if broadcastIP != "" {
		if ip := net.ParseIP(broadcastIP); ip == nil || ip.To4() == nil {
			wolErrors.Inc()
			return ErrorfL(wolLogger, "invalid broadcast IP address", fmt.Errorf("invalid IP: %s", broadcastIP))
		}
		target = broadcastIP
	}

	// Create the magic packet
	packet := createMagicPacket(mac)

	// Set up UDP connection
	conn, err := net.Dial("udp", target+":9")
	if err != nil {
		wolErrors.Inc()
		return ErrorfL(wolLogger, "failed to establish UDP connection", err)
	}
	defer conn.Close()

	// Send the packet
	_, err = conn.Write(packet)
	if err != nil {
		wolErrors.Inc()
		return ErrorfL(wolLogger, "failed to send WOL packet", err)
	}

	wolLogger.Info().Str("mac", macAddress).Msg("WOL packet sent")
	wolPackets.Inc()

	return nil
}

// createMagicPacket creates a Wake-on-LAN magic packet
func createMagicPacket(mac net.HardwareAddr) []byte {
	var buf bytes.Buffer

	// Write 6 bytes of 0xFF
	buf.Write(bytes.Repeat([]byte{0xFF}, 6))

	// Write the target MAC address 16 times
	for range 16 {
		_ = binary.Write(&buf, binary.BigEndian, mac)
	}

	return buf.Bytes()
}
