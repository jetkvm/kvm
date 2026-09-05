package usbgadget

import (
	"errors"
	"testing"
)

func TestWithTransactionClearsFailedTransaction(t *testing.T) {
	u := NewUsbGadget("test", &Devices{}, &Config{}, nil)

	want := errors.New("callback failed")
	if err := u.WithTransaction(func() error { return want }); !errors.Is(err, want) {
		t.Fatalf("first transaction: got %v, want %v", err, want)
	}
	if u.tx != nil {
		t.Fatal("failed transaction was left behind")
	}

	// A second transaction must start; before the fix it failed with
	// "transaction already exists".
	if err := u.WithTransaction(func() error { return want }); !errors.Is(err, want) {
		t.Fatalf("second transaction: got %v, want %v", err, want)
	}
}
