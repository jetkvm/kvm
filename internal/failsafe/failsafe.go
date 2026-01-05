package failsafe

import (
	"os"
	"strings"
	"sync"
	"time"

	"github.com/jetkvm/kvm/internal/logging"
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

type Result struct {
	Active   bool
	Reason   string
	CrashLog string
}

var (
	failsafeOnce        sync.Once
	failsafeResult      Result
	failsafeCrashDumpDir = supervisor.ErrorDumpDir
	failsafeLogger      = logging.GetSubsystemLogger("failsafe")
)

func Check() Result {
	failsafeOnce.Do(func() {
		failsafeResult = checkOnce()
	})
	return failsafeResult
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

func checkOnce() Result {
	// check if the failsafe environment variable is set
	if os.Getenv(failsafeEnv) == "1" {
		return Result{Active: true, Reason: "failsafe_env_set"}
	}

	// check if the failsafe file exists
	if _, err := os.Stat(failsafeFile); err == nil {
		_ = os.Remove(failsafeFile)
		return Result{Active: true, Reason: "failsafe_file_exists"}
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
		return Result{}
	}

	if fi.Mode()&os.ModeSymlink != os.ModeSymlink {
		l.Warn().Msg("last crash log is not a symlink, ignoring")
		return Result{}
	}

	// open the last crash log file and find if it contains the string "panic"
	content, err := os.ReadFile(lastCrashPath)
	if err != nil {
		l.Warn().Err(err).Msg("failed to read last crash log")
		return Result{}
	}

	// unlink the last crash log file
	_ = os.Remove(lastCrashPath)

	count, newest := countRecentCrashDumpsByMtime()
	if count < failsafeCrashThreshold {
		failsafeLogger.Info().
			Int("count", count).
			Time("newest", newest).
			Msg("crash count below failsafe threshold; skipping failsafe")
		return Result{}
	}

	crashLog := string(content)
	if strings.Contains(crashLog, supervisor.FailsafeReasonVideoMaxRestartAttemptsReached) {
		return Result{Active: true, Reason: "video", CrashLog: crashLog}
	}
	if strings.Contains(crashLog, "runtime.cgocall") {
		return Result{Active: true, Reason: "video", CrashLog: crashLog}
	}
	return Result{Active: true, Reason: "unknown", CrashLog: crashLog}
}
