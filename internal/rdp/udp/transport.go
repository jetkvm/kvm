package udp

import (
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"
)

// Transport state constants.
const (
	stateHandshake = iota
	stateReady
	stateClosed
)

// ACK coalescing delay per MS-RDPEUDP2 spec.
const ackCoalesceDelay = 200 * time.Millisecond

// Maximum receive window size advertised to peer.
const maxRcvWindowSize = 64

// sentPacket tracks an unacknowledged outgoing packet for retransmission.
type sentPacket struct {
	seqNum   uint64
	data     []byte // Raw payload (for retransmit)
	sendTime time.Time
	retries  int
}

// Transport implements net.Conn over RDPEUDP2.
//
// A single *net.UDPConn is shared across all transports on the server.
// Each Transport represents one client, identified by its remote UDP address.
// The server's read loop dispatches incoming packets to the correct Transport
// via ProcessIncomingPacket.
type Transport struct {
	udpConn    *net.UDPConn // Shared server UDP socket
	remoteAddr *net.UDPAddr // Client's UDP address

	// Sequence numbers (64-bit internal, 16-bit on wire)
	sendSeqNum atomic.Uint64
	recvSeqNum atomic.Uint64 // Highest contiguous received

	// Send window: unacknowledged packets for retransmission
	sendBuf   map[uint64]*sentPacket
	sendBufMu sync.Mutex
	sendCond  *sync.Cond // Signaled when cwnd opens up

	// Receive window: out-of-order received packets
	recvBuf   map[uint64][]byte
	recvBufMu sync.Mutex

	// Assembled in-order data for Read()
	readChan chan []byte
	readBuf  []byte // Partial read leftover

	// Transport state
	state    atomic.Int32
	stopChan chan struct{}
	stopOnce sync.Once

	// Congestion control
	cc congestion

	// ACK coalescing
	ackPending atomic.Bool

	// Read deadline support
	readDeadline  atomic.Pointer[time.Time]
	writeDeadline atomic.Pointer[time.Time]

	logger *zerolog.Logger
}

// NewTransport creates a new RDPEUDP2 transport for a specific client.
func NewTransport(udpConn *net.UDPConn, remoteAddr *net.UDPAddr, logger *zerolog.Logger) *Transport {
	t := &Transport{
		udpConn:    udpConn,
		remoteAddr: remoteAddr,
		sendBuf:    make(map[uint64]*sentPacket),
		recvBuf:    make(map[uint64][]byte),
		readChan:   make(chan []byte, 256),
		stopChan:   make(chan struct{}),
		logger:     logger,
	}
	t.state.Store(stateHandshake)
	t.cc.init()
	t.sendCond = sync.NewCond(&t.sendBufMu)

	// Initialize sequence numbers to 0; handshake sets them properly
	t.sendSeqNum.Store(0)
	t.recvSeqNum.Store(0)

	return t
}

// SetReady transitions the transport to the ready state and starts background goroutines.
func (t *Transport) SetReady() {
	t.state.Store(stateReady)
	go t.retransmitLoop()
	go t.ackLoop()
}

// Read implements net.Conn.Read.
// Delivers reassembled, in-order data from the receive pipeline.
func (t *Transport) Read(b []byte) (int, error) {
	// Return leftover data from previous Read
	if len(t.readBuf) > 0 {
		n := copy(b, t.readBuf)
		t.readBuf = t.readBuf[n:]
		return n, nil
	}

	// Check deadline
	var timer *time.Timer
	var timerCh <-chan time.Time

	if dl := t.readDeadline.Load(); dl != nil && !dl.IsZero() {
		remaining := time.Until(*dl)
		if remaining <= 0 {
			return 0, &timeoutError{op: "read"}
		}
		timer = time.NewTimer(remaining)
		timerCh = timer.C
		defer timer.Stop()
	}

	select {
	case data, ok := <-t.readChan:
		if !ok {
			return 0, net.ErrClosed
		}
		n := copy(b, data)
		if n < len(data) {
			t.readBuf = data[n:]
		}
		return n, nil
	case <-timerCh:
		return 0, &timeoutError{op: "read"}
	case <-t.stopChan:
		return 0, net.ErrClosed
	}
}

// Write implements net.Conn.Write.
// Fragments data into MTU-sized packets, assigns sequence numbers,
// and sends via RDPEUDP2 data format.
func (t *Transport) Write(b []byte) (int, error) {
	if t.state.Load() == stateClosed {
		return 0, net.ErrClosed
	}

	totalWritten := 0
	remaining := b

	for len(remaining) > 0 {
		// Fragment to MTU size
		chunkSize := min(len(remaining), MaxDataPayload)
		chunk := remaining[:chunkSize]
		remaining = remaining[chunkSize:]

		// Wait for congestion window to allow sending
		if err := t.waitForCwnd(); err != nil {
			if totalWritten > 0 {
				return totalWritten, nil
			}
			return 0, err
		}

		seqNum := t.sendSeqNum.Add(1) - 1

		// Build RDPEUDP2 v3 data packet
		pkt := &DataPacket{
			Data: &DataPayload{
				SeqNum: seqNum,
				Data:   chunk,
			},
		}

		// Piggyback ACK if pending
		if t.ackPending.CompareAndSwap(true, false) {
			pkt.Ack = &AckPayload{
				SeqNum:        t.recvSeqNum.Load(),
				RcvWindowSize: maxRcvWindowSize,
			}
		}

		raw := BuildDataPacket(pkt)

		// Apply RDPEUDP2 obfuscation and pad to minimum 8 bytes
		raw = t.padAndObfuscate(raw)

		// Buffer for retransmission
		t.sendBufMu.Lock()
		t.sendBuf[seqNum] = &sentPacket{
			seqNum:   seqNum,
			data:     chunk, // Store original chunk for retransmit
			sendTime: time.Now(),
		}
		t.sendBufMu.Unlock()

		// Send via shared UDP socket
		if _, err := t.udpConn.WriteToUDP(raw, t.remoteAddr); err != nil {
			return totalWritten, err
		}

		totalWritten += chunkSize
	}

	return totalWritten, nil
}

// Close implements net.Conn.Close.
func (t *Transport) Close() error {
	var closed bool
	t.stopOnce.Do(func() {
		t.state.Store(stateClosed)
		close(t.stopChan)
		closed = true

		// Wake up any waiters
		t.sendCond.Broadcast()

		// Close readChan to unblock Read()
		close(t.readChan)
	})

	if !closed {
		return net.ErrClosed
	}
	return nil
}

// LocalAddr implements net.Conn.
func (t *Transport) LocalAddr() net.Addr {
	return t.udpConn.LocalAddr()
}

// RemoteAddr implements net.Conn.
func (t *Transport) RemoteAddr() net.Addr {
	return t.remoteAddr
}

// SetDeadline implements net.Conn.
func (t *Transport) SetDeadline(dl time.Time) error {
	t.readDeadline.Store(&dl)
	t.writeDeadline.Store(&dl)
	return nil
}

// SetReadDeadline implements net.Conn.
func (t *Transport) SetReadDeadline(dl time.Time) error {
	t.readDeadline.Store(&dl)
	return nil
}

// SetWriteDeadline implements net.Conn.
func (t *Transport) SetWriteDeadline(dl time.Time) error {
	t.writeDeadline.Store(&dl)
	return nil
}

// waitForCwnd blocks until the congestion window allows sending.
func (t *Transport) waitForCwnd() error {
	t.sendBufMu.Lock()
	defer t.sendBufMu.Unlock()

	for len(t.sendBuf) >= t.cc.getCwnd() {
		if t.state.Load() == stateClosed {
			return net.ErrClosed
		}
		// Wait with a timeout to avoid infinite blocking
		done := make(chan struct{})
		go func() {
			t.sendCond.Wait()
			close(done)
		}()

		t.sendBufMu.Unlock()
		select {
		case <-done:
		case <-t.stopChan:
			t.sendBufMu.Lock()
			return net.ErrClosed
		}
		t.sendBufMu.Lock()
	}
	return nil
}

// retransmitLoop periodically checks for unacknowledged packets and retransmits them.
func (t *Transport) retransmitLoop() {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-t.stopChan:
			return
		case <-ticker.C:
			t.retransmitExpired()
		}
	}
}

// retransmitExpired retransmits packets that have exceeded the RTO.
func (t *Transport) retransmitExpired() {
	rto := t.cc.getRTO()
	now := time.Now()

	t.sendBufMu.Lock()
	defer t.sendBufMu.Unlock()

	for _, sp := range t.sendBuf {
		if now.Sub(sp.sendTime) < rto {
			continue
		}

		// RTO expired — retransmit
		t.cc.onTimeout()
		sp.retries++
		sp.sendTime = now

		if sp.retries > 10 {
			// Too many retries — close connection
			t.logger.Warn().
				Uint64("seqNum", sp.seqNum).
				Int("retries", sp.retries).
				Msg("UDP: too many retransmits, closing transport")
			go t.Close()
			return
		}

		// Rebuild and send
		pkt := &DataPacket{
			Data: &DataPayload{
				SeqNum: sp.seqNum,
				Data:   sp.data,
			},
		}
		raw := BuildDataPacket(pkt)
		raw = t.padAndObfuscate(raw)

		if _, err := t.udpConn.WriteToUDP(raw, t.remoteAddr); err != nil {
			t.logger.Debug().Err(err).Msg("UDP: retransmit send error")
		}
	}
}

// ackLoop sends coalesced ACKs at regular intervals.
func (t *Transport) ackLoop() {
	ticker := time.NewTicker(ackCoalesceDelay)
	defer ticker.Stop()

	for {
		select {
		case <-t.stopChan:
			return
		case <-ticker.C:
			if t.ackPending.CompareAndSwap(true, false) {
				t.sendAck()
			}
		}
	}
}

// padAndObfuscate pads a packet to minimum 8 bytes and applies prefix obfuscation.
func (t *Transport) padAndObfuscate(data []byte) []byte {
	// Pad to minimum 8 bytes for obfuscation
	if len(data) < 8 {
		padded := make([]byte, 8)
		copy(padded, data)
		data = padded
	}
	ApplyPacketPrefix(data)
	return data
}

// timeoutError implements net.Error for deadline exceeded.
type timeoutError struct {
	op string
}

func (e *timeoutError) Error() string   { return "udp: " + e.op + " timeout" }
func (e *timeoutError) Timeout() bool   { return true }
func (e *timeoutError) Temporary() bool { return true }

// Verify Transport implements net.Conn at compile time.
var _ net.Conn = (*Transport)(nil)

// Verify timeoutError implements net.Error at compile time.
var _ net.Error = (*timeoutError)(nil)

// IsClosed returns true if the transport is in the closed state.
func (t *Transport) IsClosed() bool {
	return t.state.Load() == stateClosed
}
