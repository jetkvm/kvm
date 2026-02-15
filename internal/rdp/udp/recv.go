package udp

import "time"

// ProcessIncomingPacket handles an incoming RDPEUDP2 v3 data packet.
// Called by the server's UDP read loop — not a dedicated goroutine.
func (t *Transport) ProcessIncomingPacket(data []byte) {
	if t.state.Load() == stateClosed {
		return
	}

	// Reverse RDPEUDP2 obfuscation
	if len(data) < 8 {
		return
	}
	ApplyPacketPrefix(data)

	// Parse v3 data packet
	pkt, err := ParseDataPacket(data, t.recvSeqNum.Load(), t.sendSeqNum.Load())
	if err != nil {
		t.logger.Debug().Err(err).Msg("UDP: failed to parse incoming packet")
		return
	}

	// Process ACK payload
	if pkt.Ack != nil {
		t.processAck(pkt.Ack)
	}

	// Process data payload
	if pkt.Data != nil {
		t.processData(pkt.Data)
	}
}

// processAck handles an incoming ACK, marking packets as acknowledged.
func (t *Transport) processAck(ack *AckPayload) {
	t.sendBufMu.Lock()
	defer t.sendBufMu.Unlock()

	// The ACK acknowledges all packets up to and including ack.SeqNum
	var acked int
	for seq, sp := range t.sendBuf {
		if seq <= ack.SeqNum {
			rttSample := time.Since(sp.sendTime)
			// Only use RTT from non-retransmitted packets (Karn's algorithm)
			if sp.retries == 0 {
				t.cc.onAck(rttSample)
			}
			delete(t.sendBuf, seq)
			acked++
		}
	}

	// Wake up writers waiting for cwnd space
	if acked > 0 {
		t.sendCond.Broadcast()
	}
}

// processData handles an incoming data payload.
func (t *Transport) processData(dp *DataPayload) {
	t.recvBufMu.Lock()
	defer t.recvBufMu.Unlock()

	expected := t.recvSeqNum.Load()

	if dp.SeqNum == expected {
		// In-order: deliver immediately and check for buffered continuations
		t.deliverData(dp.Data)
		t.recvSeqNum.Add(1)

		// Deliver any buffered contiguous packets
		t.deliverInOrder()
	} else if dp.SeqNum > expected {
		// Out-of-order: buffer for later delivery
		// Copy data since the caller's buffer may be reused
		dataCopy := make([]byte, len(dp.Data))
		copy(dataCopy, dp.Data)
		t.recvBuf[dp.SeqNum] = dataCopy
	}
	// dp.SeqNum < expected: duplicate, ignore

	// Mark ACK as pending
	t.ackPending.Store(true)
}

// deliverInOrder delivers buffered packets that are now contiguous.
// Must be called under recvBufMu.
func (t *Transport) deliverInOrder() {
	for {
		nextSeq := t.recvSeqNum.Load()
		data, ok := t.recvBuf[nextSeq]
		if !ok {
			break
		}
		delete(t.recvBuf, nextSeq)
		t.deliverData(data)
		t.recvSeqNum.Add(1)
	}
}

// deliverData sends data to the read channel (non-blocking).
func (t *Transport) deliverData(data []byte) {
	select {
	case t.readChan <- data:
	default:
		// Read channel full — drop oldest and retry
		select {
		case <-t.readChan:
		default:
		}
		select {
		case t.readChan <- data:
		default:
			t.logger.Warn().Msg("UDP: readChan full, dropping data")
		}
	}
}

// sendAck sends a standalone ACK packet.
func (t *Transport) sendAck() {
	ackSeq := t.recvSeqNum.Load()
	raw := BuildAckOnlyPacket(ackSeq, maxRcvWindowSize)
	raw = t.padAndObfuscate(raw)

	if _, err := t.udpConn.WriteToUDP(raw, t.remoteAddr); err != nil {
		t.logger.Debug().Err(err).Msg("UDP: failed to send ACK")
	}
}
