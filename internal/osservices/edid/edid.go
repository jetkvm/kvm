// SPDX-FileCopyrightText: 2026
// SPDX-License-Identifier: MIT

// Package edid builds the EDID of the OS Services guest display: a VESA
// E-EDID 1.3 base block followed by one CTA-861 extension block (revision 3).
//
// This package is a port of the guest display EDID builder of the JetKVM Mini
// firmware (components/jetkvm_os_services/os_services_edid.c, MIT license).
// For the same mode and serial number it returns the same bytes.
//
// The sender's computer sees this display on the KVM's HDMI port while OS
// Services video replaces its own output. Nothing samples the HDMI signal in
// that state; the EDID only has to make the computer's OS create a display at
// the requested size. Every OS adopts the preferred timing of a monitor it has
// not seen, so the requested mode is the first detailed timing. The serial
// number decides whether the OS knows the display again (see StableSerial and
// RandomSerial). The other modes stay listed so the user can still pick one.
//
// The byte layout follows VESA E-EDID Release A, Rev. 1 (EDID 1.3) and
// CTA-861-G (extension block, video and vendor-specific data blocks).
package edid

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Size is the length in bytes of a guest display EDID: the base block and one
// CTA-861 extension block.
const Size = 2 * blockLen

const (
	blockLen      = 128
	descriptorLen = 18
	textLen       = 13
)

// Base block offsets (EDID 1.3 section 3).
const (
	baseVendor          = 8
	baseProduct         = 10
	baseSerial          = 12
	baseWeek            = 16
	baseYear            = 17
	baseVersion         = 18
	baseInput           = 20
	baseSizeCM          = 21
	baseGamma           = 23
	baseFeatures        = 24
	baseChroma          = 25
	baseStandardTimings = 38
	baseDescriptors     = 54
	baseExtensions      = 126
)

const (
	// vendorID is "JET" as a packed three-letter PNP ID. The sender finds the
	// KVM's display by these bytes on every OS.
	vendorID = 0x28B4
	// The display is manufactured in week 36 of 2026; the year counts from
	// 1990.
	manufactureWeek = 36
	manufactureYear = 2026 - 1990
	// inputDigital declares a digital input. EDID 1.3 has no color depth or
	// interface fields.
	inputDigital = 0x80
	// gamma22 is (gamma x 100) - 100 for gamma 2.2.
	gamma22 = 120
	// features declares DPMS suspend (bit 6), display type 2 (bits 4:3), sRGB
	// as the default color space (bit 2) and the preferred timing in the
	// first detailed descriptor (bit 1). It declares no GTF, so no OS derives
	// extra modes from the range limits.
	features = 0x56
)

// Display descriptor tags (EDID 1.3 section 3.10.3).
const (
	tagSerialText  = 0xFF
	tagRangeLimits = 0xFD
	tagProductName = 0xFC
)

// pixelsPerInch sets the physical size of each mode. At about 110 pixels per
// inch, Windows suggests 100% scaling and macOS keeps a 1:1 desktop up to
// 1440p. At 2160p macOS picks a 2x desktop whatever the physical size says.
const pixelsPerInch = 110

// CTA-861 extension block and data block tags (CTA-861-G table 54).
const (
	ctaTag           = 0x02
	ctaRevision      = 3
	ctaHeaderLen     = 4
	ctaVideoBlock    = 2
	ctaVendorBlock   = 3
	ctaExtendedBlock = 7
	ctaYCbCr420Video = 0x0E
	ctaNativeSVD     = 0x80
)

type timing struct {
	pixelClock10kHz uint16
	hActive         uint16
	hBlank          uint16
	hFront          uint16
	hSync           uint16
	vActive         uint16
	vBlank          uint16
	vFront          uint16
	vSync           uint16
	// product is a product code of its own per mode, so settings that an OS
	// keeps per monitor model (Windows display scaling) stay with their mode.
	product uint16
	// vic is the CTA-861 Video Identification Code, or 0 for a mode that
	// CTA-861 does not define.
	vic uint8
	// ycbcr420Only offers the mode only in the YCbCr 4:2:0 Video Data Block:
	// the RGB mode needs a 594 MHz HDMI link.
	ycbcr420Only bool
}

// timings are the modes the guest display can advertise. Nothing samples HDMI
// while the guest display is presented, so the modes can exceed what the KVM
// captures.
var timings = [...]timing{
	// CTA-861 VIC 4: 74.25 MHz, 1650 x 750 total.
	{
		pixelClock10kHz: 7425,
		hActive:         1280, hBlank: 370, hFront: 110, hSync: 40,
		vActive: 720, vBlank: 30, vFront: 5, vSync: 5,
		product: 0x4011,
		vic:     4,
	},
	// CTA-861 VIC 16: 148.5 MHz, 2200 x 1125 total.
	{
		pixelClock10kHz: 14850,
		hActive:         1920, hBlank: 280, hFront: 88, hSync: 44,
		vActive: 1080, vBlank: 45, vFront: 4, vSync: 5,
		product: 0x4012,
		vic:     16,
	},
	// VESA CVT reduced blanking: 241.5 MHz, 2720 x 1481 total.
	{
		pixelClock10kHz: 24150,
		hActive:         2560, hBlank: 160, hFront: 48, hSync: 32,
		vActive: 1440, vBlank: 41, vFront: 3, vSync: 5,
		product: 0x4013,
		vic:     0,
	},
	// CTA-861 VIC 97: 594 MHz, 4400 x 2250 total.
	{
		pixelClock10kHz: 59400,
		hActive:         3840, hBlank: 560, hFront: 176, hSync: 88,
		vActive: 2160, vBlank: 90, vFront: 8, vSync: 10,
		product:      0x4014,
		vic:          97,
		ycbcr420Only: true,
	},
}

// chromaticity holds the sRGB primaries and D65 white point in 1/10000 CIE
// units (IEC 61966-2-1), in the order EDID stores them: red x, y, green x, y,
// blue x, y, white x, y.
var chromaticity = [8]uint16{6400, 3300, 3000, 6000, 1500, 600, 3127, 3290}

// hdmiVSDBPayload is the HDMI Vendor-Specific Data Block: IEEE OUI 00-0C-03,
// source physical address 1.0.0.0, no optional features, and Max_TMDS_Clock
// 600 MHz in 5 MHz units. Without it Linux treats the sink as DVI and drops
// every mode above 165 MHz, the 1440p one among them.
var hdmiVSDBPayload = [...]byte{0x03, 0x0C, 0x00, 0x10, 0x00, 0x00, 600 / 5}

// ErrUnsupportedSize is the error Build returns for a size that the guest
// display does not offer.
var ErrUnsupportedSize = errors.New("unsupported guest display size")

// Supported reports whether the guest display offers width x height, that is
// whether Build accepts the size.
func Supported(width, height int) bool {
	return timingIndex(width, height) >= 0
}

func timingIndex(width, height int) int {
	for i, t := range timings {
		if int(t.hActive) == width && int(t.vActive) == height {
			return i
		}
	}
	return -1
}

// Build returns the guest display EDID, Size bytes, with width x height at
// 60 Hz as the preferred timing. The vendor ID is 0x28B4 ("JET"), which the
// sender uses to find the KVM display. The serial number decides whether the
// guest OS knows the display again (the same serial) or takes it for a new
// monitor (a new serial). For a size that Supported rejects, Build returns an
// error that wraps ErrUnsupportedSize.
func Build(width, height int, serial uint32) ([]byte, error) {
	preferred := timingIndex(width, height)
	if preferred < 0 {
		return nil, fmt.Errorf("%w: %dx%d", ErrUnsupportedSize, width, height)
	}
	edid := make([]byte, Size)
	buildBase(edid[:blockLen], &timings[preferred], serial)
	buildCTA(edid[blockLen:], preferred)
	return edid, nil
}

// StableSerial returns the serial number for a sender that sets the display's
// mode: the first four bytes of the KVM's public key, read little-endian, with
// the height in the lowest 12 bits. It is the same at every presentation, so
// the target knows one monitor per KVM and size again, and the sender gives it
// the settings' size. The height is in it because some desktops tell monitors
// apart by name and serial only.
//
// For every height that Supported accepts, the serial is not zero, which an
// EDID uses for "no serial". StableSerial panics if publicKey is shorter than
// four bytes.
func StableSerial(publicKey []byte, height int) uint32 {
	return binary.LittleEndian.Uint32(publicKey)&^0xFFF | uint32(height)&0xFFF
}

// RandomSerial returns a serial number for any other sender: a value from
// random (crypto/rand.Reader in production) that is not zero, which an EDID
// uses for "no serial". A new serial at every presentation shows the target a
// monitor it has not seen. The target then adopts the preferred timing, which
// is the settings' size, and keeps a mode that the user picked for a monitor
// that does not return.
func RandomSerial(random io.Reader) (uint32, error) {
	var b [4]byte
	for {
		if _, err := io.ReadFull(random, b[:]); err != nil {
			return 0, fmt.Errorf("read random EDID serial: %w", err)
		}
		if serial := binary.LittleEndian.Uint32(b[:]); serial != 0 {
			return serial, nil
		}
	}
}

// Hex returns the EDID in the form that the KVM's EDID path takes
// (config.EdidString, native.VideoSetEDID): uppercase hex digits without
// separators.
func Hex(edid []byte) string {
	return strings.ToUpper(hex.EncodeToString(edid))
}

func blockChecksum(block []byte) byte {
	var sum byte
	for _, b := range block[:blockLen-1] {
		sum += b
	}
	return -sum
}

// sizeMM returns pixels / pixelsPerInch * 25.4 mm, rounded to the nearest
// millimeter.
func sizeMM(pixels uint16) uint16 {
	const divisor = pixelsPerInch * 10
	return uint16((uint32(pixels)*254 + divisor/2) / divisor)
}

// chroma10Bit returns a chromaticity coordinate as EDID's 10-bit binary
// fraction.
func chroma10Bit(value10000 uint16) uint16 {
	return uint16((uint32(value10000)*1024 + 5000) / 10000)
}

func putChromaticity(out []byte) {
	var bits [8]uint16
	for i, v := range chromaticity {
		bits[i] = chroma10Bit(v)
	}
	// Two bytes of low-order bit pairs, then the high eight bits of each
	// coordinate.
	out[0] = byte((bits[0]&3)<<6 | (bits[1]&3)<<4 | (bits[2]&3)<<2 | bits[3]&3)
	out[1] = byte((bits[4]&3)<<6 | (bits[5]&3)<<4 | (bits[6]&3)<<2 | bits[7]&3)
	for i, b := range bits {
		out[2+i] = byte(b >> 2)
	}
}

// putDetailedTiming writes a detailed timing descriptor into d, which is
// zero.
func putDetailedTiming(d []byte, t *timing, widthMM, heightMM uint16) {
	d[0] = byte(t.pixelClock10kHz)
	d[1] = byte(t.pixelClock10kHz >> 8)
	d[2] = byte(t.hActive)
	d[3] = byte(t.hBlank)
	d[4] = byte((t.hActive>>8)<<4 | t.hBlank>>8)
	d[5] = byte(t.vActive)
	d[6] = byte(t.vBlank)
	d[7] = byte((t.vActive>>8)<<4 | t.vBlank>>8)
	d[8] = byte(t.hFront)
	d[9] = byte(t.hSync)
	d[10] = byte((t.vFront&0x0F)<<4 | t.vSync&0x0F)
	d[11] = byte((t.hFront>>8)<<6 | (t.hSync>>8)<<4 | (t.vFront>>4)<<2 | t.vSync>>4)
	d[12] = byte(widthMM)
	d[13] = byte(heightMM)
	d[14] = byte((widthMM>>8)<<4 | heightMM>>8)
	// Bytes 15-16: no border. Byte 17: progressive, no stereo, digital
	// separate sync (bits 4:3), positive vertical and horizontal sync.
	d[17] = 0x18 | 0x04 | 0x02
}

// putText writes a text descriptor into d, which is zero: up to 13
// characters, then a line feed and spaces.
func putText(d []byte, tag byte, text string) {
	d[3] = tag
	n := copy(d[5:5+textLen], text)
	for i := 5 + n; i < descriptorLen; i++ {
		if i == 5+n {
			d[i] = 0x0A
		} else {
			d[i] = 0x20
		}
	}
}

// putRangeLimits writes the display range limits descriptor into d, which is
// zero.
func putRangeLimits(d []byte) {
	d[3] = tagRangeLimits
	d[5] = 23       // minimum vertical rate, Hz
	d[6] = 120      // maximum vertical rate, Hz
	d[7] = 30       // minimum horizontal rate, kHz
	d[8] = 140      // maximum horizontal rate, kHz
	d[9] = 600 / 10 // maximum pixel clock, 10 MHz units
	// Byte 10 = 0: no secondary timing formula. The rest is padding.
	d[11] = 0x0A
	for i := 12; i < descriptorLen; i++ {
		d[i] = 0x20
	}
}

// buildBase writes the EDID base block into base, which is zero.
func buildBase(base []byte, t *timing, serial uint32) {
	widthMM := sizeMM(t.hActive)
	heightMM := sizeMM(t.vActive)

	copy(base, []byte{0x00, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0x00})
	binary.BigEndian.PutUint16(base[baseVendor:], vendorID)
	binary.LittleEndian.PutUint16(base[baseProduct:], t.product)
	binary.LittleEndian.PutUint32(base[baseSerial:], serial)
	base[baseWeek] = manufactureWeek
	base[baseYear] = manufactureYear
	// 1.3, not 1.4: a Windows GPU driver has been seen refusing the same
	// modes above 165 MHz from a 1.4 EDID.
	base[baseVersion] = 1
	base[baseVersion+1] = 3
	base[baseInput] = inputDigital
	base[baseSizeCM] = byte(widthMM / 10)
	base[baseSizeCM+1] = byte(heightMM / 10)
	base[baseGamma] = gamma22
	base[baseFeatures] = features
	putChromaticity(base[baseChroma:])
	// Bytes 35-37: no established timings. Then eight unused standard timing
	// slots.
	for i := baseStandardTimings; i < baseStandardTimings+16; i++ {
		base[i] = 0x01
	}

	descriptor := func(n int) []byte {
		start := baseDescriptors + n*descriptorLen
		return base[start : start+descriptorLen]
	}
	putDetailedTiming(descriptor(0), t, widthMM, heightMM)
	putText(descriptor(1), tagProductName, "JetKVM Guest")
	putRangeLimits(descriptor(2))
	// The serial number as text too, for OSes that show it.
	putText(descriptor(3), tagSerialText, fmt.Sprintf("JKVG%08X", serial))

	base[baseExtensions] = 1
	base[blockLen-1] = blockChecksum(base)
}

// buildCTA writes the CTA-861 extension block into cta, which is zero, for
// the preferred mode timings[preferred].
func buildCTA(cta []byte, preferred int) {
	cta[0] = ctaTag
	cta[1] = ctaRevision
	// Byte 3 = 0: no underscan, audio or YCbCr support declared, and no
	// native detailed timings counted.
	offset := ctaHeaderLen

	// Video Data Block: every RGB mode with a VIC, the preferred one marked
	// native.
	videoBlock := offset
	offset++
	for i, t := range timings {
		if t.vic == 0 || t.ycbcr420Only {
			continue
		}
		svd := t.vic
		if i == preferred {
			svd |= ctaNativeSVD
		}
		cta[offset] = svd
		offset++
	}
	cta[videoBlock] = byte(ctaVideoBlock<<5 | (offset - videoBlock - 1))

	// YCbCr 4:2:0 Video Data Block: the modes offered only as 4:2:0.
	ycbcr420Block := offset
	offset++
	cta[offset] = ctaYCbCr420Video
	offset++
	for _, t := range timings {
		if t.ycbcr420Only {
			cta[offset] = t.vic
			offset++
		}
	}
	cta[ycbcr420Block] = byte(ctaExtendedBlock<<5 | (offset - ycbcr420Block - 1))

	cta[offset] = byte(ctaVendorBlock<<5 | len(hdmiVSDBPayload))
	offset++
	offset += copy(cta[offset:], hdmiVSDBPayload[:])

	// Detailed timings start after the data blocks: the modes without a VIC,
	// except the preferred one, which the base block carries.
	cta[2] = byte(offset)
	widthMM := sizeMM(timings[preferred].hActive)
	heightMM := sizeMM(timings[preferred].vActive)
	for i := range timings {
		if timings[i].vic != 0 || i == preferred {
			continue
		}
		// The image size is the panel's, the same for every timing.
		putDetailedTiming(cta[offset:offset+descriptorLen], &timings[i], widthMM, heightMM)
		offset += descriptorLen
	}
	cta[blockLen-1] = blockChecksum(cta)
}
