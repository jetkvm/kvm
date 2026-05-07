package rfb

import (
	"bytes"
	"io"
	"testing"
	"time"
)

func TestBeginFramebufferUpdate(t *testing.T) {
	srvNC, cliNC := pipeConn(t)
	c := NewConn(srvNC)

	done := make(chan error, 1)
	go func() {
		if err := c.BeginFramebufferUpdate(3); err != nil {
			done <- err
			return
		}
		done <- c.Flush()
	}()

	if err := cliNC.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	out := make([]byte, 4)
	if _, err := io.ReadFull(cliNC, out); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("BeginFramebufferUpdate: %v", err)
	}
	// type=0, padding=0, count=3 BE
	want := []byte{0, 0, 0, 3}
	if !bytes.Equal(out, want) {
		t.Errorf("got %v, want %v", out, want)
	}
}

func TestWriteRectHeader(t *testing.T) {
	srvNC, cliNC := pipeConn(t)
	c := NewConn(srvNC)

	go func() {
		_ = c.WriteRectHeader(Rect{X: 1, Y: 2, W: 1920, H: 1080, Encoding: EncodingOpenH264})
		_ = c.Flush()
	}()

	out := make([]byte, 12)
	if err := cliNC.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadFull(cliNC, out); err != nil {
		t.Fatal(err)
	}
	want := []byte{
		0x00, 0x01, // x = 1
		0x00, 0x02, // y = 2
		0x07, 0x80, // w = 1920
		0x04, 0x38, // h = 1080
		0x00, 0x00, 0x00, 0x32, // encoding = 50
	}
	if !bytes.Equal(out, want) {
		t.Errorf("got %v, want %v", out, want)
	}
}

func TestWriteOpenH264Rect(t *testing.T) {
	srvNC, cliNC := pipeConn(t)
	c := NewConn(srvNC)

	nal := []byte{0x00, 0x00, 0x00, 0x01, 0x67, 0x42, 0x00, 0x1E} // dummy NAL
	go func() {
		_ = c.WriteOpenH264Rect(
			Rect{X: 0, Y: 0, W: 1920, H: 1080, Encoding: EncodingOpenH264},
			OpenH264FlagResetContext,
			nal,
		)
		_ = c.Flush()
	}()

	out := make([]byte, 12+4+4+len(nal))
	if err := cliNC.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadFull(cliNC, out); err != nil {
		t.Fatal(err)
	}

	// 12-byte rect header
	if out[10] != 0 || out[11] != 0x32 {
		t.Errorf("encoding bytes: %x %x", out[10], out[11])
	}
	// length = len(nal) BE u32
	if out[12] != 0 || out[13] != 0 || out[14] != 0 || out[15] != byte(len(nal)) {
		t.Errorf("length: %v", out[12:16])
	}
	// flags = OpenH264FlagResetContext = 0x1 BE u32
	if out[16] != 0 || out[17] != 0 || out[18] != 0 || out[19] != 0x01 {
		t.Errorf("flags: %v", out[16:20])
	}
	// payload
	if !bytes.Equal(out[20:], nal) {
		t.Errorf("payload mismatch")
	}
}

func TestWriteRawRect(t *testing.T) {
	srvNC, cliNC := pipeConn(t)
	c := NewConn(srvNC)

	pixels := bytes.Repeat([]byte{0xAB}, 4*4*4) // 4x4 32bpp BGRA
	go func() {
		_ = c.WriteRawRect(
			Rect{X: 0, Y: 0, W: 4, H: 4, Encoding: EncodingRaw},
			pixels,
		)
		_ = c.Flush()
	}()

	out := make([]byte, 12+len(pixels))
	if err := cliNC.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadFull(cliNC, out); err != nil {
		t.Fatal(err)
	}
	// encoding = 0
	if out[8] != 0 || out[9] != 0 || out[10] != 0 || out[11] != 0 {
		t.Errorf("encoding: %v", out[8:12])
	}
	if !bytes.Equal(out[12:], pixels) {
		t.Errorf("pixels mismatch")
	}
}

func TestWriteDesktopSizeRect(t *testing.T) {
	srvNC, cliNC := pipeConn(t)
	c := NewConn(srvNC)

	go func() {
		_ = c.WriteDesktopSizeRect(1920, 1080)
		_ = c.Flush()
	}()

	out := make([]byte, 12)
	if err := cliNC.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadFull(cliNC, out); err != nil {
		t.Fatal(err)
	}

	// x=0, y=0, w=1920, h=1080, encoding=-223 (0xFFFFFF21)
	want := []byte{
		0x00, 0x00,
		0x00, 0x00,
		0x07, 0x80,
		0x04, 0x38,
		0xFF, 0xFF, 0xFF, 0x21,
	}
	if !bytes.Equal(out, want) {
		t.Errorf("got %v, want %v", out, want)
	}
}

func TestWriteExtendedMouseButtonsAnnounceRect(t *testing.T) {
	srvNC, cliNC := pipeConn(t)
	c := NewConn(srvNC)

	go func() {
		_ = c.WriteExtendedMouseButtonsAnnounceRect()
		_ = c.Flush()
	}()

	out := make([]byte, 12)
	if err := cliNC.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadFull(cliNC, out); err != nil {
		t.Fatal(err)
	}
	// x=0, y=0, w=0, h=0, encoding=-316 (0xFFFFFEC4 in two's complement)
	want := []byte{
		0x00, 0x00,
		0x00, 0x00,
		0x00, 0x00,
		0x00, 0x00,
		0xFF, 0xFF, 0xFE, 0xC4,
	}
	if !bytes.Equal(out, want) {
		t.Errorf("got %v, want %v", out, want)
	}
}

func TestWriteRawRectRejectsWrongEncoding(t *testing.T) {
	srvNC, _ := pipeConn(t)
	c := NewConn(srvNC)

	err := c.WriteRawRect(Rect{Encoding: EncodingOpenH264}, nil)
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestWriteOpenH264RectRejectsWrongEncoding(t *testing.T) {
	srvNC, _ := pipeConn(t)
	c := NewConn(srvNC)

	err := c.WriteOpenH264Rect(Rect{Encoding: EncodingRaw}, 0, nil)
	if err == nil {
		t.Fatalf("expected error")
	}
}
