package kvm

import "testing"

func TestSetAudioConfigEnabledClearsAndSaves(t *testing.T) {
	originalConfig := config
	defer func() {
		config = originalConfig
	}()

	config = &Config{AudioEnabled: true}
	saveCalls := 0

	if err := setAudioConfigEnabled(false, func() error {
		saveCalls++
		return nil
	}); err != nil {
		t.Fatalf("setAudioConfigEnabled returned error: %v", err)
	}

	if config.AudioEnabled {
		t.Fatal("expected audio config to be disabled")
	}
	if saveCalls != 1 {
		t.Fatalf("expected config to be saved once, got %d", saveCalls)
	}
}

func TestSetAudioConfigEnabledSkipsSaveWhenUnchanged(t *testing.T) {
	originalConfig := config
	defer func() {
		config = originalConfig
	}()

	config = &Config{AudioEnabled: false}
	saveCalls := 0

	if err := setAudioConfigEnabled(false, func() error {
		saveCalls++
		return nil
	}); err != nil {
		t.Fatalf("setAudioConfigEnabled returned error: %v", err)
	}

	if saveCalls != 0 {
		t.Fatalf("expected config save to be skipped, got %d saves", saveCalls)
	}
}
