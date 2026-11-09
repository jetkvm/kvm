package release

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"hash"
	"io"
)

// The checks on a release package as it streams through the KVM, and the
// order that keeps an unchecked package from reaching the sender: every
// chunk but the last goes to the sender as it arrives, and the last one only
// once the size, the SHA-256 from the index and the package's own signature
// check out. A sender without the last chunk cannot complete its hash, so it
// installs nothing.

var (
	ErrDownload  = errors.New("the package download failed")
	ErrMismatch  = errors.New("the package does not match the release index")
	ErrNotSigned = errors.New("the package is not signed with the JetKVM release key")
	ErrEnded     = errors.New("the update ended")
)

// Signature checks a detached signature over the bytes written to it.
type Signature interface {
	// Write adds package bytes. A zero-length write returns once the
	// signature is known to be by the release key, or fails.
	io.Writer
	// Verdict ends the check: nil when the bytes written carry the
	// signature.
	Verdict() error
	// Abort ends the check without a verdict.
	Abort()
}

// Check is the checks on one package.
type Check struct {
	size     uint64
	expected [32]byte
	sha      hash.Hash
	sig      Signature
	received uint64
}

// NewCheck starts the checks of a package of size bytes with the SHA-256
// the index lists. sig is the package's signature check, or nil for a dev
// package, which carries no signature. It fails, and nothing needs ending,
// when the signature is not by the release key.
func NewCheck(size uint64, sha [32]byte, sig Signature) (*Check, error) {
	if sig != nil {
		if _, err := sig.Write(nil); err != nil {
			sig.Abort()
			return nil, ErrNotSigned
		}
	}
	return &Check{size: size, expected: sha, sha: sha256.New(), sig: sig}, nil
}

// Abort ends the checks without a verdict.
func (c *Check) Abort() {
	if c.sig != nil {
		c.sig.Abort()
		c.sig = nil
	}
}

func (c *Check) update(data []byte) error {
	c.received += uint64(len(data))
	c.sha.Write(data)
	if c.sig != nil {
		if _, err := c.sig.Write(data); err != nil {
			c.Abort()
			return ErrNotSigned
		}
	}
	return nil
}

// finish is the verdict on the whole package. It ends the checks.
func (c *Check) finish() error {
	if !bytes.Equal(c.sha.Sum(nil), c.expected[:]) {
		c.Abort()
		return ErrMismatch
	}
	if c.sig == nil {
		return nil
	}
	sig := c.sig
	c.sig = nil
	if sig.Verdict() != nil {
		return ErrNotSigned
	}
	return nil
}

// Relay reads the package from download in chunks of at most chunk bytes,
// checks it and hands each chunk to send, the last one only after every
// check passed. send returns false once the update has ended; the chunk's
// bytes may be reused after it returns. Relay ends the checks. It returns
// nil once the sender has every byte, or why the sender must not install
// (the caller then cancels the update).
func Relay(download io.Reader, send func([]byte) bool, c *Check, chunk int) error {
	buf := make([]byte, min(uint64(chunk), c.size))
	for c.received < c.size {
		data := buf[:min(uint64(len(buf)), c.size-c.received)]
		if _, err := io.ReadFull(download, data); err != nil {
			c.Abort()
			return ErrDownload
		}
		if err := c.update(data); err != nil {
			return err
		}
		// The last chunk waits for the verdict.
		if c.received == c.size {
			if err := c.finish(); err != nil {
				return err
			}
		}
		if !send(data) {
			c.Abort()
			return ErrEnded
		}
	}
	return nil
}

// StreamSignature is a Signature that runs verify, which reads the signed
// bytes to their end, on the bytes written to it as they arrive.
func StreamSignature(verify func(signed io.Reader) error) Signature {
	r, w := io.Pipe()
	s := &streamSignature{w: w, done: make(chan error, 1)}
	go func() {
		err := verify(r)
		// A write after verify stopped reading fails, so no byte goes
		// unchecked.
		r.CloseWithError(err)
		s.done <- err
	}()
	return s
}

var errAborted = errors.New("the package check was aborted")

type streamSignature struct {
	w    *io.PipeWriter
	done chan error
}

func (s *streamSignature) Write(p []byte) (int, error) { return s.w.Write(p) }

func (s *streamSignature) Verdict() error {
	s.w.Close()
	return <-s.done
}

func (s *streamSignature) Abort() { s.w.CloseWithError(errAborted) }
