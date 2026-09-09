package usbgadget

import (
	"os"
	"path/filepath"
	"testing"
)

// wakeup_on_write is optional in the kernel. Where it is missing the staged
// write can never succeed and Commit ignores the failure, so it must not count
// as a pending change or every start would rebind on such kernels.
func TestHasPendingChangesIgnoresMissingOptionalAttribute(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "protocol"), []byte("1"), 0644); err != nil {
		t.Fatal(err)
	}

	stage := func() *UsbGadgetTransaction {
		tx := &UsbGadgetTransaction{c: &ChangeSet{}, log: defaultLogger}
		tx.writeGadgetAttrs(dir, gadgetAttributes{"protocol": "1", "wakeup_on_write": "0"}, "keyboard", nil)
		return tx
	}

	pending, err := stage().HasPendingChanges()
	if err != nil {
		t.Fatal(err)
	}
	if pending {
		t.Fatal("a missing optional attribute was reported as a pending change")
	}

	// Present with another value it is a real change.
	if err := os.WriteFile(filepath.Join(dir, "wakeup_on_write"), []byte("1"), 0644); err != nil {
		t.Fatal(err)
	}
	pending, err = stage().HasPendingChanges()
	if err != nil {
		t.Fatal(err)
	}
	if !pending {
		t.Fatal("an optional attribute with the wrong value was not reported as pending")
	}
}
