package wol

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"net"
)

// SendWOLMagicPacket sends a Wake-on-LAN magic packet to the specified MAC address
func RPCSendWolMagicPacket(macAddress string) error {
	// Parse the MAC address
	mac, err := net.ParseMAC(macAddress)
	if err != nil {
		return fmt.Errorf("invalid MAC address: %v", err)
	}

	// Create the magic packet
	packet := CreateMagicPacket(mac)

	// Set up UDP connection
	conn, err := net.Dial("udp", "255.255.255.255:9")
	if err != nil {
		return fmt.Errorf("failed to establish UDP connection: %v", err)
	}
	defer conn.Close()

	// Send the packet
	_, err = conn.Write(packet)
	if err != nil {
		return fmt.Errorf("failed to send WOL packet: %v", err)
	}

	return nil
}

// CreateMagicPacket creates a Wake-on-LAN magic packet
func CreateMagicPacket(mac net.HardwareAddr) []byte {
	var buf bytes.Buffer

	// Write 6 bytes of 0xFF
	buf.Write(bytes.Repeat([]byte{0xFF}, 6))

	// Write the target MAC address 16 times
	for i := 0; i < 16; i++ {
		binary.Write(&buf, binary.BigEndian, mac)
	}

	return buf.Bytes()
}
