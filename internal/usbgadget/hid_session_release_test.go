package usbgadget

import (
	"bytes"
	"io"
	"os"
	"testing"
	"time"
)

func TestAbsoluteSessionReleasePreservesLastSuccessfulPosition(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	u := newTestGadgetWithKeyboard(nil)
	u.absMouseHidFile = w
	u.enabledDevices.AbsoluteMouse = true
	read := func(want []byte) {
		t.Helper()
		_ = r.SetReadDeadline(time.Now().Add(time.Second))
		got := make([]byte, len(want))
		if _, err := io.ReadFull(r, got); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("report=%v want=%v", got, want)
		}
	}
	if err := u.ReleaseAbsoluteMouseButtons(); err != nil {
		t.Fatal(err)
	}
	if err := u.AbsMouseReport(100, 200, 1); err != nil {
		t.Fatal(err)
	}
	read([]byte{1, 1, 100, 0, 200, 0})
	if err := u.AbsMouseReport(300, 400, 1); err != nil {
		t.Fatal(err)
	}
	read([]byte{1, 1, 44, 1, 144, 1})
	if err := u.ReleaseAbsoluteMouseButtons(); err != nil {
		t.Fatal(err)
	}
	read([]byte{1, 0, 44, 1, 144, 1})
	if u.absMousePressed {
		t.Fatal("release retained pressed state")
	}
	if err := u.ReleaseAbsoluteMouseButtons(); err != nil {
		t.Fatal(err)
	}
	_ = r.SetReadDeadline(time.Now().Add(20 * time.Millisecond))
	extra := make([]byte, 1)
	if n, _ := r.Read(extra); n != 0 {
		t.Fatal("unpressed release moved pointer")
	}
}
