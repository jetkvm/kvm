package main

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func waitMonitorSignal(t *testing.T, signal <-chan struct{}, description string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", description)
	}
}

func snapshotMonitors(a *Agent, monitored map[string]*monitorEntry) map[string]*monitorEntry {
	a.monitorMu.Lock()
	defer a.monitorMu.Unlock()
	result := make(map[string]*monitorEntry, len(monitored))
	for path, entry := range monitored {
		result[path] = entry
	}
	return result
}

func TestReconcileMonitorsLateExitKeepsReplacement(t *testing.T) {
	a := newAgent()
	monitored := make(map[string]*monitorEntry)
	devices := map[string]string{"keyboard": "test keyboard"}
	oldStarted := make(chan struct{})
	releaseOld := make(chan struct{})
	var releaseOnce sync.Once
	var oldCtx context.Context
	var entries []*monitorEntry
	t.Cleanup(func() {
		releaseOnce.Do(func() { close(releaseOld) })
		a.reconcileMonitors(monitored, nil, nil)
		for _, entry := range entries {
			entry.cancel()
			waitMonitorSignal(t, entry.done, "monitor cleanup")
		}
	})

	a.reconcileMonitors(monitored, devices, func(ctx context.Context, _, _ string) {
		oldCtx = ctx
		close(oldStarted)
		<-releaseOld // Deliberately delay exit after cancellation.
	})
	old := snapshotMonitors(a, monitored)["keyboard"]
	entries = append(entries, old)
	waitMonitorSignal(t, oldStarted, "old monitor start")
	a.reconcileMonitors(monitored, nil, nil)
	waitMonitorSignal(t, oldCtx.Done(), "removed monitor cancellation")

	newStarted := make(chan struct{})
	var newCtx context.Context
	var starts atomic.Int32
	runReplacement := func(ctx context.Context, _, _ string) {
		starts.Add(1)
		newCtx = ctx
		close(newStarted)
		<-ctx.Done()
	}
	a.reconcileMonitors(monitored, devices, runReplacement)
	replacement := snapshotMonitors(a, monitored)["keyboard"]
	entries = append(entries, replacement)
	waitMonitorSignal(t, newStarted, "replacement monitor start")
	if replacement == old {
		t.Fatal("replacement reused the old monitor identity")
	}

	releaseOnce.Do(func() { close(releaseOld) })
	waitMonitorSignal(t, old.done, "old monitor exit and registry cleanup")
	if got := snapshotMonitors(a, monitored)["keyboard"]; got != replacement {
		t.Fatal("old monitor exit removed its replacement")
	}
	a.reconcileMonitors(monitored, devices, runReplacement)
	if got := snapshotMonitors(a, monitored); len(got) != 1 || got["keyboard"] != replacement {
		t.Fatal("unchanged discovery replaced the active monitor")
	}
	if starts.Load() != 1 || newCtx.Err() != nil {
		t.Fatal("replacement was duplicated or cancelled")
	}
}

func TestReconcileMonitorsExitCancelsContext(t *testing.T) {
	for _, exit := range []string{"normal return", "open failure", "read EOF"} {
		t.Run(exit, func(t *testing.T) {
			a := newAgent()
			monitored := make(map[string]*monitorEntry)
			path := filepath.Join(t.TempDir(), "input")
			if exit == "read EOF" {
				if err := os.WriteFile(path, nil, 0600); err != nil {
					t.Fatal(err)
				}
			}
			started := make(chan struct{})
			release := make(chan struct{})
			waiterDone := make(chan struct{})
			var releaseOnce sync.Once
			var monitorCtx context.Context
			a.reconcileMonitors(monitored, map[string]string{path: "test input"}, func(ctx context.Context, p, n string) {
				monitorCtx = ctx
				go func() {
					<-ctx.Done()
					close(waiterDone)
				}()
				close(started)
				<-release
				if exit != "normal return" {
					a.monitorDevice(ctx, p, n)
				}
			})
			entry := snapshotMonitors(a, monitored)[path]
			t.Cleanup(func() {
				releaseOnce.Do(func() { close(release) })
				entry.cancel()
				waitMonitorSignal(t, entry.done, "monitor cleanup")
				waitMonitorSignal(t, waiterDone, "cancellation waiter cleanup")
			})
			waitMonitorSignal(t, started, "monitor start")
			releaseOnce.Do(func() { close(release) })
			waitMonitorSignal(t, entry.done, "natural monitor exit")
			if monitorCtx.Err() != context.Canceled {
				t.Fatal("natural exit did not cancel the monitor context")
			}
			waitMonitorSignal(t, waiterDone, "cancellation waiter exit")
			if got := snapshotMonitors(a, monitored); len(got) != 0 {
				t.Fatalf("exited monitor remains registered: %v", got)
			}
		})
	}
}

func reconcileConcurrently(work func(int)) {
	const workers = 16
	start := make(chan struct{})
	var workersDone sync.WaitGroup
	for i := 0; i < workers; i++ {
		workersDone.Add(1)
		go func(i int) {
			defer workersDone.Done()
			<-start
			work(i)
		}(i)
	}
	close(start)
	workersDone.Wait()
}

func TestReconcileMonitorsConcurrentDiscoveryAndExit(t *testing.T) {
	for round := 0; round < 32; round++ {
		a := newAgent()
		monitored := make(map[string]*monitorEntry)
		devices := map[string]string{"keyboard": "keyboard", "mouse": "mouse", "tablet": "tablet"}
		started := make(chan struct{}, 16*len(devices))
		release := make(chan struct{})
		var releaseOnce sync.Once
		var starts atomic.Int32
		run := func(ctx context.Context, _, _ string) {
			starts.Add(1)
			started <- struct{}{}
			select {
			case <-ctx.Done():
			case <-release:
			}
		}
		reconcileConcurrently(func(_ int) {
			a.reconcileMonitors(monitored, devices, run)
		})
		entries := snapshotMonitors(a, monitored)
		t.Cleanup(func() {
			releaseOnce.Do(func() { close(release) })
			a.reconcileMonitors(monitored, nil, nil)
			for _, entry := range entries {
				entry.cancel()
				waitMonitorSignal(t, entry.done, "concurrent monitor cleanup")
			}
		})
		if len(entries) != len(devices) {
			t.Fatalf("round %d: got %d monitors, want %d", round, len(entries), len(devices))
		}
		for range devices {
			waitMonitorSignal(t, started, "concurrent monitor starts")
		}
		if got := starts.Load(); got != int32(len(devices)) {
			t.Fatalf("round %d: started %d monitors, want %d", round, got, len(devices))
		}
		// Natural completion races with concurrent removal/cancellation passes.
		reconcileConcurrently(func(i int) {
			if i == 0 {
				releaseOnce.Do(func() { close(release) })
			}
			a.reconcileMonitors(monitored, nil, nil)
		})
		for _, entry := range entries {
			waitMonitorSignal(t, entry.done, "concurrent monitor exit")
		}
		if got := snapshotMonitors(a, monitored); len(got) != 0 {
			t.Fatalf("round %d: monitors remain after exit: %v", round, got)
		}
	}
}

func TestPipeWireSinkNameUsesSelectedCardAndStableName(t *testing.T) {
	data := []byte(`[
		{"id": 40, "info": {"props": {"media.class": "Audio/Sink", "api.alsa.pcm.card": 1, "node.name": "other-usb-audio"}}},
		{"id": 69, "info": {"props": {"media.class": "Audio/Source", "api.alsa.pcm.card": 2, "node.name": "capture"}}},
		{"id": 70, "info": {"props": {"media.class": "Audio/Sink", "api.alsa.pcm.card": 2, "node.name": "alsa_output.usb-JetKVM", "object.serial": 54229}}}
	]`)
	if got := pipeWireSinkName(data, 2); got != "alsa_output.usb-JetKVM" {
		t.Fatalf("target = %q, want the selected card's stable node name", got)
	}
	if got := pipeWireSinkName(data, 3); got != "" {
		t.Fatalf("absent card target = %q, want no target", got)
	}
	if got := pipeWireSinkName([]byte("invalid JSON"), 2); got != "" {
		t.Fatalf("invalid dump target = %q, want no target", got)
	}
}
