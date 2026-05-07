package usbgadget

import (
	"bytes"
	"testing"
)

func TestRelativeMouseReportBytes(t *testing.T) {
	got := relativeMouseReportBytes(-2, 3, 0x05)
	want := []byte{0x05, 0xFE, 0x03, 0x00, 0x00}
	if !bytes.Equal(got, want) {
		t.Fatalf("relativeMouseReportBytes() = %v, want %v", got, want)
	}
}

func TestRelativeMouseWheelReportBytesPreservesButtons(t *testing.T) {
	got := relativeMouseWheelReportBytes(-127, 1, 0x02)
	want := []byte{0x02, 0x00, 0x00, 0x81, 0x01}
	if !bytes.Equal(got, want) {
		t.Fatalf("relativeMouseWheelReportBytes() = %v, want %v", got, want)
	}
}
