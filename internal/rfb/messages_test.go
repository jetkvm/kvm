package rfb

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

func TestReadKeyEvent(t *testing.T) {
	srvNC, cliNC := pipeConn(t)
	c := NewConn(srvNC)

	go func() {
		// type=4, down=1, padding 2, keysym 0xFFE1 (LShift) BE
		_, _ = cliNC.Write([]byte{4, 1, 0, 0, 0x00, 0x00, 0xFF, 0xE1})
	}()

	msg, err := c.ReadClientMessage()
	if err != nil {
		t.Fatalf("ReadClientMessage: %v", err)
	}
	ke, ok := msg.(KeyEventMessage)
	if !ok {
		t.Fatalf("got %T, want KeyEventMessage", msg)
	}
	if !ke.Down || ke.Keysym != 0xFFE1 {
		t.Fatalf("got %+v", ke)
	}
}

func TestReadPointerEvent(t *testing.T) {
	srvNC, cliNC := pipeConn(t)
	c := NewConn(srvNC)

	go func() {
		// type=5, mask=0x01, x=100 (0x0064), y=200 (0x00C8)
		_, _ = cliNC.Write([]byte{5, 0x01, 0x00, 0x64, 0x00, 0xC8})
	}()

	msg, err := c.ReadClientMessage()
	if err != nil {
		t.Fatalf("ReadClientMessage: %v", err)
	}
	pe, ok := msg.(PointerEventMessage)
	if !ok {
		t.Fatalf("got %T, want PointerEventMessage", msg)
	}
	if pe.ButtonMask != 0x01 || pe.X != 100 || pe.Y != 200 {
		t.Fatalf("got %+v", pe)
	}
}

func TestReadFramebufferUpdateRequest(t *testing.T) {
	srvNC, cliNC := pipeConn(t)
	c := NewConn(srvNC)

	go func() {
		// type=3, incr=1, x=0, y=0, w=1920 (0x0780), h=1080 (0x0438)
		_, _ = cliNC.Write([]byte{3, 1, 0x00, 0x00, 0x00, 0x00, 0x07, 0x80, 0x04, 0x38})
	}()

	msg, err := c.ReadClientMessage()
	if err != nil {
		t.Fatalf("ReadClientMessage: %v", err)
	}
	r, ok := msg.(FramebufferUpdateRequestMessage)
	if !ok {
		t.Fatalf("got %T, want FramebufferUpdateRequestMessage", msg)
	}
	if !r.Incremental || r.W != 1920 || r.H != 1080 {
		t.Fatalf("got %+v", r)
	}
}

func TestReadSetEncodings(t *testing.T) {
	srvNC, cliNC := pipeConn(t)
	c := NewConn(srvNC)

	go func() {
		// type=2, padding, count=3 (BE), then three int32 BE: 50 (OpenH264), 0 (Raw), -223 (DesktopSize)
		buf := []byte{2, 0, 0x00, 0x03}
		buf = append(buf, 0x00, 0x00, 0x00, 0x32) // 50
		buf = append(buf, 0x00, 0x00, 0x00, 0x00) // 0
		buf = append(buf, 0xFF, 0xFF, 0xFF, 0x21) // -223 (two's complement)
		_, _ = cliNC.Write(buf)
	}()

	msg, err := c.ReadClientMessage()
	if err != nil {
		t.Fatalf("ReadClientMessage: %v", err)
	}
	se, ok := msg.(SetEncodingsMessage)
	if !ok {
		t.Fatalf("got %T, want SetEncodingsMessage", msg)
	}
	want := []EncodingType{EncodingOpenH264, EncodingRaw, EncodingDesktopSize}
	if len(se.Encodings) != len(want) {
		t.Fatalf("len: got %d, want %d", len(se.Encodings), len(want))
	}
	for i := range want {
		if se.Encodings[i] != want[i] {
			t.Errorf("encodings[%d]: got %d, want %d", i, se.Encodings[i], want[i])
		}
	}
}

func TestReadSetPixelFormat(t *testing.T) {
	srvNC, cliNC := pipeConn(t)
	c := NewConn(srvNC)

	go func() {
		buf := []byte{0, 0, 0, 0} // type + 3 padding
		var pf [16]byte
		MarshalPixelFormat(DefaultPixelFormat(), pf[:])
		buf = append(buf, pf[:]...)
		_, _ = cliNC.Write(buf)
	}()

	msg, err := c.ReadClientMessage()
	if err != nil {
		t.Fatalf("ReadClientMessage: %v", err)
	}
	spf, ok := msg.(SetPixelFormatMessage)
	if !ok {
		t.Fatalf("got %T, want SetPixelFormatMessage", msg)
	}
	if spf.PixelFormat.BitsPerPixel != 32 {
		t.Errorf("bpp: %d", spf.PixelFormat.BitsPerPixel)
	}
}

func TestReadClientCutText(t *testing.T) {
	srvNC, cliNC := pipeConn(t)
	c := NewConn(srvNC)

	go func() {
		buf := []byte{6, 0, 0, 0} // type + 3 padding
		body := []byte("hello")
		buf = append(buf, 0x00, 0x00, 0x00, byte(len(body))) // length BE u32
		buf = append(buf, body...)
		_, _ = cliNC.Write(buf)
	}()

	msg, err := c.ReadClientMessage()
	if err != nil {
		t.Fatalf("ReadClientMessage: %v", err)
	}
	cct, ok := msg.(ClientCutTextMessage)
	if !ok {
		t.Fatalf("got %T, want ClientCutTextMessage", msg)
	}
	if !bytes.Equal(cct.Text, []byte("hello")) {
		t.Errorf("text: %q", cct.Text)
	}
}

func TestReadUnknownMessageType(t *testing.T) {
	srvNC, cliNC := pipeConn(t)
	c := NewConn(srvNC)

	go func() { _, _ = cliNC.Write([]byte{99}) }()

	_, err := c.ReadClientMessage()
	if !errors.Is(err, ErrUnknownClientMessage) {
		t.Fatalf("expected ErrUnknownClientMessage, got %v", err)
	}
}

func TestReadEOFOnEmptyConn(t *testing.T) {
	srvNC, cliNC := pipeConn(t)
	c := NewConn(srvNC)

	_ = cliNC.Close()

	_, err := c.ReadClientMessage()
	if err == nil {
		t.Fatalf("expected error on closed conn")
	}
	if !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrClosedPipe) {
		t.Logf("got %v (acceptable network error)", err)
	}
}
