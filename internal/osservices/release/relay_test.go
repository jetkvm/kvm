package release

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"io"
	"testing"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
)

// A package of three and a bit chunks.
const testChunk = 65524

func testPackage() []byte {
	data := make([]byte, 3*testChunk+1000)
	for i := range data {
		data[i] = byte(i * 7)
	}
	return data
}

func testKey(t *testing.T) *openpgp.Entity {
	t.Helper()
	key, err := openpgp.NewEntity("Release", "", "release@example.com",
		&packet.Config{Algorithm: packet.PubKeyAlgoEdDSA})
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func detachSign(t *testing.T, key *openpgp.Entity, data []byte) []byte {
	t.Helper()
	var sig bytes.Buffer
	if err := openpgp.DetachSign(&sig, key, bytes.NewReader(data), nil); err != nil {
		t.Fatal(err)
	}
	return sig.Bytes()
}

// The firmware's check: openpgp's, against the release key only.
func pgpSignature(releaseKey *openpgp.Entity, sig []byte) Signature {
	return StreamSignature(func(signed io.Reader) error {
		_, err := openpgp.CheckDetachedSignature(openpgp.EntityList{releaseKey}, signed, bytes.NewReader(sig), nil)
		return err
	})
}

// sender records what reaches the sender.
type sender struct {
	got    []byte
	chunks int
	refuse bool
}

func (s *sender) send(data []byte) bool {
	if s.refuse {
		return false
	}
	s.got = append(s.got, data...)
	s.chunks++
	return true
}

// lastChunkStart is where the held-back chunk begins.
func lastChunkStart(size int) int { return (size - 1) / testChunk * testChunk }

func TestRelaySignedPackage(t *testing.T) {
	data := testPackage()
	releaseKey := testKey(t)
	otherKey := testKey(t)
	digest := sha256.Sum256(data)
	tampered := append([]byte(nil), data...)
	tampered[len(tampered)-1] ^= 1

	for _, tc := range []struct {
		name   string
		sig    []byte
		digest [32]byte
		want   error
	}{
		{"valid", detachSign(t, releaseKey, data), digest, nil},
		// The index's hash matches, the signature is over other bytes.
		{"signature of other bytes", detachSign(t, releaseKey, tampered), digest, ErrNotSigned},
		// The signature is good, the index lists another hash.
		{"hash mismatch", detachSign(t, releaseKey, data), sha256.Sum256(tampered), ErrMismatch},
	} {
		t.Run(tc.name, func(t *testing.T) {
			check, err := NewCheck(uint64(len(data)), tc.digest, pgpSignature(releaseKey, tc.sig))
			if err != nil {
				t.Fatal(err)
			}
			var s sender
			err = Relay(bytes.NewReader(data), s.send, check, testChunk)
			if !errors.Is(err, tc.want) {
				t.Fatalf("Relay = %v, want %v", err, tc.want)
			}
			if tc.want == nil {
				if !bytes.Equal(s.got, data) || s.chunks != 4 {
					t.Fatalf("sender got %d bytes in %d chunks", len(s.got), s.chunks)
				}
				return
			}
			// Every chunk but the last reached the sender.
			if !bytes.Equal(s.got, data[:lastChunkStart(len(data))]) {
				t.Fatalf("sender got %d bytes, want %d", len(s.got), lastChunkStart(len(data)))
			}
		})
	}

	// A signature by another key, or none, fails before the download starts.
	_, err := NewCheck(uint64(len(data)), digest, pgpSignature(releaseKey, detachSign(t, otherKey, data)))
	if !errors.Is(err, ErrNotSigned) {
		t.Fatalf("NewCheck with another key's signature = %v", err)
	}
	_, err = NewCheck(uint64(len(data)), digest, pgpSignature(releaseKey, []byte("not a signature")))
	if !errors.Is(err, ErrNotSigned) {
		t.Fatalf("NewCheck with a malformed signature = %v", err)
	}
}

func TestRelayDevPackage(t *testing.T) {
	data := testPackage()
	check, _ := NewCheck(uint64(len(data)), sha256.Sum256(data), nil)
	var s sender
	if err := Relay(bytes.NewReader(data), s.send, check, testChunk); err != nil || !bytes.Equal(s.got, data) {
		t.Fatalf("Relay = %v, sender got %d bytes", err, len(s.got))
	}

	check, _ = NewCheck(uint64(len(data)), sha256.Sum256(data[1:]), nil)
	s = sender{}
	if err := Relay(bytes.NewReader(data), s.send, check, testChunk); !errors.Is(err, ErrMismatch) ||
		len(s.got) != lastChunkStart(len(data)) {
		t.Fatalf("Relay = %v, sender got %d bytes", err, len(s.got))
	}
}

func TestRelayEndsEarly(t *testing.T) {
	data := testPackage()
	releaseKey := testKey(t)
	sig := detachSign(t, releaseKey, data)
	digest := sha256.Sum256(data)

	// The download ends before the package does.
	check, err := NewCheck(uint64(len(data)), digest, pgpSignature(releaseKey, sig))
	if err != nil {
		t.Fatal(err)
	}
	var s sender
	err = Relay(bytes.NewReader(data[:len(data)-10]), s.send, check, testChunk)
	if !errors.Is(err, ErrDownload) || len(s.got) != lastChunkStart(len(data)) {
		t.Fatalf("Relay = %v, sender got %d bytes", err, len(s.got))
	}

	// The update ends on the sender's side.
	check, err = NewCheck(uint64(len(data)), digest, pgpSignature(releaseKey, sig))
	if err != nil {
		t.Fatal(err)
	}
	if err := Relay(bytes.NewReader(data), (&sender{refuse: true}).send, check, testChunk); !errors.Is(err, ErrEnded) {
		t.Fatalf("Relay = %v", err)
	}
}
