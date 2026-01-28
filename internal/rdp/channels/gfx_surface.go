package channels

import "encoding/binary"

// RDPGFX surface management operations.
// This file contains functions for creating, deleting, and managing GFX surfaces.

// Initialize creates the surface and maps it to output.
func (g *GFXChannel) Initialize(width, height uint16) error {
	if !g.ready.Load() {
		return ErrGFXNotReady
	}

	g.sendMu.Lock()
	defer g.sendMu.Unlock()

	g.width = width
	g.height = height

	// Send reset graphics
	if err := g.sendResetGraphics(width, height); err != nil {
		return err
	}

	// Create surface
	if err := g.sendCreateSurface(g.surfaceID, width, height); err != nil {
		return err
	}

	// Map surface to output
	if err := g.sendMapSurfaceToOutput(g.surfaceID, 0, 0); err != nil {
		return err
	}

	// Mark as fully initialized - surface is now ready to receive frames
	// This must be set AFTER all surface commands are sent to avoid race conditions
	// where frames arrive before the client has processed CreateSurface/MapSurfaceToOutput
	g.initialized.Store(true)

	return nil
}

// sendResetGraphics sends a reset graphics command.
// NOTE: FreeRDP requires the total ResetGraphics PDU to be exactly 340 bytes.
// This is RDPGFX_RESET_GRAPHICS_PDU_SIZE in FreeRDP code.
func (g *GFXChannel) sendResetGraphics(width, height uint16) error {
	// ResetGraphics PDU format (MS-RDPEGFX 2.2.2.4):
	// - Header: cmdId(2) + flags(2) + pduLength(4) = 8 bytes
	// - Body: width(4) + height(4) + monitorCount(4) + monitors(20*n) + padding
	// - Total PDU must be exactly 340 bytes (FreeRDP RDPGFX_RESET_GRAPHICS_PDU_SIZE)
	//
	// pduLength in header = total PDU size = 340
	const totalPDUSize = 340       // RDPGFX_RESET_GRAPHICS_PDU_SIZE
	const pduLength = totalPDUSize // pduLength = total size including header

	buf := make([]byte, totalPDUSize)

	// Header - pduLength is the total PDU size (340)
	binary.LittleEndian.PutUint16(buf[0:2], GFXCmdResetGraphics)
	binary.LittleEndian.PutUint16(buf[2:4], 0)
	binary.LittleEndian.PutUint32(buf[4:8], pduLength) // 340

	// Width, height
	binary.LittleEndian.PutUint32(buf[8:12], uint32(width))
	binary.LittleEndian.PutUint32(buf[12:16], uint32(height))

	// Monitor count = 1
	binary.LittleEndian.PutUint32(buf[16:20], 1)

	// Monitor definition (20 bytes per MS-RDPEGFX 2.2.2.4.1)
	// left(4) + top(4) + right(4) + bottom(4) + flags(4)
	binary.LittleEndian.PutUint32(buf[20:24], 0)              // left
	binary.LittleEndian.PutUint32(buf[24:28], 0)              // top
	binary.LittleEndian.PutUint32(buf[28:32], uint32(width))  // right
	binary.LittleEndian.PutUint32(buf[32:36], uint32(height)) // bottom
	binary.LittleEndian.PutUint32(buf[36:40], 1)              // flags (primary)

	// Remaining bytes are zero-padding (already zeroed by make)

	return g.sendGFXData(buf)
}

// sendCreateSurface sends a create surface command.
func (g *GFXChannel) sendCreateSurface(surfaceID, width, height uint16) error {
	buf := g.createBuf[:]

	// Header
	binary.LittleEndian.PutUint16(buf[0:2], GFXCmdCreateSurface)
	binary.LittleEndian.PutUint16(buf[2:4], 0)
	binary.LittleEndian.PutUint32(buf[4:8], GFXHeaderSize+GFXCreateSurfaceSize)

	// Surface
	binary.LittleEndian.PutUint16(buf[8:10], surfaceID)
	binary.LittleEndian.PutUint16(buf[10:12], width)
	binary.LittleEndian.PutUint16(buf[12:14], height)
	buf[14] = GFXPixelFormatXRGB

	return g.sendGFXData(buf)
}

// sendMapSurfaceToOutput sends a map surface to output command.
func (g *GFXChannel) sendMapSurfaceToOutput(surfaceID uint16, x, y uint32) error {
	buf := g.mapBuf[:]

	// Header
	binary.LittleEndian.PutUint16(buf[0:2], GFXCmdMapSurfaceToOutput)
	binary.LittleEndian.PutUint16(buf[2:4], 0)
	binary.LittleEndian.PutUint32(buf[4:8], GFXHeaderSize+GFXMapSurfaceSize)

	// Mapping - coordinates are 4 bytes each per MS-RDPEGFX
	binary.LittleEndian.PutUint16(buf[8:10], surfaceID)
	binary.LittleEndian.PutUint16(buf[10:12], 0) // reserved
	binary.LittleEndian.PutUint32(buf[12:16], x) // outputOriginX (4 bytes)
	binary.LittleEndian.PutUint32(buf[16:20], y) // outputOriginY (4 bytes)

	return g.sendGFXData(buf)
}

// UpdateResolution handles resolution change.
func (g *GFXChannel) UpdateResolution(width, height uint16) error {
	if !g.ready.Load() {
		return ErrGFXNotReady
	}

	if width == g.width && height == g.height {
		return nil
	}

	g.sendMu.Lock()
	defer g.sendMu.Unlock()

	// Clear initialized flag to prevent frames from being sent during surface recreation
	g.initialized.Store(false)

	// Delete old surface
	if err := g.sendDeleteSurface(g.surfaceID); err != nil {
		return err
	}

	g.width = width
	g.height = height

	// Create new surface
	if err := g.sendCreateSurface(g.surfaceID, width, height); err != nil {
		return err
	}

	// Remap
	if err := g.sendMapSurfaceToOutput(g.surfaceID, 0, 0); err != nil {
		return err
	}

	// Mark as initialized again - surface is ready to receive frames
	g.initialized.Store(true)

	return nil
}

// sendDeleteSurface sends a delete surface command.
func (g *GFXChannel) sendDeleteSurface(surfaceID uint16) error {
	buf := g.deleteBuf[:]

	// Header
	binary.LittleEndian.PutUint16(buf[0:2], GFXCmdDeleteSurface)
	binary.LittleEndian.PutUint16(buf[2:4], 0)
	binary.LittleEndian.PutUint32(buf[4:8], GFXHeaderSize+GFXDeleteSurfaceSize)

	// Surface ID
	binary.LittleEndian.PutUint16(buf[8:10], surfaceID)

	return g.sendGFXData(buf)
}
