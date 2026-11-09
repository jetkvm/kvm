package ffs

import (
	"encoding/binary"
	"errors"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/jetkvm/kvm/internal/osservices"
	"github.com/rs/zerolog"
)

const (
	// inQueueCapacity is the IN queue: whole messages wait here for the
	// writer. A write that finds no room waits up to inWriteWait, as the
	// KVM allows 2 seconds for each write (Byte stream).
	inQueueCapacity = 256 * 1024
	inWriteWait     = 2 * time.Second
	// readCapacity is the largest OUT read, a whole number of packets.
	readCapacity = 64 * 1024
	// kvmRestartWindow: a reconfiguration this soon after the KVM restarted
	// its own USB device does not count against the video retry budget.
	kvmRestartWindow = 10 * time.Second
)

// Link connects a Function to a Session. Mu guards the session: every
// session call and every platform hook runs with it held.
type Link struct {
	Mu      sync.Mutex
	session *osservices.Session
	log     *zerolog.Logger
	udc     string

	fn *Function

	// The OUT framer, and the wake-up for a held read.
	framer framer
	wake   chan struct{}

	// The IN queue.
	qmu        sync.Mutex
	qcond      *sync.Cond
	queue      []byte
	generation uint64
	packetSize int

	restartMu    sync.Mutex
	kvmRestartAt time.Time
}

// NewLink makes a link for a session; Start runs it on a function.
func NewLink(log *zerolog.Logger, udc string) *Link {
	l := &Link{log: log, udc: udc, wake: make(chan struct{}, 1), packetSize: PacketSizeHS}
	l.qcond = sync.NewCond(&l.qmu)
	l.framer.init(PacketSizeHS, readCapacity)
	return l
}

// SetSession sets the session the link serves, before Start.
func (l *Link) SetSession(s *osservices.Session) { l.session = s }

// Start runs the link's goroutines on fn until fn fails.
func (l *Link) Start(fn *Function) {
	l.fn = fn
	go l.control()
	go l.reader()
	go l.writer()
}

// NoteKVMRestart: the KVM is about to restart its own USB device, for a
// mount or a USB setting.
func (l *Link) NoteKVMRestart() {
	l.restartMu.Lock()
	l.kvmRestartAt = time.Now()
	l.restartMu.Unlock()
}

func (l *Link) byKVM() bool {
	l.restartMu.Lock()
	defer l.restartMu.Unlock()
	return !l.kvmRestartAt.IsZero() && time.Since(l.kvmRestartAt) < kvmRestartWindow
}

// Wake retries a read that waits for an access-unit slot or for the
// browser.
func (l *Link) Wake() {
	select {
	case l.wake <- struct{}{}:
	default:
	}
}

// --- Platform hooks for the transport (called with Mu held) ---------------

// Write queues one message for the IN endpoint, all of it or none.
func (l *Link) Write(envelope, head, body []byte) bool {
	need := len(envelope) + len(head) + len(body)
	l.qmu.Lock()
	defer l.qmu.Unlock()
	deadline := time.Now().Add(inWriteWait)
	for len(l.queue)+need > inQueueCapacity {
		left := time.Until(deadline)
		if left <= 0 {
			return false
		}
		timer := time.AfterFunc(left, func() {
			l.qmu.Lock()
			l.qcond.Broadcast()
			l.qmu.Unlock()
		})
		l.qcond.Wait()
		timer.Stop()
	}
	l.queue = append(l.queue, envelope...)
	l.queue = append(l.queue, head...)
	l.queue = append(l.queue, body...)
	l.qcond.Broadcast()
	return true
}

// flush drops what waits for the IN endpoint.
func (l *Link) flush() {
	l.qmu.Lock()
	l.queue = l.queue[:0]
	l.generation++
	l.qcond.Broadcast()
	l.qmu.Unlock()
}

// SessionEnded drops what is queued; the reader stops feeding the session
// until it is admitted again.
func (l *Link) SessionEnded() { l.flush() }

// ResetTransport: the previous session's bulk requests are dropped. The
// caller admits the session once the reset is answered (control()).
func (l *Link) ResetTransport() {
	l.flush()
}

// --- Control requests and events --------------------------------------------

func (l *Link) control() {
	for {
		events, err := l.fn.ReadEvents()
		if err != nil {
			l.log.Warn().Err(err).Msg("OS Services ep0 failed; the USB function is faulted")
			l.Mu.Lock()
			l.session.SetFaulted(true)
			l.Mu.Unlock()
			return
		}
		for _, e := range events {
			l.event(e)
		}
	}
}

func (l *Link) event(e Event) {
	switch e.Type {
	case EventSetup:
		l.setup(e.Setup)
	case EventEnable:
		l.packetSize = l.currentPacketSize()
		l.reconfigured("enable")
	case EventDisable:
		l.reconfigured("disable")
	case EventUnbind:
		l.reconfigured("unbind")
	case EventBind:
		l.log.Debug().Msg("OS Services function bound")
	}
}

func (l *Link) reconfigured(what string) {
	byKVM := l.byKVM()
	l.log.Info().Str("event", what).Bool("by_kvm", byKVM).Msg("OS Services USB function reconfigured")
	l.Mu.Lock()
	l.session.USBReconfigured(byKVM)
	l.Mu.Unlock()
	l.flush()
}

func (l *Link) currentPacketSize() int {
	speed, err := os.ReadFile("/sys/class/udc/" + l.udc + "/current_speed")
	if err == nil && strings.TrimSpace(string(speed)) == "full-speed" {
		return PacketSizeFS
	}
	return PacketSizeHS
}

func (l *Link) setup(s Setup) {
	valid := s.RequestType == 0xc1 &&
		(s.Request == osservices.USBRequestReset || s.Request == osservices.USBRequestStatus) &&
		s.Value == osservices.ProtocolMajor && s.Index == 0 && s.Length >= osservices.USBStatusLen
	if !valid {
		if err := l.fn.Stall(s); err != nil {
			l.log.Debug().Err(err).Msg("OS Services: stall failed")
		}
		return
	}
	l.Mu.Lock()
	var status [osservices.USBStatusLen]byte
	if s.Request == osservices.USBRequestReset {
		status = l.session.USBReset()
		// The reader takes the next read as the start of the new session
		// and drops what it held; the session is admitted at once, as the
		// sender discards a stale IN byte stream while it searches for
		// session.ready.
		l.framer.restart()
		l.session.TransportDrained()
	} else {
		status = l.session.USBStatus()
	}
	l.Mu.Unlock()
	l.Wake()
	if err := l.fn.Reply(status[:]); err != nil {
		l.log.Debug().Err(err).Msg("OS Services: control reply failed")
	}
}

// --- OUT endpoint -------------------------------------------------------------

func (l *Link) reading() bool {
	l.Mu.Lock()
	defer l.Mu.Unlock()
	return l.session.Reading()
}

func (l *Link) reader() {
	buf := make([]byte, readCapacity)
	for {
		// A session that ended stops reading until the next RESET: the
		// sender's writes then stall and it starts again.
		for !l.reading() {
			l.waitWake(50 * time.Millisecond)
		}
		l.Mu.Lock()
		length := l.framer.nextLength()
		l.Mu.Unlock()
		n, err := l.fn.Read(buf[:length])
		if err != nil {
			if errors.Is(err, os.ErrClosed) {
				return
			}
			// ESHUTDOWN and the like: the function was disabled; the
			// events report it.
			time.Sleep(20 * time.Millisecond)
			continue
		}
		l.Mu.Lock()
		l.framer.completed(buf[:n], length)
		l.Mu.Unlock()
		l.feed(buf[:n])
	}
}

// feed hands a read to the session. While a frame waits for a slot or
// output for the browser, the rest of the read stays here, and the bus with
// it.
func (l *Link) feed(data []byte) {
	for len(data) > 0 {
		l.Mu.Lock()
		if !l.session.Reading() {
			l.Mu.Unlock()
			return
		}
		used := l.session.Receive(data)
		holding := l.session.FrameWaiting() || l.session.Holding()
		reading := l.session.Reading()
		l.Mu.Unlock()
		data = data[used:]
		if len(data) == 0 || !reading {
			return
		}
		if holding || used == 0 {
			l.waitWake(20 * time.Millisecond)
		}
	}
}

func (l *Link) waitWake(timeout time.Duration) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-l.wake:
	case <-timer.C:
	}
}

// --- IN endpoint --------------------------------------------------------------

func (l *Link) writer() {
	chunk := make([]byte, writeChunk)
	zeroLengthDue := false
	for {
		l.qmu.Lock()
		for len(l.queue) == 0 && !zeroLengthDue {
			l.qcond.Wait()
		}
		n := copy(chunk, l.queue)
		generation := l.generation
		l.qmu.Unlock()

		written, err := l.fn.Write(chunk[:n], inWriteWait)
		if err != nil {
			if errors.Is(err, os.ErrClosed) {
				return
			}
			l.log.Info().Err(err).Int("bytes", n).Msg("OS Services: IN write failed")
			l.Mu.Lock()
			if l.session.Open() || l.session.Reading() {
				l.session.End("the IN endpoint did not drain")
			}
			l.Mu.Unlock()
			l.flush()
			zeroLengthDue = false
			time.Sleep(20 * time.Millisecond)
			continue
		}
		l.qmu.Lock()
		if l.generation == generation {
			l.queue = l.queue[:copy(l.queue, l.queue[written:])]
		}
		// A transfer of whole packets does not end the host's read; a
		// zero-length packet does, once nothing else follows.
		zeroLengthDue = n > 0 && n%l.packetSize == 0 && len(l.queue) == 0
		l.qcond.Broadcast()
		l.qmu.Unlock()
	}
}

// --- OUT framer ---------------------------------------------------------------

// framer sizes the OUT reads. The sender starts every message on a new
// transfer, and every transfer that ends before the end of a message is a
// whole number of packets. So a read that ends short ends a message, and the
// first packet of a message holds its envelope. A read never spans past the
// end of a message: the sender may then wait for an answer, and the read
// would never complete.
type framer struct {
	packetSize   int
	readCapacity int
	atBoundary   bool
	remaining    uint64
}

func (f *framer) init(packetSize, readCapacity int) {
	f.packetSize = packetSize
	f.readCapacity = readCapacity
	f.restart()
}

func (f *framer) restart() {
	f.atBoundary = true
	f.remaining = 0
}

func (f *framer) nextLength() int {
	if f.atBoundary {
		return f.packetSize
	}
	packets := (f.remaining + uint64(f.packetSize) - 1) / uint64(f.packetSize)
	return int(min(packets*uint64(f.packetSize), uint64(f.readCapacity)))
}

func (f *framer) completed(data []byte, requested int) {
	received := len(data)
	if f.atBoundary {
		// The first packet of a message holds its envelope. Fewer bytes
		// than an envelope is a zero-length packet, or not a message.
		if received >= osservices.EnvelopeLen {
			total := uint64(osservices.EnvelopeLen) + uint64(binary.LittleEndian.Uint32(data[4:]))
			if total > uint64(received) {
				f.remaining = total - uint64(received)
			} else {
				f.remaining = 0
			}
		} else {
			f.remaining = 0
		}
	} else if uint64(received) < f.remaining {
		f.remaining -= uint64(received)
	} else {
		f.remaining = 0
	}
	// A short read ends the sender's write, and with it a message.
	f.atBoundary = received < requested || f.remaining == 0
	if f.atBoundary {
		f.remaining = 0
	}
}
