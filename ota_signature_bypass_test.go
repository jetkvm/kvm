package kvm

import "testing"

func TestShouldBypassSignatureCheck_IncludePreRelease(t *testing.T) {
	params := updateParams{}
	if !shouldBypassSignatureCheck(params, true) {
		t.Fatal("expected bypass for includePreRelease=true")
	}
}

func TestShouldBypassSignatureCheck_StableNoOverride(t *testing.T) {
	params := updateParams{
		Components: map[string]string{
			"app": "1.2.3",
		},
	}
	if shouldBypassSignatureCheck(params, false) {
		t.Fatal("expected no bypass for stable update without override")
	}
}

func TestShouldBypassSignatureCheck_TargetedDevVersion(t *testing.T) {
	params := updateParams{
		Components: map[string]string{
			"app": "1.2.4-dev.1",
		},
	}
	if !shouldBypassSignatureCheck(params, false) {
		t.Fatal("expected bypass for targeted dev version")
	}
}

func TestShouldBypassSignatureCheck_TargetedStableVersion(t *testing.T) {
	params := updateParams{
		Components: map[string]string{
			"app": "1.2.4",
		},
	}
	if shouldBypassSignatureCheck(params, false) {
		t.Fatal("expected no bypass for targeted stable version")
	}
}
