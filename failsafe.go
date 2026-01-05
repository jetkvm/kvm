package kvm

import (
	"os"
	"strings"
	"sync"
	"time"

	"github.com/jetkvm/kvm/internal/supervisor"
)

const (
	failsafeDefaultLastCrashPath = "/userdata/jetkvm/crashdump/last-crash.log"
	failsafeFile                 = "/userdata/jetkvm/.enablefailsafe"
	failsafeLastCrashEnv         = "JETKVM_LAST_ERROR_PATH"
	failsafeEnv                  = "JETKVM_FORCE_FAILSAFE"
	failsafeCrashWindow          = 10 * time.Minute
	failsafeCrashThreshold       = 3
)

var (
	failsafeOnce       sync.Once
	failsafeCrashLog   = ""
	failsafeModeActive = false
	failsafeModeReason = ""
	failsafeCrashDumpDir = supervisor.ErrorDumpDir
)

type FailsafeModeNotification struct {
	Active bool   `json:"active"`
	Reason string `json:"reason"`
}

func countRecentCrashDumpsByMtime() (int, time.Time) {
	entries, err := os.ReadDir(failsafeCrashDumpDir)
	if err != nil {
		return 0, time.Time{}
	}

	var newest time.Time
	var crashFiles []time.Time

	for _, entry := range entries {
		if entry.IsDir() || entry.Name() == supervisor.ErrorDumpLastFile {
			continue
		}
		if !strings.HasPrefix(entry.Name(), "jetkvm-") || !strings.HasSuffix(entry.Name(), ".log") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		mt := info.ModTime()
		crashFiles = append(crashFiles, mt)
		if mt.After(newest) {
			newest = mt
		}
	}

	if newest.IsZero() {
		return 0, time.Time{}
	}

	cutoff := newest.Add(-failsafeCrashWindow)
	count := 0
	for _, mt := range crashFiles {
		if mt.After(cutoff) || mt.Equal(cutoff) {
			count++
		}
	}
	return count, newest
}

// this function has side effects and can be only executed once
func checkFailsafeReason() {
	failsafeOnce.Do(func() {
		// check if the failsafe environment variable is set
		if os.Getenv(failsafeEnv) == "1" {
			failsafeModeActive = true
			failsafeModeReason = "failsafe_env_set"
			return
		}

		// check if the failsafe file exists
		if _, err := os.Stat(failsafeFile); err == nil {
			failsafeModeActive = true
			failsafeModeReason = "failsafe_file_exists"
			_ = os.Remove(failsafeFile)
			return
		}

		// get the last crash log path from the environment variable
		lastCrashPath := os.Getenv(failsafeLastCrashEnv)
		if lastCrashPath == "" {
			lastCrashPath = failsafeDefaultLastCrashPath
		}

		// check if the last crash log file exists
		l := failsafeLogger.With().Str("path", lastCrashPath).Logger()
		fi, err := os.Lstat(lastCrashPath)
		if err != nil {
			if !os.IsNotExist(err) {
				l.Warn().Err(err).Msg("failed to stat last crash log")
			}
			return
		}

		if fi.Mode()&os.ModeSymlink != os.ModeSymlink {
			l.Warn().Msg("last crash log is not a symlink, ignoring")
			return
		}

		// open the last crash log file and find if it contains the string "panic"
		content, err := os.ReadFile(lastCrashPath)
		if err != nil {
			l.Warn().Err(err).Msg("failed to read last crash log")
			return
		}

		// unlink the last crash log file
		failsafeCrashLog = string(content)
		_ = os.Remove(lastCrashPath)

		count, newest := countRecentCrashDumpsByMtime()
		if count < failsafeCrashThreshold {
			failsafeLogger.Info().
				Int("count", count).
				Time("newest", newest).
				Msg("crash count below failsafe threshold; skipping failsafe")
			return
		}

		// TODO: read the goroutine stack trace and check which goroutine is panicking
		failsafeModeActive = true
		if strings.Contains(failsafeCrashLog, supervisor.FailsafeReasonVideoMaxRestartAttemptsReached) {
			failsafeModeReason = "video"
			return
		}
		if strings.Contains(failsafeCrashLog, "runtime.cgocall") {
			failsafeModeReason = "video"
			return
		} else {
			failsafeModeReason = "unknown"
		}
	})
}

func notifyFailsafeMode(session *Session) {
	if !failsafeModeActive || session == nil {
		return
	}

	jsonRpcLogger.Info().Str("reason", failsafeModeReason).Msg("sending failsafe mode notification")

	writeJSONRPCEvent("failsafeMode", FailsafeModeNotification{
		Active: true,
		Reason: failsafeModeReason,
	}, session)
}
