package ffs

import (
	"encoding/binary"
	"testing"
)

// envelope makes the first bytes of a message whose payload is size bytes.
func envelope(size uint32, packet int) []byte {
	b := make([]byte, packet)
	binary.LittleEndian.PutUint32(b[4:], size)
	return b
}

// A message read in packets: the first read is one packet with the
// envelope, the reads after it never ask past the end of the message, and
// the message's last read puts the framer back at a boundary.
func TestFramerNeverReadsPastAMessage(t *testing.T) {
	var f framer
	f.init(512, 64*1024)
	if got := f.nextLength(); got != 512 {
		t.Fatalf("first read %d, want one packet", got)
	}
	// 8 + 200000 bytes: 512 now, 199496 left.
	f.completed(envelope(200000, 512), 512)
	left := 200000 + 8 - 512
	for left > 0 {
		n := f.nextLength()
		if n%512 != 0 || n > 64*1024 {
			t.Fatalf("read of %d bytes is not whole packets within the buffer", n)
		}
		if n > left+511 {
			t.Fatalf("read of %d bytes asks past the %d bytes left of the message", n, left)
		}
		got := min(n, left)
		f.completed(make([]byte, got), n)
		left -= got
	}
	if !f.atBoundary {
		t.Fatal("not at a boundary after the whole message")
	}
	if got := f.nextLength(); got != 512 {
		t.Fatalf("next message starts with a %d-byte read, want one packet", got)
	}
}

// A short read ends the sender's write, so the next byte starts a message,
// whatever the envelope promised.
func TestFramerShortReadEndsTheMessage(t *testing.T) {
	var f framer
	f.init(512, 64*1024)
	f.completed(envelope(100000, 512), 512)
	if f.atBoundary {
		t.Fatal("at a boundary inside a long message")
	}
	f.completed(make([]byte, 100), f.nextLength())
	if !f.atBoundary || f.nextLength() != 512 {
		t.Fatalf("after a short read: boundary %t, next %d", f.atBoundary, f.nextLength())
	}
}

// A message that fits its first packet, and a zero-length packet, leave the
// framer at a boundary.
func TestFramerSmallMessagesAndZeroLengthPackets(t *testing.T) {
	var f framer
	f.init(64, 4096)
	f.completed(envelope(20, 28)[:28], 64)
	if !f.atBoundary {
		t.Fatal("a 28-byte message did not end at the short read")
	}
	f.completed(nil, 64)
	if !f.atBoundary || f.nextLength() != 64 {
		t.Fatal("a zero-length packet left the framer inside a message")
	}
	// Exactly one full packet that is the whole message.
	f.completed(envelope(56, 64), 64)
	if !f.atBoundary {
		t.Fatal("a message of exactly one packet did not end")
	}
}

// After a RESET the framer starts at a boundary, also inside a message.
func TestFramerRestart(t *testing.T) {
	var f framer
	f.init(512, 64*1024)
	f.completed(envelope(1<<20, 512), 512)
	f.restart()
	if !f.atBoundary || f.nextLength() != 512 {
		t.Fatal("restart did not return to a boundary")
	}
}
