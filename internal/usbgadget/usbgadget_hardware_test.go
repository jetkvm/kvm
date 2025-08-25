//go:build arm && linux

package usbgadget

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// Hardware integration tests for USB gadget operations
// These tests perform real hardware operations with proper cleanup and timeout handling

var (
	testConfig = &Config{
		VendorId:     "0x1d6b", // The Linux Foundation
		ProductId:    "0x0104", // Multifunction Composite Gadget
		SerialNumber: "",
		Manufacturer: "JetKVM",
		Product:      "USB Emulation Device",
		strictMode:   false, // Disable strict mode for hardware tests
	}
	testDevices = &Devices{
		AbsoluteMouse: true,
		RelativeMouse: true,
		Keyboard:      true,
		MassStorage:   true,
	}
	testGadgetName = "jetkvm-test"
)

func TestUsbGadgetHardwareInit(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping hardware test in short mode")
	}

	// Create context with timeout to prevent hanging
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Ensure clean state before test
	cleanupUsbGadget(t, testGadgetName)

	// Test USB gadget initialization with timeout
	var gadget *UsbGadget
	done := make(chan bool, 1)
	var initErr error

	go func() {
		defer func() {
			if r := recover(); r != nil {
				t.Logf("USB gadget initialization panicked: %v", r)
				initErr = assert.AnError
			}
			done <- true
		}()

		gadget = NewUsbGadget(testGadgetName, testDevices, testConfig, nil)
		if gadget == nil {
			initErr = assert.AnError
		}
	}()

	// Wait for initialization or timeout
	select {
	case <-done:
		if initErr != nil {
			t.Fatalf("USB gadget initialization failed: %v", initErr)
		}
		assert.NotNil(t, gadget, "USB gadget should be initialized")
	case <-ctx.Done():
		t.Fatal("USB gadget initialization timed out")
	}

	// Cleanup after test
	defer func() {
		if gadget != nil {
			gadget.CloseHidFiles()
		}
		cleanupUsbGadget(t, testGadgetName)
	}()

	// Validate gadget state
	assert.NotNil(t, gadget, "USB gadget should not be nil")

	// Test UDC binding state
	bound, err := gadget.IsUDCBound()
	assert.NoError(t, err, "Should be able to check UDC binding state")
	t.Logf("UDC bound state: %v", bound)
}

func TestUsbGadgetHardwareReconfiguration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping hardware test in short mode")
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	// Ensure clean state
	cleanupUsbGadget(t, testGadgetName)

	// Initialize first gadget
	gadget1 := createUsbGadgetWithTimeout(t, ctx, testGadgetName, testDevices, testConfig)
	defer func() {
		if gadget1 != nil {
			gadget1.CloseHidFiles()
		}
	}()

	// Validate initial state
	assert.NotNil(t, gadget1, "First USB gadget should be initialized")

	// Close first gadget properly
	gadget1.CloseHidFiles()
	gadget1 = nil

	// Wait for cleanup to complete
	time.Sleep(500 * time.Millisecond)

	// Test reconfiguration with different report descriptor
	altGadgetConfig := make(map[string]gadgetConfigItem)
	for k, v := range defaultGadgetConfig {
		altGadgetConfig[k] = v
	}

	// Modify absolute mouse configuration
	oldAbsoluteMouseConfig := altGadgetConfig["absolute_mouse"]
	oldAbsoluteMouseConfig.reportDesc = absoluteMouseCombinedReportDesc
	altGadgetConfig["absolute_mouse"] = oldAbsoluteMouseConfig

	// Create second gadget with modified configuration
	gadget2 := createUsbGadgetWithTimeoutAndConfig(t, ctx, testGadgetName, altGadgetConfig, testDevices, testConfig)
	defer func() {
		if gadget2 != nil {
			gadget2.CloseHidFiles()
		}
		cleanupUsbGadget(t, testGadgetName)
	}()

	assert.NotNil(t, gadget2, "Second USB gadget should be initialized")

	// Validate UDC binding after reconfiguration
	udcs := getUdcs()
	assert.NotEmpty(t, udcs, "Should have at least one UDC")

	if len(udcs) > 0 {
		udc := udcs[0]
		t.Logf("Available UDC: %s", udc)

		// Check UDC binding state
		udcStr, err := os.ReadFile("/sys/kernel/config/usb_gadget/" + testGadgetName + "/UDC")
		if err == nil {
			t.Logf("UDC binding: %s", strings.TrimSpace(string(udcStr)))
		} else {
			t.Logf("Could not read UDC binding: %v", err)
		}
	}
}

func TestUsbGadgetHardwareStressTest(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	// Create context with longer timeout for stress test
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Ensure clean state
	cleanupUsbGadget(t, testGadgetName)

	// Perform multiple rapid reconfigurations
	for i := 0; i < 3; i++ {
		t.Logf("Stress test iteration %d", i+1)

		// Create gadget
		gadget := createUsbGadgetWithTimeout(t, ctx, testGadgetName, testDevices, testConfig)
		if gadget == nil {
			t.Fatalf("Failed to create USB gadget in iteration %d", i+1)
		}

		// Validate gadget
		assert.NotNil(t, gadget, "USB gadget should be created in iteration %d", i+1)

		// Test basic operations
		bound, err := gadget.IsUDCBound()
		assert.NoError(t, err, "Should be able to check UDC state in iteration %d", i+1)
		t.Logf("Iteration %d: UDC bound = %v", i+1, bound)

		// Cleanup
		gadget.CloseHidFiles()
		gadget = nil

		// Wait between iterations
		time.Sleep(1 * time.Second)

		// Check for timeout
		select {
		case <-ctx.Done():
			t.Fatal("Stress test timed out")
		default:
			// Continue
		}
	}

	// Final cleanup
	cleanupUsbGadget(t, testGadgetName)
}

// Helper functions for hardware tests

// createUsbGadgetWithTimeout creates a USB gadget with timeout protection
func createUsbGadgetWithTimeout(t *testing.T, ctx context.Context, name string, devices *Devices, config *Config) *UsbGadget {
	return createUsbGadgetWithTimeoutAndConfig(t, ctx, name, defaultGadgetConfig, devices, config)
}

// createUsbGadgetWithTimeoutAndConfig creates a USB gadget with custom config and timeout protection
func createUsbGadgetWithTimeoutAndConfig(t *testing.T, ctx context.Context, name string, gadgetConfig map[string]gadgetConfigItem, devices *Devices, config *Config) *UsbGadget {
	var gadget *UsbGadget
	done := make(chan bool, 1)
	var createErr error

	go func() {
		defer func() {
			if r := recover(); r != nil {
				t.Logf("USB gadget creation panicked: %v", r)
				createErr = assert.AnError
			}
			done <- true
		}()

		gadget = newUsbGadget(name, gadgetConfig, devices, config, nil)
		if gadget == nil {
			createErr = assert.AnError
		}
	}()

	// Wait for creation or timeout
	select {
	case <-done:
		if createErr != nil {
			t.Logf("USB gadget creation failed: %v", createErr)
			return nil
		}
		return gadget
	case <-ctx.Done():
		t.Logf("USB gadget creation timed out")
		return nil
	}
}

// cleanupUsbGadget ensures clean state by removing any existing USB gadget configuration
func cleanupUsbGadget(t *testing.T, name string) {
	t.Logf("Cleaning up USB gadget: %s", name)

	// Try to unbind UDC first
	udcPath := "/sys/kernel/config/usb_gadget/" + name + "/UDC"
	if _, err := os.Stat(udcPath); err == nil {
		// Read current UDC binding
		if udcData, err := os.ReadFile(udcPath); err == nil && len(strings.TrimSpace(string(udcData))) > 0 {
			// Unbind UDC
			if err := os.WriteFile(udcPath, []byte(""), 0644); err != nil {
				t.Logf("Failed to unbind UDC: %v", err)
			} else {
				t.Logf("Successfully unbound UDC")
				// Wait for unbinding to complete
				time.Sleep(200 * time.Millisecond)
			}
		}
	}

	// Remove gadget directory if it exists
	gadgetPath := "/sys/kernel/config/usb_gadget/" + name
	if _, err := os.Stat(gadgetPath); err == nil {
		// Try to remove configuration links first
		configPath := gadgetPath + "/configs/c.1"
		if entries, err := os.ReadDir(configPath); err == nil {
			for _, entry := range entries {
				if entry.Type()&os.ModeSymlink != 0 {
					linkPath := configPath + "/" + entry.Name()
					if err := os.Remove(linkPath); err != nil {
						t.Logf("Failed to remove config link %s: %v", linkPath, err)
					}
				}
			}
		}

		// Remove the gadget directory (this should cascade remove everything)
		if err := os.RemoveAll(gadgetPath); err != nil {
			t.Logf("Failed to remove gadget directory: %v", err)
		} else {
			t.Logf("Successfully removed gadget directory")
		}
	}

	// Wait for cleanup to complete
	time.Sleep(300 * time.Millisecond)
}

// validateHardwareState checks the current hardware state
func validateHardwareState(t *testing.T, gadget *UsbGadget) {
	if gadget == nil {
		return
	}

	// Check UDC binding state
	bound, err := gadget.IsUDCBound()
	if err != nil {
		t.Logf("Warning: Could not check UDC binding state: %v", err)
	} else {
		t.Logf("UDC bound: %v", bound)
	}

	// Check available UDCs
	udcs := getUdcs()
	t.Logf("Available UDCs: %v", udcs)

	// Check configfs mount
	if _, err := os.Stat("/sys/kernel/config"); err != nil {
		t.Logf("Warning: configfs not available: %v", err)
	} else {
		t.Logf("configfs is available")
	}
}