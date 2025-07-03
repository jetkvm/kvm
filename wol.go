package kvm

import (
	"bytes"
	"encoding/binary"
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

// SendWOLMagicPacket sends a Wake-on-LAN magic packet to the specified MAC address
func rpcSendWOLMagicPacket(macAddress string) error {
	// Parse the MAC address
	mac, err := net.ParseMAC(macAddress)
	if err != nil {
		wolErrors.Inc()
		return ErrorfL(wolLogger, "invalid MAC address", err)
	}

	// Create the magic packet
	packet := createMagicPacket(mac)

	// Set up UDP connection
	conn, err := net.Dial("udp", "255.255.255.255:9")
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
	for i := 0; i < 16; i++ {
		_ = binary.Write(&buf, binary.BigEndian, mac)
	}

	return buf.Bytes()
}
