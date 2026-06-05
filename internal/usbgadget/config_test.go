package usbgadget

import (
	"strings"
	"testing"

	"github.com/rs/zerolog"
)

func TestAudioGadgetConfigFollowsEnabledDevice(t *testing.T) {
	u := &UsbGadget{enabledDevices: Devices{Audio: false}}
	if u.isGadgetConfigItemEnabled("audio") {
		t.Fatal("audio gadget should be disabled when audio device is disabled")
	}

	u.enabledDevices.Audio = true
	if !u.isGadgetConfigItemEnabled("audio") {
		t.Fatal("audio gadget should be enabled when audio device is enabled")
	}
}

func TestBaseGadgetConfigItemsAlwaysEnabled(t *testing.T) {
	u := &UsbGadget{}
	for _, item := range []string{"base", "base_info"} {
		if !u.isGadgetConfigItemEnabled(item) {
			t.Fatalf("%s should always be enabled", item)
		}
	}
}

func TestWakeHIDUsesTouchscreenSlotOnlyWhenTouchscreenDisabled(t *testing.T) {
	u := &UsbGadget{enabledDevices: Devices{Touchscreen: false}}
	if !u.isGadgetConfigItemEnabled("wake_hid") {
		t.Fatal("wake_hid should be enabled when touchscreen is disabled")
	}

	u.enabledDevices.Touchscreen = true
	if u.isGadgetConfigItemEnabled("wake_hid") {
		t.Fatal("wake_hid should be disabled when touchscreen uses the final HID slot")
	}
}

func TestEnabledGadgetConfigPathsAreUnique(t *testing.T) {
	for name, devices := range map[string]Devices{
		"touchscreen": {
			AbsoluteMouse: true,
			RelativeMouse: true,
			Keyboard:      true,
			Touchscreen:   true,
			MassStorage:   true,
			SerialConsole: true,
			Audio:         true,
		},
		"wake_hid": {
			AbsoluteMouse: true,
			RelativeMouse: true,
			Keyboard:      true,
			Touchscreen:   false,
			MassStorage:   true,
			SerialConsole: true,
			Audio:         true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			assertEnabledGadgetConfigPathsAreUnique(t, devices)
		})
	}
}

func assertEnabledGadgetConfigPathsAreUnique(t *testing.T, devices Devices) {
	t.Helper()

	u := &UsbGadget{enabledDevices: devices}

	paths := map[string]string{}
	configPaths := map[string]string{}

	for key, item := range defaultGadgetConfig {
		if !u.isGadgetConfigItemEnabled(key) {
			continue
		}

		if len(item.path) > 0 {
			pathKey := strings.Join(item.path, "/")
			if previous, ok := paths[pathKey]; ok {
				t.Fatalf("%s and %s share gadget path %s", previous, key, pathKey)
			}
			paths[pathKey] = key
		}

		if len(item.configPath) > 0 {
			configPathKey := strings.Join(item.configPath, "/")
			if previous, ok := configPaths[configPathKey]; ok {
				t.Fatalf("%s and %s share gadget config path %s", previous, key, configPathKey)
			}
			configPaths[configPathKey] = key
		}
	}
}

func TestDisabledSharedConfigPathIsNotRemovedWhenEnabledItemUsesIt(t *testing.T) {
	logger := zerolog.Nop()
	tx := &UsbGadgetTransaction{
		c:                  &ChangeSet{},
		log:                &logger,
		kvmGadgetPath:      "/sys/kernel/config/usb_gadget/test",
		configC1Path:       "/sys/kernel/config/usb_gadget/test/configs/c.1",
		orderedConfigItems: orderedGadgetConfigItems{{"wake_hid", wakeHIDConfig}, {"touchscreen", touchscreenConfig}},
		isGadgetConfigItemEnabled: func(key string) bool {
			return key == "touchscreen"
		},
	}

	tx.WriteGadgetConfig()

	touchscreenConfigPath := "/sys/kernel/config/usb_gadget/test/configs/c.1/hid.usb3"
	for _, change := range tx.c.Changes {
		if change.Path == touchscreenConfigPath && change.ExpectedState == FileStateAbsent && change.When == "" {
			t.Fatalf("disabled wake_hid should not request unconditional removal of enabled touchscreen config path: %s", change.String())
		}
	}

	if tx.reorderSymlinkChanges == nil {
		t.Fatal("expected touchscreen config path to be included in symlink reorder")
	}

	for _, link := range tx.reorderSymlinkChanges.ParamSymlinks {
		if link.Path == touchscreenConfigPath {
			return
		}
	}
	t.Fatalf("expected touchscreen config path %s in symlink reorder", touchscreenConfigPath)
}
