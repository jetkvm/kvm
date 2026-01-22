// Package channels implements RDP dynamic virtual channels.
package channels

import (
	"encoding/binary"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

// DRDYNVC implements the Dynamic Virtual Channel transport.
// This is the transport layer for RDPGFX, RDPSND, AUDIN, etc.

// DRDYNVC PDU types.
const (
	DVCCapabilityRequest  = 0x50 // CYCAP_REQ (version 3)
	DVCCapabilityResponse = 0x50 // CYCAP_RSP
	DVCCreateRequest      = 0x10 // CREATE_REQ
	DVCCreateResponse     = 0x10 // CREATE_RSP
	DVCDataFirst          = 0x20 // DATA_FIRST
	DVCData               = 0x30 // DATA
	DVCCloseRequest       = 0x40 // CLOSE
	DVCDataCompressed     = 0x60 // DATA_FIRST_COMPRESSED / DATA_COMPRESSED
	DVCSoftSyncRequest    = 0x70 // SOFT_SYNC_REQUEST
	DVCSoftSyncResponse   = 0x70 // SOFT_SYNC_RESPONSE
)

// DRDYNVC capability versions.
const (
	DVCVersion1 = 0x0001
	DVCVersion2 = 0x0002
	DVCVersion3 = 0x0003
)

// Channel IDs for well-known channels.
const (
	ChannelIDRDPGFX = 1
	ChannelIDRDPSND = 2
	ChannelIDRDPCAM = 3
	ChannelIDAUDIN  = 4
)

// Common errors.
var (
	ErrDVCChannelNotFound = errors.New("dvc: channel not found")
	ErrDVCChannelClosed   = errors.New("dvc: channel closed")
	ErrDVCDataTooLarge    = errors.New("dvc: data exceeds maximum size")
)

// Maximum sizes.
const (
	DVCMaxDataSize    = 1600 // Max data per PDU (fits in single MCS PDU)
	DVCMaxChannelName = 256
)

// DVCLogger is a simple logging function for DVC events.
type DVCLogger func(msg string, channel string, channelID uint32, args ...interface{})

// Maximum number of DVC channels for lock-free lookup array.
// Typical RDP sessions use 4-6 channels (GFX, RDPSND, AUDIN, RDPCAM, CLIPRDR, etc.)
const DVCMaxChannels = 16

// DVCManager manages dynamic virtual channels.
type DVCManager struct {
	channels   map[uint32]*DVCChannel
	channelsMu sync.RWMutex

	// Lock-free channel lookup for hot path (indexed by channel ID)
	// Uses atomic.Pointer for safe concurrent access without locks
	channelPtrs [DVCMaxChannels]atomic.Pointer[DVCChannel]

	nextChannelID atomic.Uint32
	version       uint16

	// Send callback for outgoing PDUs
	sendFunc func(data []byte) error

	// Optional logger for debugging
	logger DVCLogger

	// Capability exchange state
	capabilityReceived chan struct{}
	capabilityOnce     sync.Once

	// Callback for when capability is received (called synchronously from message loop)
	onCapabilityReceived func()
}

// DVC fragment buffer sizes for zero-allocation hot path.
const (
	// Max DVC header size: cmd(1) + channelID(4) + length(4) = 9 bytes
	DVCMaxHeaderSize = 9
	// Fragment buffer size: header + max data
	DVCFragmentBufSize = DVCMaxHeaderSize + DVCMaxDataSize
)

// DVCChannel represents a single dynamic virtual channel.
type DVCChannel struct {
	ID      uint32
	Name    string
	Open    bool
	Handler DVCHandler
	manager *DVCManager

	// Pre-allocated fragment buffer for zero-allocation hot path
	// Used by SendDataZeroAlloc to avoid per-fragment allocations
	fragBuf [DVCFragmentBufSize]byte

	// Fragment reassembly state (for incoming fragmented messages)
	// Per MS-RDPEDYC: DATA_FIRST + DATA PDUs must be reassembled before delivery
	reassemblyBuf    []byte // Accumulated data from DATA_FIRST and DATA PDUs
	reassemblyTotal  uint32 // Expected total length from DATA_FIRST
	reassemblyOffset uint32 // Current accumulated length
}

// DVCHandler handles incoming data on a DVC.
type DVCHandler interface {
	OnData(data []byte) error
	OnClose()
}

// DVCOpenHandler is an optional interface that handlers can implement
// to be notified when the channel is successfully opened.
type DVCOpenHandler interface {
	OnChannelOpen()
}

// NewDVCManager creates a new DVC manager.
func NewDVCManager(sendFunc func(data []byte) error) *DVCManager {
	m := &DVCManager{
		channels:           make(map[uint32]*DVCChannel),
		version:            DVCVersion3,
		sendFunc:           sendFunc,
		capabilityReceived: make(chan struct{}),
	}
	m.nextChannelID.Store(1)
	return m
}

// SetLogger sets the debug logger for the DVC manager.
func (m *DVCManager) SetLogger(logger DVCLogger) {
	m.logger = logger
}

// HandlePDU processes an incoming DRDYNVC PDU.
func (m *DVCManager) HandlePDU(data []byte) error {
	if len(data) < 1 {
		return errors.New("dvc: empty PDU")
	}

	// Extract PDU type and channel ID size from first byte
	cmdByte := data[0]
	pduType := cmdByte & 0xF0
	cbID := cmdByte & 0x03 // Channel ID size indicator

	// Log all PDU types for debugging (at trace level)
	if m.logger != nil {
		pduNames := map[byte]string{
			DVCCapabilityResponse: "CAP_RSP",
			DVCCreateResponse:     "CREATE_RSP",
			DVCDataFirst:          "DATA_FIRST",
			DVCData:               "DATA",
			DVCCloseRequest:       "CLOSE",
			DVCDataCompressed:     "DATA_COMPRESSED",
			DVCSoftSyncRequest:    "SOFT_SYNC",
		}
		name, ok := pduNames[pduType]
		if !ok {
			name = "UNKNOWN"
		}
		m.logger("DVC: received PDU %s (cmd=0x%02X type=0x%02X cbID=%d) len=%d", "", uint32(0), name, cmdByte, pduType, cbID, len(data))
	}

	switch pduType {
	case DVCCapabilityResponse:
		return m.handleCapabilityResponse(data)
	case DVCCreateResponse:
		return m.handleCreateResponse(data, cbID)
	case DVCDataFirst, DVCData:
		return m.handleData(data, cbID, pduType == DVCDataFirst)
	case DVCCloseRequest:
		return m.handleClose(data, cbID)
	case DVCDataCompressed:
		// Compressed data - log and skip for now (we don't support compression)
		if m.logger != nil {
			m.logger("DVC: compressed data received (not supported)", "", 0)
		}
		return nil
	case DVCSoftSyncRequest:
		// Soft Sync request from client - acknowledge by doing nothing
		// Per MS-RDPEDYC 2.2.3.1, soft sync is used for reconnection scenarios
		if m.logger != nil {
			m.logger("DVC: soft sync request received", "", 0)
		}
		return nil
	default:
		// Log unknown PDU types but don't return error to avoid disrupting the connection
		if m.logger != nil {
			m.logger("DVC: unknown PDU type 0x%02X", "", uint32(pduType))
		}
		return nil
	}
}

// handleCapabilityResponse processes capability response.
func (m *DVCManager) handleCapabilityResponse(data []byte) error {
	if len(data) < 4 {
		return errors.New("dvc: capability response too short")
	}
	// Skip pad byte, read version
	m.version = binary.LittleEndian.Uint16(data[2:4])
	if m.logger != nil {
		m.logger("DVC: capability response received", "", 0, m.version)
	}
	// Signal that capability exchange is complete
	m.capabilityOnce.Do(func() {
		close(m.capabilityReceived)
	})

	// Call the capability received callback synchronously
	// This allows channel creation to happen in the same execution context as the message loop
	if m.onCapabilityReceived != nil {
		m.onCapabilityReceived()
	}

	return nil
}

// SetOnCapabilityReceived sets a callback that is called synchronously when capability is received.
// This should be used instead of WaitForCapability for synchronous channel creation.
func (m *DVCManager) SetOnCapabilityReceived(callback func()) {
	m.onCapabilityReceived = callback
}

// WaitForCapability waits for the capability response with a timeout.
// Returns true if capability was received, false if timeout or context canceled.
func (m *DVCManager) WaitForCapability(timeout time.Duration) bool {
	select {
	case <-m.capabilityReceived:
		return true
	case <-time.After(timeout):
		return false
	}
}

// GetNegotiatedVersion returns the DVC version negotiated with the client.
func (m *DVCManager) GetNegotiatedVersion() uint16 {
	return m.version
}

// handleCreateResponse processes channel creation response.
func (m *DVCManager) handleCreateResponse(data []byte, cbID byte) error {
	channelID, pos := m.readChannelID(data[1:], cbID)
	if pos < 0 {
		return errors.New("dvc: invalid channel ID")
	}

	if len(data) < 1+pos+4 {
		return errors.New("dvc: create response too short")
	}

	// Read creation status
	status := binary.LittleEndian.Uint32(data[1+pos : 1+pos+4])

	m.channelsMu.Lock()
	ch, ok := m.channels[channelID]
	if ok {
		ch.Open = status == 0
		// Log channel creation result
		if m.logger != nil {
			if status == 0 {
				m.logger("DVC: channel created successfully", ch.Name, channelID)
			} else {
				m.logger("DVC: channel creation failed", ch.Name, channelID, status)
			}
		}
		// Store in lock-free array for hot path access (if channel ID fits)
		if status == 0 && channelID < DVCMaxChannels {
			m.channelPtrs[channelID].Store(ch)
		}
	}
	m.channelsMu.Unlock()

	// Notify handler that channel is open (outside lock)
	if ok && status == 0 && ch.Handler != nil {
		if openHandler, hasOpen := ch.Handler.(DVCOpenHandler); hasOpen {
			openHandler.OnChannelOpen()
		}
	}

	return nil
}

// handleData processes incoming data with fragment reassembly.
// Per MS-RDPEDYC, large messages are fragmented:
// - DATA_FIRST PDU contains Length field (total size) + first chunk
// - Subsequent DATA PDUs contain continuation chunks (no length field)
// The receiver must reassemble all fragments before delivering to handler.
//
// HOT PATH: Uses lock-free channel lookup for low-latency data handling.
func (m *DVCManager) handleData(data []byte, cbID byte, isFirst bool) error {
	channelID, pos := m.readChannelID(data[1:], cbID)
	if pos < 0 {
		return errors.New("dvc: invalid channel ID")
	}

	payload := data[1+pos:]
	var totalLength uint32
	var lenFieldSize int

	// If first PDU, parse the length field (indicates total message size)
	// The Len field is in bits 2-3 of the cmd byte:
	//   00 = 1 byte, 01 = 2 bytes, 10 = 4 bytes
	if isFirst && len(payload) > 0 {
		cmdByte := data[0]
		lenBits := (cmdByte >> 2) & 0x03
		switch lenBits {
		case 0:
			lenFieldSize = 1
			if len(payload) >= 1 {
				totalLength = uint32(payload[0])
			}
		case 1:
			lenFieldSize = 2
			if len(payload) >= 2 {
				totalLength = uint32(binary.LittleEndian.Uint16(payload[0:2]))
			}
		default: // 2 or 3
			lenFieldSize = 4
			if len(payload) >= 4 {
				totalLength = binary.LittleEndian.Uint32(payload[0:4])
			}
		}
		if len(payload) >= lenFieldSize {
			payload = payload[lenFieldSize:]
		}
	}

	// Debug log for DATA_FIRST PDU parsing (helps debug camera frame reassembly)
	if m.logger != nil && isFirst {
		m.logger("DATA_FIRST totalLen=%d payloadLen=%d", "", channelID, totalLength, len(payload))
	}

	// LOCK-FREE HOT PATH: Try atomic pointer array first (no locks)
	var ch *DVCChannel
	if channelID < DVCMaxChannels {
		ch = m.channelPtrs[channelID].Load()
	}

	// Fallback to map lookup only if not in atomic array (rare)
	if ch == nil {
		m.channelsMu.RLock()
		ch = m.channels[channelID]
		m.channelsMu.RUnlock()
	}

	if ch == nil || !ch.Open || ch.Handler == nil {
		return nil
	}

	// Fragment reassembly per MS-RDPEDYC section 3.1.5.2.2
	//
	// IMPORTANT: Data passed to handlers is only valid during the callback.
	// The underlying buffer may be reused for the next packet after the handler returns.
	// Handlers that need data for async processing MUST make their own copy.
	// (e.g., AUDIN callback copies data before sending to async channel)
	if isFirst {
		// DATA_FIRST: Start new reassembly or deliver if complete
		if totalLength > 0 && uint32(len(payload)) < totalLength {
			// Fragmented message - start reassembly
			// Allocate buffer for total message size (reuse if possible)
			if cap(ch.reassemblyBuf) >= int(totalLength) {
				ch.reassemblyBuf = ch.reassemblyBuf[:totalLength]
			} else {
				ch.reassemblyBuf = make([]byte, totalLength)
			}
			copy(ch.reassemblyBuf, payload)
			ch.reassemblyTotal = totalLength
			ch.reassemblyOffset = uint32(len(payload))
			return nil // Wait for more DATA PDUs
		}
		// Single-fragment message (fits in one PDU) - deliver directly
		// Zero-allocation: pass slice of the read buffer (handler copies if needed)
		ch.reassemblyTotal = 0
		ch.reassemblyOffset = 0
		return ch.Handler.OnData(payload)
	}

	// DATA (continuation): Append to reassembly buffer
	if ch.reassemblyTotal > 0 {
		// Append payload to reassembly buffer
		remaining := ch.reassemblyTotal - ch.reassemblyOffset
		copyLen := uint32(len(payload))
		if copyLen > remaining {
			copyLen = remaining
		}
		copy(ch.reassemblyBuf[ch.reassemblyOffset:], payload[:copyLen])
		ch.reassemblyOffset += copyLen

		// Check if reassembly is complete
		if ch.reassemblyOffset >= ch.reassemblyTotal {
			// Deliver complete message from reassembly buffer
			// The reassemblyBuf is a stable buffer owned by this channel,
			// so handlers can use it safely (no copy needed here).
			// If handler needs data beyond callback, it copies.
			data := ch.reassemblyBuf[:ch.reassemblyTotal]
			ch.reassemblyTotal = 0
			ch.reassemblyOffset = 0
			return ch.Handler.OnData(data)
		}
		return nil // Still waiting for more data
	}

	// No reassembly in progress - treat as single-fragment DATA
	// (This can happen if DATA_FIRST was never received, e.g., protocol error)
	return ch.Handler.OnData(payload)
}

// handleClose processes channel close.
func (m *DVCManager) handleClose(data []byte, cbID byte) error {
	channelID, _ := m.readChannelID(data[1:], cbID)

	// Clear lock-free pointer first (if in range)
	if channelID < DVCMaxChannels {
		m.channelPtrs[channelID].Store(nil)
	}

	m.channelsMu.Lock()
	ch, ok := m.channels[channelID]
	if ok {
		ch.Open = false
		if ch.Handler != nil {
			ch.Handler.OnClose()
		}
	}
	m.channelsMu.Unlock()

	return nil
}

// readChannelID reads a variable-length channel ID.
func (m *DVCManager) readChannelID(data []byte, cbID byte) (uint32, int) {
	switch cbID {
	case 0:
		if len(data) < 1 {
			return 0, -1
		}
		return uint32(data[0]), 1
	case 1:
		if len(data) < 2 {
			return 0, -1
		}
		return uint32(binary.LittleEndian.Uint16(data[:2])), 2
	case 2:
		if len(data) < 4 {
			return 0, -1
		}
		return binary.LittleEndian.Uint32(data[:4]), 4
	default:
		return 0, -1
	}
}

// SendCapabilityRequest sends a capability request.
func (m *DVCManager) SendCapabilityRequest() error {
	// CYCAP_REQ format:
	// - cmd(1) + pad(1) + version(2) = 4 bytes (for version 1)
	// - cmd(1) + pad(1) + version(2) + PriorityCharge0-3(8) = 12 bytes (for version 2/3)
	// Per MS-RDPEDYC 2.2.2.1
	var buf []byte
	if m.version >= DVCVersion2 {
		// Version 2/3: include priority charges
		buf = make([]byte, 12)
		buf[0] = DVCCapabilityRequest | 0x03 // Sp=0, cbChId=3 (version 3)
		buf[1] = 0                           // Pad
		binary.LittleEndian.PutUint16(buf[2:4], m.version)
		// PriorityCharge0-3: default bandwidth allocation (equal distribution)
		binary.LittleEndian.PutUint16(buf[4:6], 0)   // PriorityCharge0
		binary.LittleEndian.PutUint16(buf[6:8], 0)   // PriorityCharge1
		binary.LittleEndian.PutUint16(buf[8:10], 0)  // PriorityCharge2
		binary.LittleEndian.PutUint16(buf[10:12], 0) // PriorityCharge3
	} else {
		// Version 1: no priority charges
		buf = make([]byte, 4)
		buf[0] = DVCCapabilityRequest
		buf[1] = 0 // Pad
		binary.LittleEndian.PutUint16(buf[2:4], m.version)
	}
	return m.sendFunc(buf)
}

// CreateChannel creates a new dynamic virtual channel.
func (m *DVCManager) CreateChannel(name string, handler DVCHandler) (*DVCChannel, error) {
	channelID := m.nextChannelID.Add(1) - 1

	ch := &DVCChannel{
		ID:      channelID,
		Name:    name,
		Open:    false,
		Handler: handler,
		manager: m,
	}

	m.channelsMu.Lock()
	m.channels[channelID] = ch
	m.channelsMu.Unlock()

	// Send create request
	return ch, m.sendCreateRequest(channelID, name)
}

// sendCreateRequest sends a channel creation request.
func (m *DVCManager) sendCreateRequest(channelID uint32, name string) error {
	if m.logger != nil {
		m.logger("DVC: sending create request", name, channelID)
	}

	// CREATE_REQ: cmd(1) + channelID(1-4) + name(null-terminated)
	nameBytes := []byte(name)
	if len(nameBytes) > DVCMaxChannelName {
		return ErrDVCDataTooLarge
	}

	// Use 1-byte channel ID if possible
	cbID := byte(0)
	idLen := 1
	if channelID > 0xFF {
		cbID = 1
		idLen = 2
	}
	if channelID > 0xFFFF {
		cbID = 2
		idLen = 4
	}

	buf := make([]byte, 1+idLen+len(nameBytes)+1)
	buf[0] = DVCCreateRequest | cbID

	switch idLen {
	case 1:
		buf[1] = byte(channelID)
	case 2:
		binary.LittleEndian.PutUint16(buf[1:3], uint16(channelID))
	case 4:
		binary.LittleEndian.PutUint32(buf[1:5], channelID)
	}

	copy(buf[1+idLen:], nameBytes)
	buf[len(buf)-1] = 0 // Null terminator

	return m.sendFunc(buf)
}

// SendData sends data on a channel using zero-allocation hot path.
// HOT PATH: This function is called for every DVC fragment.
// Uses pre-allocated fragBuf to avoid heap allocations.
func (ch *DVCChannel) SendData(data []byte) error {
	if !ch.Open {
		return ErrDVCChannelClosed
	}

	// Determine channel ID encoding (constant for the lifetime of the channel)
	cbID := byte(0)
	idLen := 1
	if ch.ID > 0xFF {
		cbID = 1
		idLen = 2
	}
	if ch.ID > 0xFFFF {
		cbID = 2
		idLen = 4
	}

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
	cbID := byte(0)
	idLen := 1
	if ch.ID > 0xFF {
		cbID = 1
		idLen = 2
	}
	if ch.ID > 0xFFFF {
		cbID = 2
		idLen = 4
	}

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

// Close closes the DVC manager and all its channels.
func (m *DVCManager) Close() {
	// Clear all lock-free pointers first
	for i := range m.channelPtrs {
		m.channelPtrs[i].Store(nil)
	}

	m.channelsMu.Lock()
	defer m.channelsMu.Unlock()

	for _, ch := range m.channels {
		if ch.Open {
			ch.Open = false
			if ch.Handler != nil {
				ch.Handler.OnClose()
			}
		}
	}

	// Clear the channels map
	m.channels = make(map[uint32]*DVCChannel)
}
