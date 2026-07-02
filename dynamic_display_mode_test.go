package kvm

import (
	"encoding/hex"
	"testing"
)

func TestBuildDynamicDisplayEDIDUsesCompanionMode(t *testing.T) {
	edidHex, err := buildDynamicDisplayEDID(DisplayMode{
		Width:     1080,
		Height:    2400,
		RefreshHz: 30,
		Source:    "companion",
	})
	if err != nil {
		t.Fatalf("buildDynamicDisplayEDID returned error: %v", err)
	}

	edid, err := hex.DecodeString(edidHex)
	if err != nil {
		t.Fatalf("decode EDID: %v", err)
	}
	if len(edid) != 128 {
		t.Fatalf("EDID length = %d, want 128", len(edid))
	}

	width, height := detailedTimingActiveSize(edid[54:72])
	if width != 1080 || height != 2400 {
		t.Fatalf("preferred DTD = %dx%d, want 1080x2400", width, height)
	}
	for block := 0; block+127 < len(edid); block += 128 {
		sum := 0
		for i := 0; i < 128; i++ {
			sum += int(edid[block+i])
		}
		if sum%256 != 0 {
			t.Fatalf("EDID block %d checksum invalid: sum %% 256 = %d", block/128, sum%256)
		}
	}
}

func TestBuildDynamicDisplayEDIDRejectsOversizedMode(t *testing.T) {
	_, err := buildDynamicDisplayEDID(DisplayMode{
		Width:     5000,
		Height:    2400,
		RefreshHz: 30,
	})
	if err == nil {
		t.Fatal("expected oversized mode to fail")
	}
}

func TestSelectCompanionDisplayModeScalesLongEdge(t *testing.T) {
	mode := selectCompanionDisplayMode(TargetMetadata{
		TargetType:    "android",
		Fresh:         true,
		DisplayWidth:  1080,
		DisplayHeight: 2400,
	})

	if mode.Width != 720 || mode.Height != 1600 || mode.RefreshHz != 60 {
		t.Fatalf("mode = %dx%d@%d, want 720x1600@60", mode.Width, mode.Height, mode.RefreshHz)
	}
	if mode.Source != "companion-aspect-scaled" {
		t.Fatalf("source = %q, want companion-aspect-scaled", mode.Source)
	}
}

func detailedTimingActiveSize(dtd []byte) (int, int) {
	width := int(dtd[2]) | (int(dtd[4]&0xf0) << 4)
	height := int(dtd[5]) | (int(dtd[7]&0xf0) << 4)
	return width, height
}
