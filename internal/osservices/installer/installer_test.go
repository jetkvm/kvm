package installer

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

// The images the JetKVM Mini's generator makes for the same versions, with
// the link changed to kvm=:
//
//	sed 's/?mini=/?kvm=/' builtin_installer_iso.py > ref.py
//	python3 ref.py --version 0.5.2 --iso-output ref.iso && sha256sum ref.iso
func TestImageMatchesTheMiniGenerator(t *testing.T) {
	for version, want := range map[string]string{
		"0.5.2":     "bd0f97196f4053a8551cfad8239b54fd6bcb14f83c7efc023f341ab6bbd1dd6f",
		"0.1.0+dev": "3b759da90a9034e0ae4ab932680f785879222c427d89cd8169e80f70955c4194",
	} {
		digest := sha256.Sum256(Image(version))
		if got := hex.EncodeToString(digest[:]); got != want {
			t.Errorf("%s: sha256 %s, want %s", version, got, want)
		}
	}
}

func TestPageLinksToThisFirmware(t *testing.T) {
	image := Image("0.1.0+dev")
	link := []byte(`url=https://jetkvm.com/os-services/install?kvm=0.1.0%2Bdev"`)
	if page := image[fileSector*sector:]; !bytes.Contains(page[:sector], link) {
		t.Fatalf("the page does not hold %s", link)
	}
	if len(image) != (fileSector+1+padSectors)*sector {
		t.Fatalf("image of %d bytes", len(image))
	}
}
