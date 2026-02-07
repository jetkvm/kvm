package kvm

import (
	"bufio"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jetkvm/kvm/internal/rdp"
)

const (
	captureDir         = "/tmp/rdp-captures"
	captureMaxSizeMB   = 16
	captureMaxSessions = 10

	// pcap constants
	pcapMagic   = 0xa1b2c3d4
	pcapSnaplen = 65535
	dltRaw      = 101

	// TCP flags
	tcpFlagFIN = 0x01
	tcpFlagSYN = 0x02
	tcpFlagACK = 0x10

	// Fixed header sizes
	pcapHdrLen = 16
	ipHdrLen   = 20
	tcpHdrLen  = 20
	allHdrLen  = pcapHdrLen + ipHdrLen + tcpHdrLen // 56 bytes
)

// CaptureSessionInfo is the metadata returned to the WebUI.
type CaptureSessionInfo struct {
	ID         string `json:"id"`
	ClientName string `json:"clientName"`
	RemoteAddr string `json:"remoteAddr"`
	StartTime  string `json:"startTime"`
	Size       int64  `json:"size"`
	Active     bool   `json:"active"`
}

// CaptureState is returned by getRDPCaptureState.
type CaptureState struct {
	Enabled  bool                 `json:"enabled"`
	Sessions []CaptureSessionInfo `json:"sessions"`
}

// CaptureManager manages packet capture state. Singleton.
type CaptureManager struct {
	mu       sync.Mutex
	enabled  atomic.Bool
	sessions []*captureSession
}

var (
	captureManager     *CaptureManager
	captureManagerOnce sync.Once
)

// GetRDPCaptureManager returns the global CaptureManager.
func GetRDPCaptureManager() *CaptureManager {
	captureManagerOnce.Do(func() {
		captureManager = &CaptureManager{}
		_ = os.MkdirAll(captureDir, 0755)
	})
	return captureManager
}

func (m *CaptureManager) SetEnabled(enabled bool) {
	m.enabled.Store(enabled)
}

func (m *CaptureManager) IsEnabled() bool {
	return m.enabled.Load()
}

// NewCapture creates a new capture session if enabled. Returns nil otherwise.
func (m *CaptureManager) NewCapture(remoteAddr string) rdp.PacketCapture {
	if !m.enabled.Load() {
		return nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.enabled.Load() {
		return nil
	}

	// Enforce session limit (FIFO)
	for len(m.sessions) >= captureMaxSessions {
		oldest := m.sessions[0]
		m.sessions = m.sessions[1:]
		oldest.remove()
	}

	sess, err := newCaptureSession(remoteAddr)
	if err != nil {
		rdpLogger.Warn().Err(err).Msg("RDP capture: failed to create session")
		return nil
	}

	m.sessions = append(m.sessions, sess)
	return &rdpPacketCapture{session: sess}
}

func (m *CaptureManager) GetState() CaptureState {
	m.mu.Lock()
	defer m.mu.Unlock()

	sessions := make([]CaptureSessionInfo, 0, len(m.sessions))
	for _, s := range m.sessions {
		sessions = append(sessions, s.info())
	}
	return CaptureState{
		Enabled:  m.enabled.Load(),
		Sessions: sessions,
	}
}

func (m *CaptureManager) DeleteSession(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i, s := range m.sessions {
		if s.id == id {
			s.remove()
			m.sessions = append(m.sessions[:i], m.sessions[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("session not found: %s", id)
}

func (m *CaptureManager) DeleteAll() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, s := range m.sessions {
		s.remove()
	}
	m.sessions = nil
}

// flushAndGetPath flushes any buffered data and returns the pcap file path.
func (m *CaptureManager) flushAndGetPath(id string) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, s := range m.sessions {
		if s.id == id {
			s.flush()
			return s.filePath, true
		}
	}
	return "", false
}

// captureSession holds per-connection capture state.
type captureSession struct {
	mu         sync.Mutex
	id         string
	file       *os.File
	writer     *bufio.Writer
	filePath   string
	clientSeq  uint32
	serverSeq  uint32
	clientAddr [4]byte
	serverAddr [4]byte
	clientPort uint16
	serverPort uint16
	startTime  time.Time
	byteCount  int64
	maxBytes   int64
	stopped    bool
	clientName string
	remoteAddr string
	// Pre-allocated header template: pcap (16) + IPv4 (20) + TCP (20).
	// Static fields pre-filled once; writePacket patches variable fields.
	hdrBuf [allHdrLen]byte
}

func newCaptureSession(remoteAddr string) (*captureSession, error) {
	id := randomID(8)
	filePath := filepath.Join(captureDir, "rdp-"+id+".pcap")
	f, err := os.Create(filePath)
	if err != nil {
		return nil, err
	}

	s := &captureSession{
		id:         id,
		file:       f,
		writer:     bufio.NewWriterSize(f, 64*1024),
		filePath:   filePath,
		startTime:  time.Now(),
		maxBytes:   int64(captureMaxSizeMB) * 1024 * 1024,
		remoteAddr: remoteAddr,
	}
	s.initHeaderTemplate()

	if err := writePcapGlobalHeader(s.writer); err != nil {
		f.Close()
		os.Remove(filePath)
		return nil, err
	}

	return s, nil
}

// initHeaderTemplate pre-fills the fixed fields of the pcap+IPv4+TCP header.
func (s *captureSession) initHeaderTemplate() {
	h := &s.hdrBuf
	h[pcapHdrLen] = 0x45                                          // IPv4, IHL=5
	h[pcapHdrLen+8] = 64                                          // TTL
	h[pcapHdrLen+9] = 6                                           // Protocol: TCP
	h[pcapHdrLen+ipHdrLen+12] = 0x50                              // TCP data offset=5
	binary.BigEndian.PutUint16(h[pcapHdrLen+ipHdrLen+14:], 65535) // TCP window
}

func (s *captureSession) setAddresses(local, remote net.Addr) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.clientAddr = addrToIPv4(remote)
	s.serverAddr = addrToIPv4(local)
	s.clientPort = addrPort(remote)
	s.serverPort = addrPort(local)

	s.writeTCPHandshake()
}

// record writes a pcap packet. isClient: true = client→server, false = server→client.
func (s *captureSession) record(isClient bool, data []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.stopped || s.byteCount >= s.maxBytes {
		return
	}

	var srcAddr, dstAddr *[4]byte
	var srcPort, dstPort uint16
	var seq, ack uint32

	if isClient {
		srcAddr, dstAddr = &s.clientAddr, &s.serverAddr
		srcPort, dstPort = s.clientPort, s.serverPort
		seq, ack = s.clientSeq, s.serverSeq
	} else {
		srcAddr, dstAddr = &s.serverAddr, &s.clientAddr
		srcPort, dstPort = s.serverPort, s.clientPort
		seq, ack = s.serverSeq, s.clientSeq
	}

	s.writePacket(time.Now(), srcAddr, dstAddr, srcPort, dstPort, seq, ack, tcpFlagACK, data)

	payloadLen := uint32(len(data))
	if isClient {
		s.clientSeq += payloadLen
	} else {
		s.serverSeq += payloadLen
	}
	s.byteCount += int64(payloadLen) + allHdrLen
}

// writePacket patches the pre-allocated header buffer and writes it + payload.
// Must be called with s.mu held.
func (s *captureSession) writePacket(ts time.Time, srcAddr, dstAddr *[4]byte,
	srcPort, dstPort uint16, seq, ack uint32, flags uint8, payload []byte) {

	ipTotalLen := uint16(ipHdrLen + tcpHdrLen + len(payload))

	h := &s.hdrBuf

	// Pcap packet header (bytes 0–15)
	binary.LittleEndian.PutUint32(h[0:], uint32(ts.Unix()))
	binary.LittleEndian.PutUint32(h[4:], uint32(ts.Nanosecond()/1000))
	binary.LittleEndian.PutUint32(h[8:], uint32(ipTotalLen))
	binary.LittleEndian.PutUint32(h[12:], uint32(ipTotalLen))

	// IPv4 variable fields (bytes 16–35)
	binary.BigEndian.PutUint16(h[pcapHdrLen+2:], ipTotalLen)
	copy(h[pcapHdrLen+12:], srcAddr[:])
	copy(h[pcapHdrLen+16:], dstAddr[:])

	// TCP variable fields (bytes 36–55)
	tcpOff := pcapHdrLen + ipHdrLen
	binary.BigEndian.PutUint16(h[tcpOff:], srcPort)
	binary.BigEndian.PutUint16(h[tcpOff+2:], dstPort)
	binary.BigEndian.PutUint32(h[tcpOff+4:], seq)
	binary.BigEndian.PutUint32(h[tcpOff+8:], ack)
	h[tcpOff+13] = flags

	_, _ = s.writer.Write(h[:])
	if len(payload) > 0 {
		_, _ = s.writer.Write(payload)
	}
}

func (s *captureSession) setClientName(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clientName = name
}

func (s *captureSession) close() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.stopped {
		return
	}
	s.stopped = true

	ts := time.Now()
	s.writePacket(ts, &s.clientAddr, &s.serverAddr, s.clientPort, s.serverPort,
		s.clientSeq, s.serverSeq, tcpFlagFIN|tcpFlagACK, nil)
	s.writePacket(ts, &s.serverAddr, &s.clientAddr, s.serverPort, s.clientPort,
		s.serverSeq, s.clientSeq+1, tcpFlagFIN|tcpFlagACK, nil)

	s.writer.Flush()
	s.file.Close()
}

func (s *captureSession) remove() {
	s.close()
	os.Remove(s.filePath)
}

// flush writes buffered data to disk (for download of active sessions).
func (s *captureSession) flush() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.stopped {
		s.writer.Flush()
	}
}

func (s *captureSession) info() CaptureSessionInfo {
	s.mu.Lock()
	defer s.mu.Unlock()

	return CaptureSessionInfo{
		ID:         s.id,
		ClientName: s.clientName,
		RemoteAddr: s.remoteAddr,
		StartTime:  s.startTime.Format(time.RFC3339),
		Size:       s.byteCount,
		Active:     !s.stopped,
	}
}

// writeTCPHandshake writes a synthetic SYN/SYN-ACK/ACK. Must hold s.mu.
func (s *captureSession) writeTCPHandshake() {
	ts := time.Now()
	s.writePacket(ts, &s.clientAddr, &s.serverAddr, s.clientPort, s.serverPort,
		s.clientSeq, 0, tcpFlagSYN, nil)
	s.clientSeq++
	s.writePacket(ts, &s.serverAddr, &s.clientAddr, s.serverPort, s.clientPort,
		s.serverSeq, s.clientSeq, tcpFlagSYN|tcpFlagACK, nil)
	s.serverSeq++
	s.writePacket(ts, &s.clientAddr, &s.serverAddr, s.clientPort, s.serverPort,
		s.clientSeq, s.serverSeq, tcpFlagACK, nil)
}

// addrToIPv4 extracts a 4-byte IPv4 address from a net.Addr.
func addrToIPv4(addr net.Addr) [4]byte {
	if tcpAddr, ok := addr.(*net.TCPAddr); ok {
		if ip4 := tcpAddr.IP.To4(); ip4 != nil {
			return [4]byte{ip4[0], ip4[1], ip4[2], ip4[3]}
		}
	}
	return [4]byte{127, 0, 0, 1}
}

// addrPort extracts the port number from a net.Addr.
func addrPort(addr net.Addr) uint16 {
	if tcpAddr, ok := addr.(*net.TCPAddr); ok {
		return uint16(tcpAddr.Port)
	}
	return 0
}

// captureConn wraps a net.Conn to intercept Read/Write for pcap capture.
type captureConn struct {
	mu      sync.Mutex
	inner   net.Conn
	session *captureSession
}

func newCaptureConn(inner net.Conn, session *captureSession) *captureConn {
	cc := &captureConn{inner: inner, session: session}
	session.setAddresses(inner.LocalAddr(), inner.RemoteAddr())
	return cc
}

func (c *captureConn) Read(b []byte) (int, error) {
	n, err := c.inner.Read(b)
	if n > 0 {
		c.session.record(true, b[:n])
	}
	return n, err
}

func (c *captureConn) Write(b []byte) (int, error) {
	n, err := c.inner.Write(b)
	if n > 0 {
		c.session.record(false, b[:n])
	}
	return n, err
}

func (c *captureConn) Close() error                       { return c.inner.Close() }
func (c *captureConn) LocalAddr() net.Addr                { return c.inner.LocalAddr() }
func (c *captureConn) RemoteAddr() net.Addr               { return c.inner.RemoteAddr() }
func (c *captureConn) SetDeadline(t time.Time) error      { return c.inner.SetDeadline(t) }
func (c *captureConn) SetReadDeadline(t time.Time) error  { return c.inner.SetReadDeadline(t) }
func (c *captureConn) SetWriteDeadline(t time.Time) error { return c.inner.SetWriteDeadline(t) }

func (c *captureConn) Inner() net.Conn {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.inner
}

func (c *captureConn) SwapInner(newConn net.Conn) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.inner = newConn
}

// rdpPacketCapture implements rdp.PacketCapture.
type rdpPacketCapture struct {
	session *captureSession
	cc      *captureConn
}

func (p *rdpPacketCapture) WrapConn(conn net.Conn) net.Conn {
	p.cc = newCaptureConn(conn, p.session)
	return p.cc
}

func (p *rdpPacketCapture) SwapInner(newConn net.Conn) net.Conn {
	if p.cc != nil {
		p.cc.SwapInner(newConn)
		return p.cc
	}
	return newConn
}

func (p *rdpPacketCapture) Inner() net.Conn {
	if p.cc != nil {
		return p.cc.Inner()
	}
	return nil
}

func (p *rdpPacketCapture) SetClientName(name string) {
	p.session.setClientName(name)
}

func (p *rdpPacketCapture) Close() {
	p.session.close()
}

// --- pcap format ---

func writePcapGlobalHeader(w io.Writer) error {
	var buf [24]byte
	binary.LittleEndian.PutUint32(buf[0:], pcapMagic)
	binary.LittleEndian.PutUint16(buf[4:], 2) // major
	binary.LittleEndian.PutUint16(buf[6:], 4) // minor
	binary.LittleEndian.PutUint32(buf[16:], pcapSnaplen)
	binary.LittleEndian.PutUint32(buf[20:], dltRaw)
	_, err := w.Write(buf[:])
	return err
}

func randomID(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// --- RPC handlers ---

func rpcSetRDPCaptureEnabled(enabled bool) error {
	GetRDPCaptureManager().SetEnabled(enabled)
	return nil
}

func rpcGetRDPCaptureState() (CaptureState, error) {
	state := GetRDPCaptureManager().GetState()
	sort.Slice(state.Sessions, func(i, j int) bool {
		return state.Sessions[i].StartTime > state.Sessions[j].StartTime
	})
	return state, nil
}

func rpcDeleteRDPCapture(sessionId string) error {
	return GetRDPCaptureManager().DeleteSession(sessionId)
}

func rpcDeleteAllRDPCaptures() error {
	GetRDPCaptureManager().DeleteAll()
	return nil
}

// --- HTTP handler ---

func handleRDPCaptureDownload(c *gin.Context) {
	sessionId := c.Param("sessionId")
	path, ok := GetRDPCaptureManager().flushAndGetPath(sessionId)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "capture session not found"})
		return
	}

	c.Header("Content-Type", "application/vnd.tcpdump.pcap")
	c.Header("Content-Disposition", "attachment; filename=rdp-capture-"+sessionId+".pcap")
	c.File(path)
}
