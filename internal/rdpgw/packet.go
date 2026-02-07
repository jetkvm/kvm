package rdpgw

import (
	"encoding/binary"
	"fmt"
	"io"
)

// MS-TSGU wire format: 8-byte LE header + payload
// Header: pktType(u16) + reserved(u16) + totalSize(u32)
const packetHeaderSize = 8

// maxPacketSize is the maximum allowed packet size to prevent memory exhaustion.
const maxPacketSize = 64 * 1024

// readPacket reads one MS-TSGU packet from r.
// Returns the packet type and payload (header excluded from payload).
func readPacket(r io.Reader) (pktType uint16, payload []byte, err error) {
	var hdr [packetHeaderSize]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return 0, nil, fmt.Errorf("read header: %w", err)
	}

	pktType = binary.LittleEndian.Uint16(hdr[0:2])
	// hdr[2:4] reserved
	totalSize := binary.LittleEndian.Uint32(hdr[4:8])

	if totalSize < packetHeaderSize {
		return 0, nil, fmt.Errorf("invalid packet size %d", totalSize)
	}
	if totalSize > maxPacketSize {
		return 0, nil, fmt.Errorf("packet too large: %d bytes", totalSize)
	}

	payloadSize := totalSize - packetHeaderSize
	if payloadSize == 0 {
		return pktType, nil, nil
	}

	payload = make([]byte, payloadSize)
	if _, err := io.ReadFull(r, payload); err != nil {
		return 0, nil, fmt.Errorf("read payload: %w", err)
	}

	return pktType, payload, nil
}

// writePacket writes one MS-TSGU packet to w.
func writePacket(w io.Writer, pktType uint16, payload []byte) error {
	totalSize := uint32(packetHeaderSize + len(payload))
	var hdr [packetHeaderSize]byte
	binary.LittleEndian.PutUint16(hdr[0:2], pktType)
	// hdr[2:4] reserved = 0
	binary.LittleEndian.PutUint32(hdr[4:8], totalSize)

	if _, err := w.Write(hdr[:]); err != nil {
		return fmt.Errorf("write header: %w", err)
	}
	if len(payload) > 0 {
		if _, err := w.Write(payload); err != nil {
			return fmt.Errorf("write payload: %w", err)
		}
	}
	return nil
}

// parseHandshakeRequest parses a HANDSHAKE_REQUEST payload.
// Returns the client's extended authentication capabilities flags.
// MS-TSGU 2.2.9.2.1.1
func parseHandshakeRequest(data []byte) (extAuth uint16, err error) {
	// Minimum: verMajor(1) + verMinor(1) + clientVersion(2) + extAuth(2) = 6
	if len(data) < 6 {
		return 0, fmt.Errorf("handshake request too short: %d", len(data))
	}
	extAuth = binary.LittleEndian.Uint16(data[4:6])
	return extAuth, nil
}

// buildHandshakeResponse builds a HANDSHAKE_RESPONSE payload.
// We advertise the same extended auth capabilities the client requested.
func buildHandshakeResponse(extAuth uint16) []byte {
	// MS-TSGU 2.2.9.2.1.2: errorCode(4) + verMajor(1) + verMinor(1) + serverVersion(2) + extAuth(2) = 10
	buf := make([]byte, 10)
	binary.LittleEndian.PutUint32(buf[0:4], errorCodeSuccess)
	buf[4] = 0 // verMajor
	buf[5] = 0 // verMinor
	binary.LittleEndian.PutUint16(buf[6:8], 0) // serverVersion
	binary.LittleEndian.PutUint16(buf[8:10], extAuth)
	return buf
}

// parseTunnelCreate parses a TUNNEL_CREATE payload.
// Returns the client's capability flags and PAA cookie string.
// MS-TSGU 2.2.9.2.1.3
func parseTunnelCreate(data []byte) (capabilities uint32, cookie string, err error) {
	// Minimum: capsFlags(4) + fieldsPresent(2) + reserved(2) = 8
	if len(data) < 8 {
		return 0, "", fmt.Errorf("tunnel create too short: %d", len(data))
	}

	capabilities = binary.LittleEndian.Uint32(data[0:4])
	fieldsPresent := binary.LittleEndian.Uint16(data[4:6])
	// data[6:8] reserved

	offset := 8

	// PAA cookie is present when HTTP_EXTENDED_AUTH_PAA was negotiated
	if fieldsPresent&httpExtendedAuthPAA != 0 {
		// cookieLen(2) is byte count of UTF-16LE string (including null terminator)
		if offset+2 > len(data) {
			return 0, "", fmt.Errorf("tunnel create: missing cookie length")
		}
		cookieLen := int(binary.LittleEndian.Uint16(data[offset : offset+2]))
		offset += 2

		if cookieLen > 0 {
			if offset+cookieLen > len(data) {
				return 0, "", fmt.Errorf("tunnel create: cookie data truncated")
			}
			cookie = decodeUTF16LE(data[offset : offset+cookieLen])
		}
	}

	return capabilities, cookie, nil
}

// buildTunnelResponse builds a TUNNEL_RESPONSE payload.
// MS-TSGU 2.2.9.2.1.4
func buildTunnelResponse(tunnelID uint32, caps uint32) []byte {
	// serverVersion(2) + errorCode(4) + fieldsPresent(2) + reserved(2) + tunnelID(4) + caps(4) = 18
	fields := uint16(httpTunnelResponseFieldTunnelID | httpTunnelResponseFieldCaps)
	buf := make([]byte, 18)
	binary.LittleEndian.PutUint16(buf[0:2], 0) // serverVersion
	binary.LittleEndian.PutUint32(buf[2:6], errorCodeSuccess)
	binary.LittleEndian.PutUint16(buf[6:8], fields)
	// buf[8:10] reserved
	binary.LittleEndian.PutUint32(buf[10:14], tunnelID)
	binary.LittleEndian.PutUint32(buf[14:18], caps)
	return buf
}

// parseTunnelAuth parses a TUNNEL_AUTH payload.
// Returns the client machine name.
// MS-TSGU 2.2.9.2.1.5
func parseTunnelAuth(data []byte) (clientName string, err error) {
	// Minimum: clientNameLen(2) = 2
	if len(data) < 2 {
		return "", fmt.Errorf("tunnel auth too short: %d", len(data))
	}

	nameLen := int(binary.LittleEndian.Uint16(data[0:2]))
	if nameLen > 0 && 2+nameLen <= len(data) {
		clientName = decodeUTF16LE(data[2 : 2+nameLen])
	}

	return clientName, nil
}

// buildTunnelAuthResponse builds a TUNNEL_AUTH_RESPONSE payload.
// MS-TSGU 2.2.9.2.1.5
func buildTunnelAuthResponse(redirectFlags uint32, idleTimeout uint32) []byte {
	// errorCode(4) + fieldsPresent(2) + reserved(2) + redirectFlags(4) + idleTimeout(4) = 16
	buf := make([]byte, 16)
	binary.LittleEndian.PutUint32(buf[0:4], errorCodeSuccess)
	binary.LittleEndian.PutUint16(buf[4:6], 0x03) // fields: redirect + idle timeout
	// buf[6:8] reserved
	binary.LittleEndian.PutUint32(buf[8:12], redirectFlags)
	binary.LittleEndian.PutUint32(buf[12:16], idleTimeout) // 0 = no timeout
	return buf
}

// parseChannelCreate parses a CHANNEL_CREATE payload.
// Returns the target server name and port.
// MS-TSGU 2.2.9.2.1.6
func parseChannelCreate(data []byte) (server string, port uint16, err error) {
	// Minimum: numResources(1) + numAltResources(1) + port(2) + protocol(2) + nameLen(2) = 8
	if len(data) < 8 {
		return "", 0, fmt.Errorf("channel create too short: %d", len(data))
	}

	// numResources at [0], numAltResources at [1]
	port = binary.LittleEndian.Uint16(data[2:4])
	// protocol at [4:6]
	nameLen := int(binary.LittleEndian.Uint16(data[6:8]))

	if nameLen > 0 && 8+nameLen <= len(data) {
		server = decodeUTF16LE(data[8 : 8+nameLen])
	}

	return server, port, nil
}

// buildChannelResponse builds a CHANNEL_RESPONSE payload.
// Includes the channel ID and optionally the UDP port for ShortPath.
// MS-TSGU 2.2.9.2.1.6
func buildChannelResponse(channelID uint32, udpPort uint16) []byte {
	fields := uint16(httpChannelResponseFieldChannelID)
	size := 14 // errorCode(4) + fieldsPresent(2) + reserved(2) + channelID(4) + padding(2)

	if udpPort > 0 {
		fields |= httpChannelResponseFieldUDPPort
		size += 2 // udpPort(2)
	}

	buf := make([]byte, size)
	binary.LittleEndian.PutUint32(buf[0:4], errorCodeSuccess)
	binary.LittleEndian.PutUint16(buf[4:6], fields)
	// buf[6:8] reserved
	binary.LittleEndian.PutUint32(buf[8:12], channelID)
	// buf[12:14] padding
	off := 14
	if udpPort > 0 {
		binary.LittleEndian.PutUint16(buf[off:off+2], udpPort)
	}

	return buf
}

// buildCloseChannelResponse builds a CLOSE_CHANNEL_RESPONSE payload.
func buildCloseChannelResponse() []byte {
	// errorCode(4) + fieldsPresent(2) + reserved(2) = 8
	buf := make([]byte, 8)
	binary.LittleEndian.PutUint32(buf[0:4], errorCodeSuccess)
	return buf
}
