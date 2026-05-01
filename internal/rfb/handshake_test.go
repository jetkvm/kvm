package rfb

import (
	"bytes"
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

// pipeConn returns two ends of a net.Pipe for in-memory protocol tests.
func pipeConn(t *testing.T) (server, client net.Conn) {
	t.Helper()
	a, b := net.Pipe()
	t.Cleanup(func() {
		_ = a.Close()
		_ = b.Close()
	})
	return a, b
}

func TestHandshakeServerVersion38(t *testing.T) {
	srvNC, cliNC := pipeConn(t)
	c := NewConn(srvNC)

	// Client writes its version in a goroutine.
	cliErr := make(chan error, 1)
	cliRead := make(chan []byte, 1)
	go func() {
		var srvVer [12]byte
		if _, err := io.ReadFull(cliNC, srvVer[:]); err != nil {
			cliErr <- err
			return
		}
		cliRead <- srvVer[:]
		if _, err := cliNC.Write([]byte(ProtocolVersion38)); err != nil {
			cliErr <- err
			return
		}
		cliErr <- nil
	}()

	v, err := c.HandshakeServerVersion()
	if err != nil {
		t.Fatalf("HandshakeServerVersion: %v", err)
	}
	if v != ProtocolVersion38 {
		t.Fatalf("client version: got %q, want %q", v, ProtocolVersion38)
	}
	if got := <-cliRead; string(got) != ProtocolVersion38 {
		t.Fatalf("server advertised %q", got)
	}
	if err := <-cliErr; err != nil {
		t.Fatalf("client goroutine: %v", err)
	}
}

func TestHandshakeServerVersionRejectsOldClients(t *testing.T) {
	srvNC, cliNC := pipeConn(t)
	c := NewConn(srvNC)

	cliErr := make(chan error, 1)
	go func() {
		var v [12]byte
		_, _ = io.ReadFull(cliNC, v[:])
		_, err := cliNC.Write([]byte("RFB 003.003\n"))
		cliErr <- err
	}()

	v, err := c.HandshakeServerVersion()
	if !errors.Is(err, ErrUnsupportedProtocol) {
		t.Fatalf("expected ErrUnsupportedProtocol, got %v", err)
	}
	if v != "RFB 003.003\n" {
		t.Fatalf("returned version %q", v)
	}
	if err := <-cliErr; err != nil {
		t.Fatalf("client goroutine: %v", err)
	}
}

func TestOfferSecurityTypesPicksMatch(t *testing.T) {
	srvNC, cliNC := pipeConn(t)
	c := NewConn(srvNC)

	srvErr := make(chan error, 1)
	srvType := make(chan SecurityType, 1)
	go func() {
		t, err := c.OfferSecurityTypes([]SecurityType{SecVNCAuth, SecNone})
		srvErr <- err
		srvType <- t
	}()

	// Client reads count + types, picks SecNone (1).
	if err := cliNC.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	var hdr [3]byte
	if _, err := io.ReadFull(cliNC, hdr[:]); err != nil {
		t.Fatal(err)
	}
	if hdr[0] != 2 {
		t.Fatalf("count: %d", hdr[0])
	}
	if hdr[1] != byte(SecVNCAuth) || hdr[2] != byte(SecNone) {
		t.Fatalf("types: %v", hdr[1:])
	}
	if _, err := cliNC.Write([]byte{byte(SecNone)}); err != nil {
		t.Fatal(err)
	}
	if err := <-srvErr; err != nil {
		t.Fatalf("OfferSecurityTypes: %v", err)
	}
	if got := <-srvType; got != SecNone {
		t.Fatalf("chosen: %d", got)
	}
}

func TestOfferSecurityTypesRejectsUnknown(t *testing.T) {
	srvNC, cliNC := pipeConn(t)
	c := NewConn(srvNC)

	srvErr := make(chan error, 1)
	go func() {
		_, err := c.OfferSecurityTypes([]SecurityType{SecVNCAuth})
		srvErr <- err
	}()

	var hdr [2]byte
	_, _ = io.ReadFull(cliNC, hdr[:])
	// Client picks something the server didn't offer (SecNone).
	_, _ = cliNC.Write([]byte{byte(SecNone)})

	err := <-srvErr
	if !errors.Is(err, ErrNoCommonSecurity) {
		t.Fatalf("expected ErrNoCommonSecurity, got %v", err)
	}
}

func TestSendServerInit(t *testing.T) {
	srvNC, cliNC := pipeConn(t)
	c := NewConn(srvNC)

	done := make(chan error, 1)
	go func() {
		done <- c.SendServerInit(ServerInit{
			Width:       1920,
			Height:      1080,
			PixelFormat: DefaultPixelFormat(),
			Name:        "JetKVM",
		})
	}()

	if err := cliNC.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	// 2+2 dimensions, 16 pixel format, 4 name length, name
	out := make([]byte, 2+2+16+4+len("JetKVM"))
	if _, err := io.ReadFull(cliNC, out); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("SendServerInit: %v", err)
	}

	// Width/Height
	if out[0] != 0x07 || out[1] != 0x80 { // 1920 = 0x0780
		t.Errorf("width: %x %x", out[0], out[1])
	}
	if out[2] != 0x04 || out[3] != 0x38 { // 1080 = 0x0438
		t.Errorf("height: %x %x", out[2], out[3])
	}
	// Pixel format byte 0 = 32 bpp
	if out[4] != 32 {
		t.Errorf("bpp: %d", out[4])
	}
	// Name length = 6, then "JetKVM"
	nameLen := uint32(out[20])<<24 | uint32(out[21])<<16 | uint32(out[22])<<8 | uint32(out[23])
	if nameLen != 6 {
		t.Errorf("name length: %d", nameLen)
	}
	if !bytes.Equal(out[24:], []byte("JetKVM")) {
		t.Errorf("name: %q", out[24:])
	}
}

func TestReadClientInit(t *testing.T) {
	srvNC, cliNC := pipeConn(t)
	c := NewConn(srvNC)

	go func() { _, _ = cliNC.Write([]byte{1}) }() // shared

	init, err := c.ReadClientInit()
	if err != nil {
		t.Fatalf("ReadClientInit: %v", err)
	}
	if !init.Shared {
		t.Errorf("expected Shared=true")
	}
}
