package usbgadget

import (
	"testing"
	"time"
)

func TestHostEnumerationWait(t *testing.T) {
	start := time.Unix(100, 0)
	var w HostEnumerationWait

	if w.ShouldRebind(true, start) {
		t.Fatal("first observation must start the wait, not rebind")
	}
	if w.ShouldRebind(true, start.Add(USBHostEnumerationTimeout-time.Second)) {
		t.Fatal("rebind before the timeout")
	}
	if !w.ShouldRebind(true, start.Add(USBHostEnumerationTimeout)) {
		t.Fatal("no rebind once the timeout elapsed")
	}
	if w.ShouldRebind(true, start.Add(USBHostEnumerationTimeout+time.Second)) {
		t.Fatal("second rebind right after the first")
	}
	if w.ShouldRebind(true, start.Add(3*USBHostEnumerationTimeout)) {
		t.Fatal("the episode permits one rebind, not one per timeout")
	}
}

func TestHostEnumerationWaitHostAbsent(t *testing.T) {
	start := time.Unix(100, 0)
	var w HostEnumerationWait

	if w.ShouldRebind(false, start) {
		t.Fatal("rebind with no host")
	}
	if w.ShouldRebind(true, start.Add(USBHostEnumerationTimeout)) {
		t.Fatal("an absent host must not have started the wait")
	}

	w.ShouldRebind(true, start)
	if w.ShouldRebind(false, start.Add(USBHostEnumerationTimeout)) {
		t.Fatal("rebind with the host absent")
	}
	if w.ShouldRebind(true, start.Add(USBHostEnumerationTimeout+time.Second)) {
		t.Fatal("host absence must reset the wait")
	}
}

func TestHostEnumerationWaitReset(t *testing.T) {
	start := time.Unix(100, 0)
	var w HostEnumerationWait

	w.ShouldRebind(true, start)
	w.ShouldRebind(true, start.Add(USBHostEnumerationTimeout))
	w.Reset()
	if w.ShouldRebind(true, start.Add(USBHostEnumerationTimeout)) {
		t.Fatal("Reset must forget the wait")
	}
	if !w.ShouldRebind(true, start.Add(2*USBHostEnumerationTimeout)) {
		t.Fatal("a fresh episode after Reset must permit a rebind again")
	}
}
