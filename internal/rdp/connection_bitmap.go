package rdp

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"runtime"
	"sync"

	"github.com/jetkvm/kvm/internal/rdp/protocol"
)

// Bitmap update handling for RDP connections.
// This file contains all bitmap-related code for clients that don't support RDPGFX.

// Tile size for Fast-Path bitmap updates with fragmentation support.
// With fragmentation enabled (via Multifragment Update Capability), we can use
// larger tiles that get split across multiple PDUs automatically.
// Using 256x256 tiles for better visual experience and reduced overhead.
// Each tile: 256x256 at 32bpp = 262144 bytes (fragmented into ~16KB chunks)
const bitmapTileSize = 256

// tileBufferPool reduces allocations for bitmap tile data.
// Max tile size: 256x256x4 = 262144 bytes (256KB per tile)
var tileBufferPool = sync.Pool{
	New: func() any {
		buf := make([]byte, bitmapTileSize*bitmapTileSize*4)
		return &buf
	},
}

// bgrxBufferPool reduces allocations for YUV→BGRX conversion.
// Max size: 1920x1080x4 = ~8MB (handles 1080p)
var bgrxBufferPool = sync.Pool{
	New: func() interface{} {
		buf := make([]byte, 1920*1080*4)
		return &buf
	},
}

// rgbTileSize is smaller than bitmapTileSize to fit within Fast-Path reassembly limits.
// 64x64 at 32bpp = 16384 bytes per tile.
const rgbTileSize = 64

// maxTilesPerRGBUpdate is the maximum tiles per bitmap update to stay under 64KB reassembly limit.
// 64x64 tile = 16384 bytes + 18 byte header = 16402 bytes per tile
// 3 tiles = ~49KB, safely under 64KB
const maxTilesPerRGBUpdate = 3

// rgbTileBufferPool provides reusable buffers for RGB tile data.
var rgbTileBufferPool = sync.Pool{
	New: func() any {
		buf := make([]byte, rgbTileSize*rgbTileSize*4)
		return &buf
	},
}

// tileRect represents a single bitmap tile for RDP updates.
type tileRect struct {
	left, top     int
	right, bottom int
	data          []byte
}

// convertYUV422ToBGRX converts YUV422 YUYV format to BGRX.
// Input: YUYV packed (2 pixels in 4 bytes: Y0 U0 Y1 V0)
// Output: BGRX (4 bytes per pixel: B G R X)
// This is optimized for speed with integer arithmetic (no floats).
func convertYUV422ToBGRX(yuv []byte, width, height int) []byte {
	// Get pooled buffer
	bufPtr := bgrxBufferPool.Get().(*[]byte)
	bgrx := *bufPtr

	// Ensure buffer is large enough
	bgrxSize := width * height * 4
	if len(bgrx) < bgrxSize {
		bgrx = make([]byte, bgrxSize)
		*bufPtr = bgrx
	}

	// YUV422 YUYV: 2 bytes per pixel average (4 bytes for 2 pixels)
	// Process 2 pixels at a time
	yuvStride := width * 2
	bgrxStride := width * 4

	for row := 0; row < height; row++ {
		yuvOffset := row * yuvStride
		bgrxOffset := row * bgrxStride

		for col := 0; col < width; col += 2 {
			// Extract YUYV components (4 bytes = 2 pixels)
			y0 := int(yuv[yuvOffset])
			u := int(yuv[yuvOffset+1])
			y1 := int(yuv[yuvOffset+2])
			v := int(yuv[yuvOffset+3])
			yuvOffset += 4

			// Precompute UV terms (integer approximation of BT.601)
			// R = Y + 1.402*(V-128) ≈ Y + (359*(V-128))>>8
			// G = Y - 0.344*(U-128) - 0.714*(V-128) ≈ Y - (88*(U-128) + 183*(V-128))>>8
			// B = Y + 1.772*(U-128) ≈ Y + (454*(U-128))>>8
			u128 := u - 128
			v128 := v - 128

			rComp := (359 * v128) >> 8
			gComp := (88*u128 + 183*v128) >> 8
			bComp := (454 * u128) >> 8

			// Pixel 0
			r0 := y0 + rComp
			g0 := y0 - gComp
			b0 := y0 + bComp

			// Clamp to [0, 255]
			if r0 < 0 {
				r0 = 0
			} else if r0 > 255 {
				r0 = 255
			}
			if g0 < 0 {
				g0 = 0
			} else if g0 > 255 {
				g0 = 255
			}
			if b0 < 0 {
				b0 = 0
			} else if b0 > 255 {
				b0 = 255
			}

			// BGRX format
			bgrx[bgrxOffset] = byte(b0)
			bgrx[bgrxOffset+1] = byte(g0)
			bgrx[bgrxOffset+2] = byte(r0)
			bgrx[bgrxOffset+3] = 0xFF
			bgrxOffset += 4

			// Pixel 1
			r1 := y1 + rComp
			g1 := y1 - gComp
			b1 := y1 + bComp

			// Clamp to [0, 255]
			if r1 < 0 {
				r1 = 0
			} else if r1 > 255 {
				r1 = 255
			}
			if g1 < 0 {
				g1 = 0
			} else if g1 > 255 {
				g1 = 255
			}
			if b1 < 0 {
				b1 = 0
			} else if b1 > 255 {
				b1 = 255
			}

			bgrx[bgrxOffset] = byte(b1)
			bgrx[bgrxOffset+1] = byte(g1)
			bgrx[bgrxOffset+2] = byte(r1)
			bgrx[bgrxOffset+3] = 0xFF
			bgrxOffset += 4
		}

		// Yield every row to prevent scheduler starvation on single-core systems
		if row&0x3F == 0 { // Every 64 rows
			runtime.Gosched()
		}
	}

	return bgrx[:bgrxSize]
}

// releaseBGRXBuffer returns a BGRX buffer to the pool.
// Must be called after the buffer is no longer needed.
func releaseBGRXBuffer(buf []byte) {
	bgrxBufferPool.Put(&buf)
}

// SendBitmapUpdate sends a bitmap update PDU to the client.
// This is used for clients that don't support RDPGFX (like Jump Desktop).
// The frame should be JPEG data which will be decoded and sent as RGB bitmap.
func (c *Connection) SendBitmapUpdate(jpegData []byte) error {
	if c.closed.Load() {
		return nil
	}

	// Decode JPEG
	img, err := jpeg.Decode(bytes.NewReader(jpegData))
	if err != nil {
		return fmt.Errorf("failed to decode JPEG: %w", err)
	}

	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()

	// Send bitmap update using tiles - optimized path for YCbCr (common JPEG format)
	return c.sendTiledBitmapUpdateFast(img, width, height)
}

// sendTiledBitmapUpdateFast is an optimized version that:
// - Uses buffer pooling to reduce GC pressure
// - Sends tiles immediately without building a full list
// - Optimizes YCbCr→BGRX conversion with direct pixel access
func (c *Connection) sendTiledBitmapUpdateFast(img image.Image, width, height int) error {
	// Calculate tile grid
	tilesX := (width + bitmapTileSize - 1) / bitmapTileSize
	tilesY := (height + bitmapTileSize - 1) / bitmapTileSize

	// Get pooled buffer for tile data (reused across tiles)
	bufPtr := tileBufferPool.Get().(*[]byte)
	tileBuffer := *bufPtr
	defer tileBufferPool.Put(bufPtr)

	// Try to get direct pixel access based on image type
	var ycbcr *image.YCbCr
	var rgba *image.RGBA
	var nrgba *image.NRGBA

	switch v := img.(type) {
	case *image.YCbCr:
		ycbcr = v
	case *image.RGBA:
		rgba = v
	case *image.NRGBA:
		nrgba = v
	}

	// Process and send each tile immediately
	var tile tileRect
	for ty := 0; ty < tilesY; ty++ {
		for tx := 0; tx < tilesX; tx++ {
			// Calculate tile bounds
			left := tx * bitmapTileSize
			top := ty * bitmapTileSize
			right := left + bitmapTileSize - 1
			bottom := top + bitmapTileSize - 1

			// Clamp to image bounds
			if right >= width {
				right = width - 1
			}
			if bottom >= height {
				bottom = height - 1
			}

			tileW := right - left + 1
			tileH := bottom - top + 1
			dataSize := tileW * tileH * 4

			// Use appropriate conversion based on image type
			if ycbcr != nil {
				convertYCbCrTileToBGRX(ycbcr, left, top, tileW, tileH, tileBuffer)
			} else if rgba != nil {
				convertRGBATileToBGRX(rgba, left, top, tileW, tileH, tileBuffer)
			} else if nrgba != nil {
				convertNRGBATileToBGRX(nrgba, left, top, tileW, tileH, tileBuffer)
			} else {
				// Fallback to generic (slower) conversion
				convertGenericTileToBGRX(img, left, top, tileW, tileH, tileBuffer)
			}

			// Setup tile for sending
			tile.left = left
			tile.top = top
			tile.right = right
			tile.bottom = bottom
			tile.data = tileBuffer[:dataSize]

			// Send this tile via Fast-Path (supports fragmentation for large tiles)
			if err := c.sendFastPathBitmapUpdate([]tileRect{tile}); err != nil {
				return err
			}

			// Yield to scheduler to prevent starving other goroutines (HID, HTTPS)
			// Critical on single-core systems like RV1106
			runtime.Gosched()
		}
	}

	return nil
}

// convertYCbCrTileToBGRX converts a tile from YCbCr to BGRX format (bottom-up scanlines).
// This is the fast path for JPEG images which are typically YCbCr.
func convertYCbCrTileToBGRX(img *image.YCbCr, left, top, tileW, tileH int, dst []byte) {
	for y := 0; y < tileH; y++ {
		srcY := top + y
		dstY := tileH - 1 - y // RDP expects bottom-up
		dstRowOffset := dstY * tileW * 4

		for x := 0; x < tileW; x++ {
			srcX := left + x
			r, g, b := color.YCbCrToRGB(
				img.Y[img.YOffset(srcX, srcY)],
				img.Cb[img.COffset(srcX, srcY)],
				img.Cr[img.COffset(srcX, srcY)],
			)
			offset := dstRowOffset + x*4
			dst[offset+0] = b
			dst[offset+1] = g
			dst[offset+2] = r
			dst[offset+3] = 0
		}
	}
}

// convertRGBATileToBGRX converts a tile from RGBA to BGRX format (bottom-up scanlines).
func convertRGBATileToBGRX(img *image.RGBA, left, top, tileW, tileH int, dst []byte) {
	stride := img.Stride
	pix := img.Pix
	minX := img.Rect.Min.X
	minY := img.Rect.Min.Y

	for y := 0; y < tileH; y++ {
		srcY := top + y - minY
		dstY := tileH - 1 - y
		srcRowOffset := srcY * stride
		dstRowOffset := dstY * tileW * 4

		for x := 0; x < tileW; x++ {
			srcX := left + x - minX
			srcOffset := srcRowOffset + srcX*4
			dstOffset := dstRowOffset + x*4

			dst[dstOffset+0] = pix[srcOffset+2] // B
			dst[dstOffset+1] = pix[srcOffset+1] // G
			dst[dstOffset+2] = pix[srcOffset+0] // R
			dst[dstOffset+3] = 0                // X
		}
	}
}

// convertNRGBATileToBGRX converts a tile from NRGBA to BGRX format (bottom-up scanlines).
func convertNRGBATileToBGRX(img *image.NRGBA, left, top, tileW, tileH int, dst []byte) {
	stride := img.Stride
	pix := img.Pix
	minX := img.Rect.Min.X
	minY := img.Rect.Min.Y

	for y := 0; y < tileH; y++ {
		srcY := top + y - minY
		dstY := tileH - 1 - y
		srcRowOffset := srcY * stride
		dstRowOffset := dstY * tileW * 4

		for x := 0; x < tileW; x++ {
			srcX := left + x - minX
			srcOffset := srcRowOffset + srcX*4
			dstOffset := dstRowOffset + x*4

			dst[dstOffset+0] = pix[srcOffset+2] // B
			dst[dstOffset+1] = pix[srcOffset+1] // G
			dst[dstOffset+2] = pix[srcOffset+0] // R
			dst[dstOffset+3] = 0                // X
		}
	}
}

// convertGenericTileToBGRX is the fallback for any image type.
// It uses the image.Image interface which is slower but always works.
func convertGenericTileToBGRX(img image.Image, left, top, tileW, tileH int, dst []byte) {
	for y := 0; y < tileH; y++ {
		srcY := top + y
		dstY := tileH - 1 - y
		dstRowOffset := dstY * tileW * 4

		for x := 0; x < tileW; x++ {
			r, g, b, _ := img.At(left+x, srcY).RGBA()
			offset := dstRowOffset + x*4
			dst[offset+0] = byte(b >> 8)
			dst[offset+1] = byte(g >> 8)
			dst[offset+2] = byte(r >> 8)
			dst[offset+3] = 0
		}
	}
}

// sendFastPathBitmapUpdate sends a bitmap update via Fast-Path with fragmentation support.
// Per MS-RDPBCGR 2.2.9.1.2 and 2.2.9.1.2.1.2 (TS_FP_UPDATE_BITMAP).
// This bypasses the MCS 16KB limit by using fragmentation for large updates.
func (c *Connection) sendFastPathBitmapUpdate(tiles []tileRect) error {
	// Build the bitmap update data (same format as slow-path)
	// TS_UPDATE_BITMAP_DATA: updateType(2) + numberRectangles(2) + rectangles
	totalDataLen := 4 // updateType + numberRectangles
	for _, tile := range tiles {
		totalDataLen += 18 + len(tile.data) // TS_BITMAP_DATA header + pixel data
	}

	bitmapData := make([]byte, totalDataLen)
	binary.LittleEndian.PutUint16(bitmapData[0:2], protocol.UpdateTypeBitmap)
	binary.LittleEndian.PutUint16(bitmapData[2:4], uint16(len(tiles)))
	pos := 4

	for _, tile := range tiles {
		tileW := tile.right - tile.left + 1
		tileH := tile.bottom - tile.top + 1

		binary.LittleEndian.PutUint16(bitmapData[pos:pos+2], uint16(tile.left))
		binary.LittleEndian.PutUint16(bitmapData[pos+2:pos+4], uint16(tile.top))
		binary.LittleEndian.PutUint16(bitmapData[pos+4:pos+6], uint16(tile.right))
		binary.LittleEndian.PutUint16(bitmapData[pos+6:pos+8], uint16(tile.bottom))
		binary.LittleEndian.PutUint16(bitmapData[pos+8:pos+10], uint16(tileW))
		binary.LittleEndian.PutUint16(bitmapData[pos+10:pos+12], uint16(tileH))
		binary.LittleEndian.PutUint16(bitmapData[pos+12:pos+14], 32)
		binary.LittleEndian.PutUint16(bitmapData[pos+14:pos+16], protocol.BitmapCompressionNone)
		binary.LittleEndian.PutUint16(bitmapData[pos+16:pos+18], uint16(len(tile.data)))
		pos += 18
		copy(bitmapData[pos:], tile.data)
		pos += len(tile.data)
	}

	// Maximum fragment size (leave room for Fast-Path headers)
	// Fast-Path PDU max = 16383, headers = ~10 bytes, so use 16000 for safety
	const maxFragmentSize = 16000

	if len(bitmapData) <= maxFragmentSize {
		// Single fragment - no fragmentation needed
		return c.sendFastPathFragment(bitmapData, protocol.FastPathFragSingle)
	}

	// Fragment the data
	offset := 0
	remaining := len(bitmapData)
	isFirst := true

	for remaining > 0 {
		chunkSize := remaining
		if chunkSize > maxFragmentSize {
			chunkSize = maxFragmentSize
		}

		var fragFlag byte
		if isFirst {
			fragFlag = protocol.FastPathFragFirst
			isFirst = false
		} else if remaining-chunkSize > 0 {
			fragFlag = protocol.FastPathFragNext
		} else {
			fragFlag = protocol.FastPathFragLast
		}

		if err := c.sendFastPathFragment(bitmapData[offset:offset+chunkSize], fragFlag); err != nil {
			return err
		}

		offset += chunkSize
		remaining -= chunkSize
	}

	return nil
}

// sendFastPathFragment sends a single Fast-Path fragment.
func (c *Connection) sendFastPathFragment(data []byte, fragFlag byte) error {
	// Fast-Path Update structure (MS-RDPBCGR 2.2.9.1.2.1):
	// - updateHeader (1 byte): updateCode(4 bits) | fragmentation(2 bits) | compression(2 bits)
	//   compression = 0: no compression, NO size field
	//   compression = 2 (0x80): no compression, HAS size field (required for fragmentation)
	// - compressionFlags (1 byte): only present if compression != 0
	// - size (2 bytes): only present if compression != 0
	// - updateData (variable)

	// Set compression = 2 (0x80) to indicate size field is present
	updateHeader := protocol.FastPathUpdateBitmap | fragFlag | 0x80

	// Fast-Path PDU structure:
	// - fpOutputHeader (1 byte): action(2 bits) | reserved(4 bits) | flags(2 bits)
	// - length1 (1 byte)
	// - length2 (1 byte, optional)
	// - fpOutputUpdates (variable)

	// Calculate total PDU length
	// Update: header(1) + compressionFlags(1) + size(2) + data
	updateLen := 1 + 1 + 2 + len(data)

	// PDU: fpOutputHeader(1) + length(1-2) + update
	pduLen := 1 + updateLen
	if pduLen >= 128 {
		pduLen++ // Need 2-byte length encoding
	}
	pduLen++ // length1 byte

	buf := make([]byte, pduLen)
	pos := 0

	// fpOutputHeader: action = 0 (Fast-Path), no encryption
	buf[pos] = protocol.FastPathActionFastPath
	pos++

	// Length encoding
	totalLen := pduLen
	if totalLen < 128 {
		buf[pos] = byte(totalLen)
		pos++
	} else {
		buf[pos] = byte(0x80 | (totalLen >> 8))
		buf[pos+1] = byte(totalLen)
		pos += 2
	}

	// Fast-Path Update header
	buf[pos] = updateHeader
	pos++

	// compressionFlags (required when compression bits != 0)
	buf[pos] = 0x00 // No compression
	pos++

	// Size field (2 bytes, little-endian) - size of the update data
	binary.LittleEndian.PutUint16(buf[pos:pos+2], uint16(len(data)))
	pos += 2

	// Update data
	copy(buf[pos:], data)

	// Write directly to connection (no TPKT/X224 wrapper for Fast-Path)
	_, err := c.conn.Write(buf)
	return err
}
