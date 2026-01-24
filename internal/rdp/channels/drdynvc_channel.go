package channels

import "encoding/binary"

// DVCChannel send operations and lifecycle management.
// This file contains the hot-path data sending code for DVC channels.

// SendData sends data on a channel using zero-allocation hot path.
// HOT PATH: This function is called for every DVC fragment.
// Uses pre-allocated fragBuf to avoid heap allocations.
func (ch *DVCChannel) SendData(data []byte) error {
	if !ch.Open {
		return ErrDVCChannelClosed
	}

	// Determine channel ID encoding (constant for the lifetime of the channel)
	cbID, idLen := channelIDEncoding(ch.ID)

	totalLen := len(data)

	// For small data, send in single PDU
	if totalLen <= DVCMaxDataSize {
		return ch.sendDataPDUZeroAlloc(data, false, 0, cbID, idLen)
	}

	// Fragment large data
	pos := 0
	first := true

	for pos < totalLen {
		chunkSize := totalLen - pos
		if chunkSize > DVCMaxDataSize {
			chunkSize = DVCMaxDataSize
		}

		var err error
		if first {
			err = ch.sendDataPDUZeroAlloc(data[pos:pos+chunkSize], true, uint32(totalLen), cbID, idLen)
			first = false
		} else {
			err = ch.sendDataPDUZeroAlloc(data[pos:pos+chunkSize], false, 0, cbID, idLen)
		}

		if err != nil {
			return err
		}
		pos += chunkSize
	}

	return nil
}

// sendDataPDUZeroAlloc sends a single data PDU using the pre-allocated fragment buffer.
// HOT PATH: Zero heap allocations.
func (ch *DVCChannel) sendDataPDUZeroAlloc(data []byte, isFirst bool, totalLen uint32, cbID byte, idLen int) error {
	pduType := byte(DVCData)
	lenFieldSize := 0
	cbIDLocal := cbID // Don't modify the passed-in cbID

	if isFirst {
		pduType = DVCDataFirst
		// Add length field for first PDU
		// The Len field is in bits 2-3 of the cmd byte:
		//   00 = 1 byte, 01 = 2 bytes, 10 = 4 bytes
		if totalLen > 0xFFFF {
			lenFieldSize = 4
			cbIDLocal |= 0x08 // Len=10 (bits 2-3) for 4-byte length
		} else if totalLen > 0xFF {
			lenFieldSize = 2
			cbIDLocal |= 0x04 // Len=01 (bits 2-3) for 2-byte length
		} else {
			lenFieldSize = 1
			// Len=00 (no bits set) for 1-byte length
		}
	}

	// Build PDU in pre-allocated buffer (zero allocation)
	buf := ch.fragBuf[:1+idLen+lenFieldSize+len(data)]
	buf[0] = pduType | cbIDLocal

	pos := 1
	switch idLen {
	case 1:
		buf[pos] = byte(ch.ID)
	case 2:
		binary.LittleEndian.PutUint16(buf[pos:pos+2], uint16(ch.ID))
	case 4:
		binary.LittleEndian.PutUint32(buf[pos:pos+4], ch.ID)
	}
	pos += idLen

	if isFirst {
		switch lenFieldSize {
		case 1:
			buf[pos] = byte(totalLen)
		case 2:
			binary.LittleEndian.PutUint16(buf[pos:pos+2], uint16(totalLen))
		case 4:
			binary.LittleEndian.PutUint32(buf[pos:pos+4], totalLen)
		}
		pos += lenFieldSize
	}

	copy(buf[pos:], data)

	return ch.manager.sendFunc(buf)
}

// Close closes the channel.
func (ch *DVCChannel) Close() error {
	if !ch.Open {
		return nil
	}

	ch.Open = false

	// Send close request
	cbID, idLen := channelIDEncoding(ch.ID)

	buf := make([]byte, 1+idLen)
	buf[0] = DVCCloseRequest | cbID

	switch idLen {
	case 1:
		buf[1] = byte(ch.ID)
	case 2:
		binary.LittleEndian.PutUint16(buf[1:3], uint16(ch.ID))
	case 4:
		binary.LittleEndian.PutUint32(buf[1:5], ch.ID)
	}

	return ch.manager.sendFunc(buf)
}
