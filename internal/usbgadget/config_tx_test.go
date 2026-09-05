package usbgadget

import "testing"

// With every device class disabled WriteGadgetConfig stages no function and
// leaves reorderSymlinkChanges nil. Commit used to dereference it (#1541).
func TestCommitWithoutFunctions(t *testing.T) {
	tx := &UsbGadgetTransaction{c: &ChangeSet{}, log: defaultLogger}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit of an empty transaction failed: %v", err)
	}
}
