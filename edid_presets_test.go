//go:build linux && arm

package kvm

import (
	"encoding/hex"
	"testing"

	"github.com/jetkvm/kvm/internal/native"
)

func TestEDIDPresets(t *testing.T) {
	presets, err := rpcGetEDIDPresets()
	if err != nil {
		t.Fatalf("rpcGetEDIDPresets: %v", err)
	}
	if len(presets) == 0 {
		t.Fatal("no EDID presets")
	}
	if presets[0].EDID != native.DefaultEDID {
		t.Errorf("first preset must be the default EDID, got %q", presets[0].Name)
	}

	names := map[string]bool{}
	edids := map[string]bool{}
	for _, p := range presets {
		if p.Name == "" {
			t.Errorf("preset %q has no name", p.EDID[:16])
		}
		if names[p.Name] {
			t.Errorf("duplicate preset name %q", p.Name)
		}
		names[p.Name] = true

		raw, err := hex.DecodeString(p.EDID)
		if err != nil {
			t.Errorf("preset %q is not hex: %v", p.Name, err)
			continue
		}
		if len(raw)%128 != 0 || len(raw) == 0 {
			t.Errorf("preset %q is %d bytes, want a multiple of 128", p.Name, len(raw))
		}
		if edids[p.EDID] {
			t.Errorf("preset %q duplicates another preset's EDID", p.Name)
		}
		edids[p.EDID] = true
	}
}
