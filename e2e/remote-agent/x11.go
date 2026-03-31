package main

import (
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
)

// X11Client is a minimal X11 protocol client that can create a window and blit pixels.
// It speaks raw X11 protocol over a Unix socket — no cgo, no external deps.
type X11Client struct {
	conn       net.Conn
	rootWindow uint32
	rootVisual uint32
	rootDepth  uint8
	idBase     uint32
	idMask     uint32
	idNext     uint32
	seqNum     uint16
}

// findXauthority finds the Xauthority file for the current user's session.
// It checks XAUTHORITY env, then looks for Mutter's Xwayland auth file,
// then falls back to ~/.Xauthority.
func findXauthority() string {
	if v := os.Getenv("XAUTHORITY"); v != "" {
		return v
	}
	// Look for Mutter's Xwayland auth file (GNOME on Wayland)
	uid := os.Getuid()
	pattern := fmt.Sprintf("/run/user/%d/.mutter-Xwaylandauth.*", uid)
	matches, _ := filepath.Glob(pattern)
	if len(matches) > 0 {
		return matches[0]
	}
	// Fallback to standard location
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".Xauthority")
	}
	return ""
}

// X11Connect connects to the X11 display server.
func X11Connect() (*X11Client, error) {
	display := os.Getenv("DISPLAY")
	if display == "" {
		display = ":0"
	}

	// Parse display number from ":N" or ":N.S"
	displayNum := "0"
	if idx := strings.IndexByte(display, ':'); idx >= 0 {
		rest := display[idx+1:]
		if dotIdx := strings.IndexByte(rest, '.'); dotIdx >= 0 {
			rest = rest[:dotIdx]
		}
		displayNum = rest
	}

	socketPath := fmt.Sprintf("/tmp/.X11-unix/X%s", displayNum)
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("connect to X11 socket %s: %w", socketPath, err)
	}

	c := &X11Client{conn: conn}
	if err := c.handshake(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("X11 handshake: %w", err)
	}
	return c, nil
}

func (c *X11Client) handshake() error {
	// Read auth cookie from Xauthority file
	authName, authData := readXauthCookie()

	// Client hello: byte-order 'l' (little-endian), protocol version 11.0
	authNamePad := (4 - (len(authName) % 4)) % 4
	authDataPad := (4 - (len(authData) % 4)) % 4
	helloLen := 12 + len(authName) + authNamePad + len(authData) + authDataPad

	hello := make([]byte, helloLen)
	hello[0] = 'l' // little-endian
	binary.LittleEndian.PutUint16(hello[2:], 11)                 // major version
	binary.LittleEndian.PutUint16(hello[4:], 0)                  // minor version
	binary.LittleEndian.PutUint16(hello[6:], uint16(len(authName))) // auth name length
	binary.LittleEndian.PutUint16(hello[8:], uint16(len(authData))) // auth data length
	copy(hello[12:], authName)
	copy(hello[12+len(authName)+authNamePad:], authData)

	if _, err := c.conn.Write(hello); err != nil {
		return err
	}

	// Read response header (8 bytes)
	hdr := make([]byte, 8)
	if _, err := readFull(c.conn, hdr); err != nil {
		return fmt.Errorf("read setup header: %w", err)
	}

	status := hdr[0]
	if status == 0 {
		// Failed — read reason
		reasonLen := hdr[1]
		additionalLen := binary.LittleEndian.Uint16(hdr[6:])
		extra := make([]byte, additionalLen*4)
		readFull(c.conn, extra)
		return fmt.Errorf("X11 connection refused: %s", string(extra[:reasonLen]))
	}
	if status == 2 {
		return fmt.Errorf("X11 requires authentication")
	}

	// Success (status == 1) — read the rest of the setup
	additionalLen := binary.LittleEndian.Uint16(hdr[6:])
	setup := make([]byte, additionalLen*4)
	if _, err := readFull(c.conn, setup); err != nil {
		return fmt.Errorf("read setup: %w", err)
	}

	// Parse setup info
	// Offsets in the setup response (after the 8-byte header):
	// 0-3: release number
	// 4-7: resource-id base
	// 8-11: resource-id mask
	// 12-15: motion buffer size
	// 16-17: vendor length
	// 18-19: max request length
	// 20: number of screens
	// 21: number of formats
	c.idBase = binary.LittleEndian.Uint32(setup[4:])
	c.idMask = binary.LittleEndian.Uint32(setup[8:])
	c.idNext = 0

	vendorLen := binary.LittleEndian.Uint16(setup[16:])
	numFormats := setup[21]

	// Skip past vendor string (padded to 4 bytes) and pixel formats (8 bytes each)
	vendorPad := (4 - (int(vendorLen) % 4)) % 4
	offset := 24 + int(vendorLen) + vendorPad + int(numFormats)*8 // note: fixed header is 24 bytes after the initial 8

	// We need to handle the offset calculation properly
	// The setup data starts at byte 0 of our 'setup' slice
	// Fixed fields: 32 bytes, then vendor (padded), then formats, then screens
	offset = 32 + int(vendorLen) + vendorPad + int(numFormats)*8

	// Now we're at the first screen
	// Screen structure:
	// 0-3: root window
	// 4-7: default colormap
	// 8-11: white pixel
	// 12-15: black pixel
	// 16-19: current input masks
	// 20-21: width in pixels
	// 22-23: height in pixels
	// 24-25: width in mm
	// 26-27: height in mm
	// 28-29: min installed maps
	// 30-31: max installed maps
	// 32-35: root visual
	// 36: backing stores
	// 37: save unders
	// 38: root depth
	// 39: number of depths
	if offset+40 > len(setup) {
		return fmt.Errorf("setup too short for screen info (offset=%d, len=%d)", offset, len(setup))
	}

	c.rootWindow = binary.LittleEndian.Uint32(setup[offset:])
	c.rootVisual = binary.LittleEndian.Uint32(setup[offset+32:])
	c.rootDepth = setup[offset+38]

	return nil
}

func (c *X11Client) nextID() uint32 {
	// Find the lowest bit in the mask to use as increment
	inc := c.idMask & (^c.idMask + 1) // lowest set bit
	id := c.idBase | (c.idNext & c.idMask)
	c.idNext += inc
	return id
}

// CreateWindow creates an override-redirect window at (x, y) with the given size.
func (c *X11Client) CreateWindow(x, y, width, height int) (uint32, error) {
	wid := c.nextID()

	// CreateWindow request: opcode 1
	// depth: copy from root
	// Values: override-redirect = true, event-mask = ExposureMask
	const (
		cwOverrideRedirect = 0x00000200
		cwEventMask        = 0x00000800
		cwBackPixel        = 0x00000002
		exposureMask       = 0x00008000
	)

	valueMask := uint32(cwBackPixel | cwOverrideRedirect | cwEventMask)
	numValues := 3 // back-pixel, override-redirect, event-mask
	reqLen := 8 + numValues // in 4-byte units

	buf := make([]byte, reqLen*4)
	buf[0] = 1                                                          // opcode: CreateWindow
	buf[1] = c.rootDepth                                                // depth
	binary.LittleEndian.PutUint16(buf[2:], uint16(reqLen))              // request length
	binary.LittleEndian.PutUint32(buf[4:], wid)                        // window id
	binary.LittleEndian.PutUint32(buf[8:], c.rootWindow)               // parent
	binary.LittleEndian.PutUint16(buf[12:], uint16(x))                 // x
	binary.LittleEndian.PutUint16(buf[14:], uint16(y))                 // y
	binary.LittleEndian.PutUint16(buf[16:], uint16(width))             // width
	binary.LittleEndian.PutUint16(buf[18:], uint16(height))            // height
	binary.LittleEndian.PutUint16(buf[20:], 0)                         // border width
	binary.LittleEndian.PutUint16(buf[22:], 1)                         // class: InputOutput
	binary.LittleEndian.PutUint32(buf[24:], c.rootVisual)              // visual
	binary.LittleEndian.PutUint32(buf[28:], valueMask)                 // value mask
	binary.LittleEndian.PutUint32(buf[32:], 0x00000000)                // back-pixel: black
	binary.LittleEndian.PutUint32(buf[36:], 1)                         // override-redirect: true
	binary.LittleEndian.PutUint32(buf[40:], exposureMask)              // event-mask

	if _, err := c.conn.Write(buf); err != nil {
		return 0, err
	}
	c.seqNum++
	return wid, nil
}

// MapWindow makes a window visible.
func (c *X11Client) MapWindow(wid uint32) error {
	buf := make([]byte, 8)
	buf[0] = 8 // opcode: MapWindow
	binary.LittleEndian.PutUint16(buf[2:], 2) // length
	binary.LittleEndian.PutUint32(buf[4:], wid)
	_, err := c.conn.Write(buf)
	c.seqNum++
	return err
}

// PutImage blits BGRA pixel data to a window using ZPixmap format.
func (c *X11Client) PutImage(wid uint32, gcid uint32, width, height int, data []byte) error {
	// PutImage request: opcode 72
	dataLen := len(data)
	reqLen := 6 + (dataLen+3)/4 // header is 6 uint32s, then pixel data padded to 4 bytes

	buf := make([]byte, reqLen*4)
	buf[0] = 72                                                    // opcode: PutImage
	buf[1] = 2                                                     // format: ZPixmap
	binary.LittleEndian.PutUint16(buf[2:], uint16(reqLen))         // request length
	binary.LittleEndian.PutUint32(buf[4:], wid)                    // drawable
	binary.LittleEndian.PutUint32(buf[8:], gcid)                   // gc
	binary.LittleEndian.PutUint16(buf[12:], uint16(width))         // width
	binary.LittleEndian.PutUint16(buf[14:], uint16(height))        // height
	binary.LittleEndian.PutUint16(buf[16:], 0)                     // dst-x
	binary.LittleEndian.PutUint16(buf[18:], 0)                     // dst-y
	buf[20] = 0                                                    // left-pad
	buf[21] = c.rootDepth                                          // depth
	copy(buf[24:], data)

	_, err := c.conn.Write(buf)
	c.seqNum++
	return err
}

// CreateGC creates a minimal graphics context.
func (c *X11Client) CreateGC(drawable uint32) (uint32, error) {
	gcid := c.nextID()

	buf := make([]byte, 16)
	buf[0] = 55 // opcode: CreateGC
	binary.LittleEndian.PutUint16(buf[2:], 4)    // length
	binary.LittleEndian.PutUint32(buf[4:], gcid)  // gc id
	binary.LittleEndian.PutUint32(buf[8:], drawable) // drawable
	// value-mask = 0 (no values)

	_, err := c.conn.Write(buf)
	c.seqNum++
	return gcid, err
}

// DestroyWindow destroys a window.
func (c *X11Client) DestroyWindow(wid uint32) error {
	buf := make([]byte, 8)
	buf[0] = 4 // opcode: DestroyWindow
	binary.LittleEndian.PutUint16(buf[2:], 2)
	binary.LittleEndian.PutUint32(buf[4:], wid)
	_, err := c.conn.Write(buf)
	c.seqNum++
	return err
}

// Close closes the X11 connection.
func (c *X11Client) Close() error {
	return c.conn.Close()
}

// readXauthCookie reads the MIT-MAGIC-COOKIE-1 from the Xauthority file.
// Returns (authName, authData) or empty slices if not found.
func readXauthCookie() ([]byte, []byte) {
	path := findXauthority()
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil
	}

	// Xauthority file format: sequence of entries, each:
	//   2 bytes: family (big-endian)
	//   2 bytes: address length, then address
	//   2 bytes: display number length, then display number
	//   2 bytes: auth name length, then auth name
	//   2 bytes: auth data length, then auth data
	offset := 0
	for offset+2 <= len(data) {
		// family
		if offset+2 > len(data) {
			break
		}
		offset += 2 // skip family

		// address
		if offset+2 > len(data) {
			break
		}
		addrLen := int(binary.BigEndian.Uint16(data[offset:]))
		offset += 2 + addrLen

		// display number
		if offset+2 > len(data) {
			break
		}
		dispLen := int(binary.BigEndian.Uint16(data[offset:]))
		offset += 2 + dispLen

		// auth name
		if offset+2 > len(data) {
			break
		}
		nameLen := int(binary.BigEndian.Uint16(data[offset:]))
		offset += 2
		if offset+nameLen > len(data) {
			break
		}
		name := data[offset : offset+nameLen]
		offset += nameLen

		// auth data
		if offset+2 > len(data) {
			break
		}
		dataLen := int(binary.BigEndian.Uint16(data[offset:]))
		offset += 2
		if offset+dataLen > len(data) {
			break
		}
		authData := data[offset : offset+dataLen]
		offset += dataLen

		if string(name) == "MIT-MAGIC-COOKIE-1" {
			return name, authData
		}
	}
	return nil, nil
}

func readFull(conn net.Conn, buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := conn.Read(buf[total:])
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}
