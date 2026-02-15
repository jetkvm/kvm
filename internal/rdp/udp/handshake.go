package udp

import (
	"fmt"
)

// HandleSYN processes the client's SYN packet and returns the SYN+ACK response.
// The securityCookie is used to validate the cookie hash in the SYN.
func (t *Transport) HandleSYN(synData []byte, securityCookie [16]byte) ([]byte, error) {
	syn, err := ParseSynPacket(synData)
	if err != nil {
		return nil, fmt.Errorf("parse SYN: %w", err)
	}

	// Validate cookie hash
	if !syn.HasCookieHash {
		return nil, ErrInvalidCookie
	}
	if !ValidateCookieHash(securityCookie, syn.CookieHash) {
		return nil, ErrInvalidCookie
	}

	// Validate version — we require RDPEUDP2 (v3)
	if syn.HasSYNEX {
		if syn.Version != RDPUDPVersion3 && syn.Version != RDPUDPVersion2 {
			return nil, fmt.Errorf("%w: client version 0x%04X", ErrInvalidVersion, syn.Version)
		}
	}

	// Store client's initial sequence number as our recv base
	t.recvSeqNum.Store(uint64(syn.InitialSeqNum))

	// Generate server's initial sequence number
	serverSeqNum := uint32(t.sendSeqNum.Load())

	// Negotiate MTU (conservative for reliable delivery)
	mtu := max(min(syn.UpstreamMTU, syn.DownstreamMTU, 1232), 64)

	t.logger.Debug().
		Uint32("clientSeqNum", syn.InitialSeqNum).
		Uint32("serverSeqNum", serverSeqNum).
		Uint16("negotiatedMTU", mtu).
		Uint16("clientVersion", syn.Version).
		Msg("UDP: processing SYN")

	// Build SYN+ACK
	synAck := BuildSynAckPacket(serverSeqNum, syn.InitialSeqNum, mtu, securityCookie)
	return synAck, nil
}

// HandleACK processes the client's ACK (third message of the 3-way handshake).
// After this, the transport transitions to RDPEUDP2 v3 data mode.
func (t *Transport) HandleACK(ackData []byte) error {
	if len(ackData) < 2 {
		return ErrPacketTooShort
	}

	flags := uint16(ackData[0]) | uint16(ackData[1])<<8
	if flags&RDPUDPFlagACK == 0 {
		return fmt.Errorf("udp: expected ACK flag in handshake ACK")
	}

	t.logger.Debug().Msg("UDP: handshake ACK received, transitioning to v3 data mode")

	// Transition to ready state — starts retransmit and ACK loops
	t.SetReady()
	return nil
}

// IsHandshakeState returns true if the transport is still in the handshake phase.
func (t *Transport) IsHandshakeState() bool {
	return t.state.Load() == stateHandshake
}
