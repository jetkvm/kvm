package rfb

import (
	"image"
	"image/color"
	"image/draw"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

// PlaceholderMessage is the default message shown to non-H.264 VNC
// clients. The text explains why no image is being sent.
var PlaceholderMessage = []string{
	"JetKVM VNC server",
	"",
	"This client did not negotiate H.264 (RFB encoding 50).",
	"Please use TigerVNC >= 1.13.0 built with FFmpeg.",
	"",
	"Mouse and keyboard input still work.",
}

// PlaceholderImage renders a width x height 32-bit BGRA buffer with
// PlaceholderMessage rendered on a dark blue background. Suitable for
// use as a Raw rectangle payload (encoding 0) — the byte layout is
// little-endian B, G, R, A in row-major order.
//
// Returns nil if width or height is zero or negative.
func PlaceholderImage(width, height int) []byte {
	return PlaceholderImageWithMessage(width, height, PlaceholderMessage)
}

// PlaceholderImageWithMessage is the same as PlaceholderImage but lets
// the caller substitute the text shown.
func PlaceholderImageWithMessage(width, height int, lines []string) []byte {
	if width <= 0 || height <= 0 {
		return nil
	}

	const (
		bgR, bgG, bgB    uint8 = 0x10, 0x18, 0x40 // dark blue
		fgR, fgG, fgB    uint8 = 0xFF, 0xFF, 0xFF
		lineSpacingPx          = 18
	)

	rgba := image.NewRGBA(image.Rect(0, 0, width, height))
	bg := color.RGBA{R: bgR, G: bgG, B: bgB, A: 0xFF}
	draw.Draw(rgba, rgba.Bounds(), &image.Uniform{C: bg}, image.Point{}, draw.Src)

	face := basicfont.Face7x13
	totalH := len(lines) * lineSpacingPx
	startY := (height - totalH) / 2
	if startY < lineSpacingPx {
		startY = lineSpacingPx
	}

	drawer := &font.Drawer{
		Dst:  rgba,
		Src:  &image.Uniform{C: color.RGBA{R: fgR, G: fgG, B: fgB, A: 0xFF}},
		Face: face,
	}

	for i, line := range lines {
		if line == "" {
			continue
		}
		// Center horizontally based on measured advance.
		w := drawer.MeasureString(line).Round()
		x := (width - w) / 2
		if x < 4 {
			x = 4
		}
		y := startY + i*lineSpacingPx + face.Ascent
		drawer.Dot = fixed.P(x, y)
		drawer.DrawString(line)
	}

	return rgbaToBGRA(rgba.Pix, width, height, rgba.Stride)
}

// rgbaToBGRA converts an in-memory image.RGBA's Pix slice (R, G, B, A
// in that order, with the supplied stride) to the BGRA layout the RFB
// default pixel format expects (B, G, R, A on the wire, little-endian).
func rgbaToBGRA(rgba []byte, width, height, stride int) []byte {
	out := make([]byte, width*height*4)
	for y := 0; y < height; y++ {
		srcRow := rgba[y*stride : y*stride+width*4]
		dstRow := out[y*width*4 : (y+1)*width*4]
		for x := 0; x < width; x++ {
			r := srcRow[x*4+0]
			g := srcRow[x*4+1]
			b := srcRow[x*4+2]
			a := srcRow[x*4+3]
			dstRow[x*4+0] = b
			dstRow[x*4+1] = g
			dstRow[x*4+2] = r
			dstRow[x*4+3] = a
		}
	}
	return out
}
