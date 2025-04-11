package kvm

import (
	"bytes"
	"encoding/binary"
	"net"
)

// SendWOLMagicPacket sends a Wake-on-LAN magic packet to the specified MAC address
func rpcSendWOLMagicPacket(macAddress string) error {
	// Parse the MAC address
	mac, err := net.ParseMAC(macAddress)
	if err != nil {
		return ErrorfL(wolLogger, "invalid MAC address", err)
	}

	// Create the magic packet
	packet := createMagicPacket(mac)

	// Set up UDP connection
	conn, err := net.Dial("udp", "255.255.255.255:9")
	if err != nil {
		return ErrorfL(wolLogger, "failed to establish UDP connection", err)
	}
	defer conn.Close()

	// Send the packet
	_, err = conn.Write(packet)
	if err != nil {
		return ErrorfL(wolLogger, "failed to send WOL packet", err)
	}

	wolLogger.Info().Str("mac", macAddress).Msg("WOL packet sent")

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
