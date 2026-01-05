package kvm

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeFileWithMtime(t *testing.T, dir, name string, mt time.Time) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("log"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := os.Chtimes(path, mt, mt); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
}

func TestCountRecentCrashDumpsByMtimeCountsWindow(t *testing.T) {
	tmp := t.TempDir()
	prev := failsafeCrashDumpDir
	failsafeCrashDumpDir = tmp
	t.Cleanup(func() {
		failsafeCrashDumpDir = prev
	})

	base := time.Now().Truncate(time.Second)
	writeFileWithMtime(t, tmp, "jetkvm-20250101-000001.log", base.Add(-5*time.Minute))
	writeFileWithMtime(t, tmp, "jetkvm-20250101-000002.log", base)
	writeFileWithMtime(t, tmp, "jetkvm-20250101-000003.log", base.Add(-20*time.Minute))
	writeFileWithMtime(t, tmp, "last-crash.log", base)
	writeFileWithMtime(t, tmp, "other.log", base)

	count, newest := countRecentCrashDumpsByMtime()
	if count != 2 {
		t.Fatalf("count=%d, want 2", count)
	}
	if !newest.Equal(base) {
		t.Fatalf("newest=%v, want %v", newest, base)
	}
}

func TestCountRecentCrashDumpsByMtimeEmpty(t *testing.T) {
	tmp := t.TempDir()
	prev := failsafeCrashDumpDir
	failsafeCrashDumpDir = tmp
	t.Cleanup(func() {
		failsafeCrashDumpDir = prev
	})

	count, newest := countRecentCrashDumpsByMtime()
	if count != 0 {
		t.Fatalf("count=%d, want 0", count)
	}
	if !newest.IsZero() {
		t.Fatalf("newest=%v, want zero time", newest)
	}
}

func TestCountRecentCrashDumpsByMtimeExactCutoff(t *testing.T) {
	tmp := t.TempDir()
	prev := failsafeCrashDumpDir
	failsafeCrashDumpDir = tmp
	t.Cleanup(func() { failsafeCrashDumpDir = prev })

	base := time.Now().Truncate(time.Second)
	writeFileWithMtime(t, tmp, "jetkvm-20250101-000001.log", base)
	writeFileWithMtime(t, tmp, "jetkvm-20250101-000002.log", base.Add(-failsafeCrashWindow)) // exactly at cutoff

	count, _ := countRecentCrashDumpsByMtime()
	if count != 2 {
		t.Fatalf("count=%d, want 2 (file at exact cutoff should be included)", count)
	}
}

func TestCountRecentCrashDumpsByMtimeNonexistentDir(t *testing.T) {
	prev := failsafeCrashDumpDir
	failsafeCrashDumpDir = "/nonexistent/path"
	t.Cleanup(func() { failsafeCrashDumpDir = prev })

	count, newest := countRecentCrashDumpsByMtime()
	if count != 0 || !newest.IsZero() {
		t.Fatalf("expected (0, zero time) for nonexistent dir, got (%d, %v)", count, newest)
	}
}
