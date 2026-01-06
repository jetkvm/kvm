package failsafe

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

func TestCountRecentCrashDumpsByMtimeThirdOutsideWindow(t *testing.T) {
	// Test scenario: 2 crashes at T and T+5min, then a 3rd crash at T+16min (11 min after 2nd).
	// The 10-minute window from the newest (T+16min) is T+6min to T+16min.
	// Only the 3rd crash falls within this window, so count should be 1.
	tmp := t.TempDir()
	prev := failsafeCrashDumpDir
	failsafeCrashDumpDir = tmp
	t.Cleanup(func() {
		failsafeCrashDumpDir = prev
	})

	base := time.Now().Truncate(time.Second)
	// Crash A at T (16 minutes before the newest)
	writeFileWithMtime(t, tmp, "jetkvm-20250101-000001.log", base)
	// Crash B at T + 5 minutes (11 minutes before the newest)
	writeFileWithMtime(t, tmp, "jetkvm-20250101-000002.log", base.Add(5*time.Minute))
	// Crash C at T + 16 minutes (the newest, 11 minutes after B)
	writeFileWithMtime(t, tmp, "jetkvm-20250101-000003.log", base.Add(16*time.Minute))

	count, newest := countRecentCrashDumpsByMtime()
	// Only crash C should be counted (A and B are outside the 10-minute window from C)
	if count != 1 {
		t.Fatalf("count=%d, want 1 (only newest crash should be in window)", count)
	}
	expectedNewest := base.Add(16 * time.Minute)
	if !newest.Equal(expectedNewest) {
		t.Fatalf("newest=%v, want %v", newest, expectedNewest)
	}
}

func TestCheckOnceClearsSymlink(t *testing.T) {
	tmp := t.TempDir()
	prev := failsafeCrashDumpDir
	failsafeCrashDumpDir = tmp
	t.Cleanup(func() {
		failsafeCrashDumpDir = prev
	})

	lastCrash := filepath.Join(tmp, "last-crash.log")
	t.Setenv(failsafeLastCrashEnv, lastCrash)
	t.Setenv(failsafeEnv, "0")

	base := time.Now().Truncate(time.Second)
	writeFileWithMtime(t, tmp, "jetkvm-20250101-000001.log", base.Add(-2*time.Minute))
	writeFileWithMtime(t, tmp, "jetkvm-20250101-000002.log", base.Add(-1*time.Minute))
	writeFileWithMtime(t, tmp, "jetkvm-20250101-000003.log", base)

	if err := os.Symlink(filepath.Join(tmp, "jetkvm-20250101-000003.log"), lastCrash); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	result := checkOnce()
	if !result.Active {
		t.Fatalf("expected failsafe active")
	}
	if _, err := os.Lstat(lastCrash); !os.IsNotExist(err) {
		t.Fatalf("last-crash.log should be removed, err=%v", err)
	}

	result = checkOnce()
	if result.Active {
		t.Fatalf("expected failsafe inactive without last-crash.log")
	}
}
