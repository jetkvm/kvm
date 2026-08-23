package kvm

import (
	"errors"
	"testing"

	"github.com/jetkvm/kvm/internal/usbgadget"
)

func TestRPCHIDReportRejectsEveryNonConfiguredUSBState(t *testing.T) {
	originalState := usbState
	t.Cleanup(func() {
		usbStateLock.Lock()
		usbState = originalState
		usbStateLock.Unlock()
	})

	for _, state := range []string{
		usbgadget.USBStateUnknown,
		usbgadget.USBStateNotAttached,
		"suspended",
	} {
		t.Run(state, func(t *testing.T) {
			usbStateLock.Lock()
			usbState = state
			usbStateLock.Unlock()

			called := false
			err := rpcHidReport(func() error {
				called = true
				return nil
			})
			if err == nil {
				t.Fatal("rpcHidReport returned success while USB was not configured")
			}
			if called {
				t.Fatal("rpcHidReport attempted a HID write while USB was not configured")
			}
		})
	}
}

func TestRPCHIDReportReturnsHIDWriteError(t *testing.T) {
	originalState := usbState
	t.Cleanup(func() {
		usbStateLock.Lock()
		usbState = originalState
		usbStateLock.Unlock()
	})

	usbStateLock.Lock()
	usbState = usbgadget.USBStateConfigured
	usbStateLock.Unlock()

	want := errors.New("HID write failed")
	if err := rpcHidReport(func() error { return want }); !errors.Is(err, want) {
		t.Fatalf("rpcHidReport error = %v, want %v", err, want)
	}
}
