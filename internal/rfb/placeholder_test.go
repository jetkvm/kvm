package rfb

import (
	"bytes"
	"testing"
)

func TestPlaceholderImageDimensions(t *testing.T) {
	cases := []struct {
		w, h int
	}{
		{640, 480},
		{1280, 720},
		{1920, 1080},
		{800, 600},
	}
	for _, c := range cases {
		buf := PlaceholderImage(c.w, c.h)
		if want := c.w * c.h * 4; len(buf) != want {
			t.Errorf("%dx%d: got %d bytes, want %d", c.w, c.h, len(buf), want)
		}
	}
}

func TestPlaceholderImageZeroDims(t *testing.T) {
	if buf := PlaceholderImage(0, 100); buf != nil {
		t.Errorf("expected nil for width=0, got %d bytes", len(buf))
	}
	if buf := PlaceholderImage(100, 0); buf != nil {
		t.Errorf("expected nil for height=0")
	}
	if buf := PlaceholderImage(-1, 100); buf != nil {
		t.Errorf("expected nil for negative width")
	}
}

func TestPlaceholderImageBackgroundColour(t *testing.T) {
	// Top-left corner should be the dark blue background. We don't
	// know exactly which BGRA shade we picked without re-deriving it,
	// so just verify the alpha channel is opaque and the colour is
	// blue-dominant (B > R+G).
	buf := PlaceholderImage(640, 480)
	b, g, r, a := buf[0], buf[1], buf[2], buf[3]
	if a != 0xFF {
		t.Errorf("alpha at (0,0): got %#02x, want 0xff", a)
	}
	if int(b) <= int(r)+int(g) {
		t.Errorf("background not blue-dominant: B=%d R=%d G=%d", b, r, g)
	}
}

func TestPlaceholderImageHasForeground(t *testing.T) {
	// At least one pixel must differ from the background — otherwise
	// the text wasn't rendered at all.
	buf := PlaceholderImage(800, 600)
	if len(buf) < 8 {
		t.Fatal("buffer too small")
	}
	bgPixel := buf[0:4]
	hasFG := false
	for i := 0; i < len(buf); i += 4 {
		if !bytes.Equal(buf[i:i+4], bgPixel) {
			hasFG = true
			break
		}
	}
	if !hasFG {
		t.Errorf("placeholder image is uniform colour — text not rendered")
	}
}

func TestPlaceholderImageWithMessage(t *testing.T) {
	buf := PlaceholderImageWithMessage(640, 480, []string{"X"})
	if len(buf) != 640*480*4 {
		t.Errorf("len: %d", len(buf))
	}
}

func TestRGBAtoBGRAConversion(t *testing.T) {
	// One row, one pixel: R=0xAA, G=0xBB, B=0xCC, A=0xDD.
	src := []byte{0xAA, 0xBB, 0xCC, 0xDD}
	got := rgbaToBGRA(src, 1, 1, 4)
	want := []byte{0xCC, 0xBB, 0xAA, 0xDD}
	if !bytes.Equal(got, want) {
		t.Errorf("got %x, want %x", got, want)
	}
}

func TestRGBAtoBGRAStridePadding(t *testing.T) {
	// 2x1 RGBA with stride = 12 (4 bytes padding past the second pixel).
	src := []byte{
		0xAA, 0xBB, 0xCC, 0xDD, // pixel 0: R G B A
		0x11, 0x22, 0x33, 0x44, // pixel 1
		0x99, 0x99, 0x99, 0x99, // padding (must be ignored)
	}
	got := rgbaToBGRA(src, 2, 1, 12)
	want := []byte{
		0xCC, 0xBB, 0xAA, 0xDD,
		0x33, 0x22, 0x11, 0x44,
	}
	if !bytes.Equal(got, want) {
		t.Errorf("got %x, want %x", got, want)
	}
}
