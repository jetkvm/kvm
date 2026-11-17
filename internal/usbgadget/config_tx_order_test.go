package usbgadget

import (
	"strings"
	"testing"

	"github.com/sourcegraph/tf-dag/dag"
)

// Rebinding while a disabled function's symlink is still present enumerates
// it on the host, and removing it afterwards makes configfs unbind the gadget
// again, so the removals have to come before the UDC write. Nothing ordered
// them: the UDC write only waited on the reorder change of the enabled
// functions.
func TestUDCWriteFollowsDisabledFunctionRemovals(t *testing.T) {
	// Devices{} disables every class the config can disable. The wake HID
	// function stays enabled, as on a device.
	u := NewUsbGadget("test", &Devices{}, &Config{}, nil)
	u.udc = "test.usb"
	if err := u.newUsbGadgetTransaction(true); err != nil {
		t.Fatal(err)
	}
	tx := u.tx
	tx.WriteGadgetConfig()
	// Complete the graph the way Commit does.
	if tx.reorderSymlinkChanges != nil {
		tx.addFileChange("gadget-finalize", *tx.reorderSymlinkChanges)
	}

	r := ChangeSetResolver{changeset: tx.c, g: &dag.AcyclicGraph{}, l: tx.log}
	changes, err := r.GetChanges()
	if err != nil {
		t.Fatal(err)
	}

	// Only the unconditional removals staged for disabled functions count;
	// enabled functions stage a conditional one that runs before their own
	// changes.
	udcIndex, lastRemoval, removals := -1, -1, 0
	for i, c := range changes {
		switch {
		case c.Key == "udc":
			udcIndex = i
		case c.ExpectedState == FileStateAbsent && c.When == "" &&
			strings.HasPrefix(c.Path, tx.configC1Path+"/"):
			lastRemoval = i
			removals++
		}
	}
	if udcIndex < 0 || removals == 0 {
		t.Fatalf("expected a UDC write and function removals, got udc=%d removals=%d", udcIndex, removals)
	}
	if lastRemoval > udcIndex {
		t.Fatalf("UDC write at %d precedes a function removal at %d", udcIndex, lastRemoval)
	}
}
