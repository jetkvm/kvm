package usbgadget

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sourcegraph/tf-dag/dag"
)

// The mounted mode lives nowhere but the LUN itself, so a restart that adopts
// the medium has to adopt its mode too. Otherwise the defaults would be
// rebuilt and a disk-mounted image would come back as a CD-ROM.
func TestSyncMassStorageAdoptsLiveMode(t *testing.T) {
	gadgetDir := t.TempDir()
	lun := filepath.Join(gadgetDir, "functions", "mass_storage.usb0", "lun.0")
	if err := os.MkdirAll(lun, 0755); err != nil {
		t.Fatal(err)
	}
	live := map[string]string{
		"file":           "/userdata/jetkvm/images/debian-arm64.iso\n",
		"cdrom":          "0\n",
		"ro":             "0\n",
		"removable":      "1\n",
		"inquiry_string": "JetKVM  Virtual Media\n",
	}
	for name, content := range live {
		if err := os.WriteFile(filepath.Join(lun, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	u := &UsbGadget{
		kvmGadgetPath: gadgetDir,
		configMap:     deepCopyConfigMap(defaultGadgetConfig),
		log:           defaultLogger,
	}
	u.syncMassStorageImageFromKernel()

	attrs := u.configMap["mass_storage_lun0"].attrs
	for _, attr := range []string{"cdrom", "ro"} {
		if got, want := attrs[attr], strings.TrimSpace(live[attr]); got != want {
			t.Errorf("%s not adopted: got %q, want %q", attr, got, want)
		}
	}

	// With the mode adopted the transaction has nothing to write, so nothing
	// can be refused while the medium is open.
	tx := &UsbGadgetTransaction{c: &ChangeSet{}, log: defaultLogger}
	tx.writeGadgetAttrs(lun, attrs, "mass_storage.usb0", nil)
	pending, err := tx.HasPendingChanges()
	if err != nil {
		t.Fatal(err)
	}
	if pending {
		t.Error("a restart with the medium already mounted still wants to write the LUN attributes")
	}
}

// Adopting the mode is what lets a later mode change write cdrom on its own.
// Without the adoption ro would differ from the defaults too and the same
// transaction would write both, one of them needlessly.
func TestModeChangeAfterAdoptionWritesCdromOnly(t *testing.T) {
	gadgetDir := t.TempDir()
	lun := filepath.Join(gadgetDir, "functions", "mass_storage.usb0", "lun.0")
	if err := os.MkdirAll(lun, 0755); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"file":           "/userdata/jetkvm/images/debian-arm64.iso\n",
		"cdrom":          "0\n",
		"ro":             "0\n",
		"removable":      "1\n",
		"inquiry_string": "JetKVM  Virtual Media\n",
	} {
		if err := os.WriteFile(filepath.Join(lun, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	u := &UsbGadget{
		kvmGadgetPath: gadgetDir,
		configMap:     deepCopyConfigMap(defaultGadgetConfig),
		log:           defaultLogger,
	}
	u.syncMassStorageImageFromKernel()

	// What setMassStorageMode does when the mount RPC asks for CDROM.
	attrs := u.configMap["mass_storage_lun0"].attrs
	attrs["cdrom"] = "1"

	tx := &UsbGadgetTransaction{c: &ChangeSet{}, log: defaultLogger}
	tx.writeGadgetAttrs(lun, attrs, "mass_storage.usb0", nil)

	r := ChangeSetResolver{changeset: tx.c, g: &dag.AcyclicGraph{}, l: tx.log}
	changes, err := r.GetChanges()
	if err != nil {
		t.Fatal(err)
	}
	written := make([]string, 0, 1)
	for _, c := range changes {
		if c.Action() != FileChangeResolvedActionDoNothing {
			written = append(written, filepath.Base(c.Path))
		}
	}
	if len(written) != 1 || written[0] != "cdrom" {
		t.Errorf("expected cdrom to be the only attribute written, got %v", written)
	}
}
