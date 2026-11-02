package kvm

import (
	"errors"
	"reflect"
	"testing"

	"github.com/jetkvm/kvm/internal/native"
)

type edidTransactionNative struct {
	native.EmptyNativeInterface
	writes   []string
	failures []error
}

func (n *edidTransactionNative) VideoSetEDID(value string) error {
	n.writes = append(n.writes, value)
	if len(n.writes) <= len(n.failures) {
		return n.failures[len(n.writes)-1]
	}
	return nil
}

func setupEDIDTransaction(t *testing.T) *edidTransactionNative {
	t.Helper()
	oldConfig, oldNative, oldAdvertised := config, nativeInstance, hostDisplayAdvertised
	config = &Config{EdidString: edidPresets[1].EDID}
	hostDisplayAdvertised = true
	n := &edidTransactionNative{}
	nativeInstance = n
	t.Cleanup(func() {
		config, nativeInstance, hostDisplayAdvertised = oldConfig, oldNative, oldAdvertised
	})
	return n
}

func TestEDIDApplyFailureRestoresAdvertisement(t *testing.T) {
	for _, failRollback := range []bool{false, true} {
		t.Run(map[bool]string{false: "restore succeeds", true: "restore fails"}[failRollback], func(t *testing.T) {
			n := setupEDIDTransaction(t)
			applyErr, rollbackErr := errors.New("apply ioctl"), errors.New("restore ioctl")
			n.failures = []error{applyErr}
			if failRollback {
				n.failures = append(n.failures, rollbackErr)
			}
			saved := false
			err := setEDIDWithSave("", func() error { saved = true; return nil })
			if !errors.Is(err, applyErr) || (failRollback && !errors.Is(err, rollbackErr)) {
				t.Fatalf("lost transaction error: %v", err)
			}
			if saved || config.EdidString != edidPresets[1].EDID || !reflect.DeepEqual(n.writes, []string{native.DefaultEDID, edidPresets[1].EDID}) {
				t.Fatalf("failed apply did not restore: saved=%v config=%q writes=%v", saved, config.EdidString, n.writes)
			}
		})
	}
}

func TestEDIDSaveFailurePreservesRollbackError(t *testing.T) {
	n := setupEDIDTransaction(t)
	saveErr, rollbackErr := errors.New("config disk full"), errors.New("restore ioctl")
	n.failures = []error{nil, rollbackErr}
	err := setEDIDWithSave("", func() error { return saveErr })
	if !errors.Is(err, saveErr) || !errors.Is(err, rollbackErr) {
		t.Fatalf("lost save or restore error: %v", err)
	}
	if config.EdidString != edidPresets[1].EDID || len(n.writes) != 2 {
		t.Fatalf("failed save did not restore config/advertisement: config=%q writes=%v", config.EdidString, n.writes)
	}
}

func TestEDIDResetSavesEmptyConfigAfterApply(t *testing.T) {
	n := setupEDIDTransaction(t)
	saved := false
	err := setEDIDWithSave("", func() error {
		if config.EdidString != "" || !reflect.DeepEqual(n.writes, []string{native.DefaultEDID}) {
			t.Fatal("config was saved before applying the reset")
		}
		saved = true
		return nil
	})
	if err != nil || !saved || config.EdidString != "" || len(n.writes) != 1 {
		t.Fatalf("reset failed: saved=%v config=%q writes=%v error=%v", saved, config.EdidString, n.writes, err)
	}
}
