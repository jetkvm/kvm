// SPDX-FileCopyrightText: 2026
// SPDX-License-Identifier: MIT

// The guest display EDID against the bytes of the JetKVM Mini's C builder for
// the same mode and serial (testdata/guest_edids.txt), and against the EDID
// and CTA-861 rules read back independently of the builder: block checksums,
// the vendor ID and serial number, the preferred timing, and the full set of
// modes offered. The assertions follow the Mini's
// tests/test_os_services_edid.c.
//
// 3840x2160 is CTA-861 VIC 97 (2160p60) and is offered only in a YCbCr 4:2:0
// Video Data Block, which keeps the HDMI TMDS rate at 297 MHz without
// scrambling.

package edid_test

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/jetkvm/kvm/internal/osservices/edid"
)

const block = 128

type guestMode struct {
	width         int
	height        int
	pixelClockKHz uint32
	hTotal        int
	vTotal        int
	vic           byte // 0: not a CTA-861 mode
	ycbcr420Only  bool
}

var guestModes = []guestMode{
	{1280, 720, 74250, 1650, 750, 4, false},
	{1920, 1080, 148500, 2200, 1125, 16, false},
	{2560, 1440, 241500, 2720, 1481, 0, false},
	{3840, 2160, 594000, 4400, 2250, 97, true},
}

func (m guestMode) String() string { return fmt.Sprintf("%dx%d", m.width, m.height) }

func build(t *testing.T, width, height int, serial uint32) []byte {
	t.Helper()
	got, err := edid.Build(width, height, serial)
	if err != nil {
		t.Fatalf("Build(%d, %d, %#08x): %v", width, height, serial, err)
	}
	if len(got) != edid.Size {
		t.Fatalf("Build(%d, %d, %#08x) returned %d bytes, want %d", width, height, serial, len(got), edid.Size)
	}
	return got
}

type goldenEDID struct {
	width, height int
	kind          string
	serial        uint32
	hex           string
}

// readGolden parses testdata/guest_edids.txt.
func readGolden(t *testing.T) (key []byte, golden []goldenEDID) {
	t.Helper()
	f, err := os.Open("testdata/guest_edids.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for line := 1; scanner.Scan(); line++ {
		text := scanner.Text()
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		fields := strings.Fields(text)
		if fields[0] == "key" && len(fields) == 2 {
			if key, err = hex.DecodeString(fields[1]); err != nil {
				t.Fatalf("line %d: %v", line, err)
			}
			continue
		}
		if len(fields) != 4 {
			t.Fatalf("line %d: %d fields, want 4", line, len(fields))
		}
		var g goldenEDID
		if _, err := fmt.Sscanf(fields[0], "%dx%d", &g.width, &g.height); err != nil {
			t.Fatalf("line %d: size %q: %v", line, fields[0], err)
		}
		g.kind = fields[1]
		serial, err := strconv.ParseUint(fields[2], 16, 32)
		if err != nil {
			t.Fatalf("line %d: serial %q: %v", line, fields[2], err)
		}
		g.serial = uint32(serial)
		g.hex = fields[3]
		golden = append(golden, g)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if key == nil {
		t.Fatal("no key line")
	}
	return key, golden
}

func TestBuildMatchesTheMiniBuilder(t *testing.T) {
	key, golden := readGolden(t)

	covered := map[string]bool{}
	for _, g := range golden {
		name := fmt.Sprintf("%dx%d/%s/%08X", g.width, g.height, g.kind, g.serial)
		covered[fmt.Sprintf("%dx%d/%s", g.width, g.height, g.kind)] = true
		t.Run(name, func(t *testing.T) {
			if g.kind == "stable" {
				if got := edid.StableSerial(key, g.height); got != g.serial {
					t.Errorf("StableSerial(key, %d) = %08X, want %08X", g.height, got, g.serial)
				}
			}
			want, err := hex.DecodeString(g.hex)
			if err != nil {
				t.Fatal(err)
			}
			got := build(t, g.width, g.height, g.serial)
			for i := range want {
				if got[i] != want[i] {
					t.Fatalf("byte %d is 0x%02X, want 0x%02X", i, got[i], want[i])
				}
			}
			if h := edid.Hex(got); h != g.hex {
				t.Errorf("Hex() = %s, want %s", h, g.hex)
			}
		})
	}
	for _, m := range guestModes {
		for _, kind := range []string{"random", "stable"} {
			if !covered[fmt.Sprintf("%s/%s", m, kind)] {
				t.Errorf("testdata has no %s EDID for %s", kind, m)
			}
		}
	}
}

func blockSum(b []byte) byte {
	var sum byte
	for _, v := range b[:block] {
		sum += v
	}
	return sum
}

func TestChecksumsVendorAndHeader(t *testing.T) {
	header := []byte{0x00, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0x00}
	for _, m := range guestModes {
		for _, serial := range []uint32{1, 0x80000000, 0xFFFFFFFF, 0x0BADF00D} {
			t.Run(fmt.Sprintf("%s/%08X", m, serial), func(t *testing.T) {
				e := build(t, m.width, m.height, serial)
				if !bytes.Equal(e[:8], header) {
					t.Errorf("header % X", e[:8])
				}
				if s := blockSum(e); s != 0 {
					t.Errorf("base block sums to 0x%02X", s)
				}
				if s := blockSum(e[block:]); s != 0 {
					t.Errorf("extension block sums to 0x%02X", s)
				}
				// One extension, and it is CTA-861 revision 3.
				if e[126] != 1 || e[block] != 0x02 || e[block+1] != 3 {
					t.Errorf("extensions %d, tag 0x%02X, revision %d", e[126], e[block], e[block+1])
				}
				// EDID 1.3, digital input.
				if e[18] != 1 || e[19] != 3 || e[20]&0x80 == 0 {
					t.Errorf("version %d.%d, input 0x%02X", e[18], e[19], e[20])
				}
				// Vendor 0x28B4, big-endian, is "JET" in 5-bit letters.
				id := uint16(e[8])<<8 | uint16(e[9])
				vendor := string([]byte{
					byte('A' - 1 + (id>>10)&0x1F),
					byte('A' - 1 + (id>>5)&0x1F),
					byte('A' - 1 + id&0x1F),
				})
				if id != 0x28B4 || vendor != "JET" {
					t.Errorf("vendor 0x%04X %q, want 0x28B4 \"JET\"", id, vendor)
				}
			})
		}
	}
}

// findDescriptor returns the offset of the display descriptor with tag in the
// base block, or -1.
func findDescriptor(e []byte, tag byte) int {
	for offset := 54; offset < 126; offset += 18 {
		d := e[offset:]
		if d[0] == 0 && d[1] == 0 && d[2] == 0 && d[3] == tag {
			return offset
		}
	}
	return -1
}

func TestSerialPlacement(t *testing.T) {
	a := build(t, 1920, 1080, 0xC0FFEE42)
	b := build(t, 1920, 1080, 0x00000007)

	// Little-endian at bytes 12-15.
	if !bytes.Equal(a[12:16], []byte{0x42, 0xEE, 0xFF, 0xC0}) {
		t.Errorf("serial bytes % X, want 42 EE FF C0", a[12:16])
	}
	if !bytes.Equal(b[12:16], []byte{0x07, 0x00, 0x00, 0x00}) {
		t.Errorf("serial bytes % X, want 07 00 00 00", b[12:16])
	}

	// And as text in the serial number descriptor.
	for _, c := range []struct {
		e    []byte
		want string
	}{{a, "JKVGC0FFEE42\n"}, {b, "JKVG00000007\n"}} {
		offset := findDescriptor(c.e, 0xFF)
		if offset < 0 {
			t.Fatal("no serial number descriptor")
		}
		if got := string(c.e[offset+5 : offset+18]); got != c.want {
			t.Errorf("serial text %q, want %q", got, c.want)
		}
	}

	// Nothing else depends on the serial: the two differ only in those bytes
	// and the base block checksum.
	textStart := findDescriptor(b, 0xFF) + 5
	for i := range a {
		serialByte := (i >= 12 && i <= 15) ||
			(i >= textStart+4 && i < textStart+12) || i == 127
		if !serialByte && a[i] != b[i] {
			t.Errorf("byte %d depends on the serial", i)
		}
	}

	// The name descriptor.
	offset := findDescriptor(a, 0xFC)
	if offset < 0 {
		t.Fatal("no product name descriptor")
	}
	if got := string(a[offset+5 : offset+18]); got != "JetKVM Guest\n" {
		t.Errorf("product name %q", got)
	}
}

type decodedTiming struct {
	width, height int
	pixelClockKHz uint32
	hTotal        int
	vTotal        int
}

func decodeDTD(d []byte) decodedTiming {
	var t decodedTiming
	t.pixelClockKHz = (uint32(d[0]) | uint32(d[1])<<8) * 10
	t.width = int(d[2]) | int(d[4]&0xF0)<<4
	t.hTotal = t.width + (int(d[3]) | int(d[4]&0x0F)<<8)
	t.height = int(d[5]) | int(d[7]&0xF0)<<4
	t.vTotal = t.height + (int(d[6]) | int(d[7]&0x0F)<<8)
	return t
}

func (t decodedTiming) is(m guestMode) bool {
	return t.width == m.width && t.height == m.height &&
		t.pixelClockKHz == m.pixelClockKHz && t.hTotal == m.hTotal && t.vTotal == m.vTotal
}

func TestPreferredTimingAndModeSet(t *testing.T) {
	for mi, mode := range guestModes {
		t.Run(mode.String(), func(t *testing.T) {
			e := build(t, mode.width, mode.height, 0x13572468)

			// The feature byte says the first descriptor is the preferred
			// timing, and that descriptor is the requested mode at 60 Hz.
			if e[24]&0x02 == 0 {
				t.Error("feature byte does not declare a preferred timing")
			}
			if e[24]&0x01 != 0 {
				t.Error("feature byte declares GTF-derived modes")
			}
			if e[54] == 0 && e[55] == 0 {
				t.Fatal("first descriptor is not a detailed timing")
			}
			preferred := decodeDTD(e[54:])
			if !preferred.is(mode) {
				t.Fatalf("preferred timing %+v, want %+v", preferred, mode)
			}
			millihertz := uint64(preferred.pixelClockKHz) * 1000000 /
				(uint64(preferred.hTotal) * uint64(preferred.vTotal))
			if millihertz < 59900 || millihertz > 60100 {
				t.Errorf("refresh %d mHz, want 60 Hz", millihertz)
			}
			// Positive sync, digital separate.
			if e[54+17] != 0x1E {
				t.Errorf("sync flags 0x%02X, want 0x1E", e[54+17])
			}
			// The image size there matches the base block's centimeters and
			// is near 110 pixels per inch.
			size := e[54+12:]
			widthMM := int(size[0]) | int(size[2]&0xF0)<<4
			heightMM := int(size[1]) | int(size[2]&0x0F)<<8
			if int(e[21]) != widthMM/10 || int(e[22]) != heightMM/10 {
				t.Errorf("base size %dx%d cm, detailed timing %dx%d mm", e[21], e[22], widthMM, heightMM)
			}
			if ppi := mode.width * 254 / widthMM / 10; ppi < 109 || ppi > 110 {
				t.Errorf("%d pixels per inch", ppi)
			}

			// Walk the CTA data blocks: one Video Data Block, one YCbCr 4:2:0
			// Video Data Block with the 4:2:0-only modes, the HDMI VSDB,
			// nothing else. A 4:2:0-only mode in the plain Video Data Block
			// would claim RGB, which needs a 594 MHz link at 2160p60.
			cta := e[block:]
			seen := make([]bool, len(guestModes))
			seen[mi] = true
			native := 0
			hdmi := false
			ycbcr420 := false
			dtdStart := int(cta[2])
			for offset := 4; offset < dtdStart; {
				tag := cta[offset] >> 5
				length := int(cta[offset] & 0x1F)
				if offset+1+length > dtdStart {
					t.Fatalf("data block at %d overruns the detailed timings at %d", offset, dtdStart)
				}
				payload := cta[offset+1 : offset+1+length]
				switch {
				case tag == 2:
					for _, svd := range payload {
						vic := svd & 0x7F
						known := false
						for k, other := range guestModes {
							if other.vic != 0 && other.vic == vic && !other.ycbcr420Only {
								seen[k] = true
								known = true
							}
						}
						if !known {
							t.Errorf("Video Data Block offers VIC %d", vic)
						}
						if svd&0x80 != 0 {
							native++
							if vic != mode.vic {
								t.Errorf("native VIC %d, want %d", vic, mode.vic)
							}
						}
					}
				case tag == 7 && length >= 1 && payload[0] == 0x0E:
					for _, vic := range payload[1:] {
						known := false
						for k, other := range guestModes {
							if other.ycbcr420Only && other.vic == vic {
								seen[k] = true
								known = true
							}
						}
						if !known {
							t.Errorf("YCbCr 4:2:0 Video Data Block offers VIC %d", vic)
						}
					}
					ycbcr420 = true
				case tag == 3:
					if length != 7 || !bytes.Equal(payload[:5], []byte{0x03, 0x0C, 0x00, 0x10, 0x00}) {
						t.Errorf("HDMI VSDB % X", payload)
					}
					hdmi = true
				default:
					t.Errorf("unexpected CTA data block tag %d", tag)
				}
				offset += 1 + length
			}
			if !hdmi || !ycbcr420 {
				t.Errorf("HDMI VSDB %t, YCbCr 4:2:0 Video Data Block %t", hdmi, ycbcr420)
			}
			wantNative := 0
			if mode.vic != 0 && !mode.ycbcr420Only {
				wantNative = 1
			}
			if native != wantNative {
				t.Errorf("%d native SVDs, want %d", native, wantNative)
			}

			// Detailed timings after the data blocks list the rest.
			for offset := dtdStart; offset+18 <= block-1 && cta[offset]|cta[offset+1] != 0; offset += 18 {
				dtd := decodeDTD(cta[offset:])
				known := false
				for k, other := range guestModes {
					if dtd.is(other) {
						seen[k] = true
						known = true
						if k == mi {
							t.Errorf("extension repeats the preferred timing")
						}
					}
				}
				if !known {
					t.Errorf("extension offers detailed timing %+v", dtd)
				}
			}
			for k, s := range seen {
				if !s {
					t.Errorf("%s is not offered", guestModes[k])
				}
			}
		})
	}
}

func TestSupported(t *testing.T) {
	cases := []struct {
		width, height int
		want          bool
	}{
		{1280, 720, true},
		{1920, 1080, true},
		{2560, 1440, true},
		{3840, 2160, true},
		{0, 0, false},
		{720, 1280, false},
		{1920, 1200, false},
		{1280, 1024, false},
		{3840, 2161, false},
		{-1280, -720, false},
	}
	for _, c := range cases {
		if got := edid.Supported(c.width, c.height); got != c.want {
			t.Errorf("Supported(%d, %d) = %t, want %t", c.width, c.height, got, c.want)
		}
		got, err := edid.Build(c.width, c.height, 1)
		if c.want {
			continue
		}
		if !errors.Is(err, edid.ErrUnsupportedSize) || got != nil {
			t.Errorf("Build(%d, %d) = %d bytes, %v; want nil, ErrUnsupportedSize", c.width, c.height, len(got), err)
		}
	}
}

func TestStableSerial(t *testing.T) {
	zeroKey := make([]byte, 32)
	// The low 12 bits of the key's first word are set, and get replaced.
	lowBitsKey := append([]byte{0xFF, 0x0F, 0x00, 0x00}, zeroKey[4:]...)
	// Only the first four bytes count.
	tailKey := append([]byte{0x00, 0x10, 0x00, 0x80}, bytes.Repeat([]byte{0xFF}, 28)...)

	cases := []struct {
		name   string
		key    []byte
		height int
		want   uint32
	}{
		{"zero key", zeroKey, 1080, 0x00000438},
		{"low key bits replaced", lowBitsKey, 720, 0x000002D0},
		{"little-endian, tail ignored", tailKey, 1440, 0x800015A0},
		{"height above 12 bits", zeroKey, 0x1870, 0x00000870},
		{"short key of four bytes", []byte{0x00, 0xF0, 0xFF, 0xFF}, 2160, 0xFFFFF870},
	}
	for _, c := range cases {
		if got := edid.StableSerial(c.key, c.height); got != c.want {
			t.Errorf("%s: StableSerial = %08X, want %08X", c.name, got, c.want)
		}
	}

	// Zero means "no serial" in an EDID; every offered height keeps the
	// serial off zero whatever the key.
	for _, m := range guestModes {
		if edid.StableSerial(zeroKey, m.height) == 0 {
			t.Errorf("StableSerial is zero at height %d", m.height)
		}
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestRandomSerial(t *testing.T) {
	// Zero is skipped: the next four bytes are the serial.
	r := bytes.NewReader([]byte{0, 0, 0, 0, 0x4D, 0x3C, 0x2B, 0x1A})
	got, err := edid.RandomSerial(r)
	if err != nil || got != 0x1A2B3C4D {
		t.Errorf("RandomSerial = %08X, %v; want 1A2B3C4D", got, err)
	}

	if _, err := edid.RandomSerial(bytes.NewReader([]byte{0, 0, 0, 0})); err == nil {
		t.Error("RandomSerial with only a zero serial left returned no error")
	}
	if _, err := edid.RandomSerial(errReader{}); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("RandomSerial error %v, want one that wraps the reader's", err)
	}
}

func TestHex(t *testing.T) {
	cases := []struct {
		in   []byte
		want string
	}{
		{nil, ""},
		{[]byte{0x00, 0x0A, 0xB4, 0xFF}, "000AB4FF"},
	}
	for _, c := range cases {
		if got := edid.Hex(c.in); got != c.want {
			t.Errorf("Hex(% X) = %q, want %q", c.in, got, c.want)
		}
	}
}
