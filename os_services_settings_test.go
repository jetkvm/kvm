package kvm

import "testing"

func TestMergeOSServicesConfigSwitch(t *testing.T) {
	base := *defaultOSServicesConfig()
	if base.Enabled {
		t.Fatalf("OS Services is on by default")
	}

	merged, err := mergeOSServicesConfig(base, map[string]any{"enabled": true})
	if err != nil || !merged.Enabled {
		t.Fatalf("enabled true: %+v, %v", merged, err)
	}
	if _, err := mergeOSServicesConfig(base, map[string]any{"enabled": "yes"}); err == nil {
		t.Fatalf("a string was taken for the switch")
	}
}

// The browser sends back the settings it got, with the record of the last
// sender in them. Only the KVM writes that record.
func TestMergeOSServicesConfigKeepsLastSender(t *testing.T) {
	base := *defaultOSServicesConfig()
	base.LastSenderVersion = "0.4.6"
	base.LastSenderAt = "2026-11-17T20:00:00Z"

	merged, err := mergeOSServicesConfig(base, map[string]any{
		"enabled":             true,
		"last_sender_version": "9.9.9",
		"last_sender_at":      "",
	})
	if err != nil {
		t.Fatal(err)
	}
	if merged.LastSenderVersion != base.LastSenderVersion || merged.LastSenderAt != base.LastSenderAt {
		t.Fatalf("a settings request changed the last sender: %+v", merged)
	}
}
