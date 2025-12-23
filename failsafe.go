package kvm

import (
	"os"
	"strings"
	"sync"

	"github.com/jetkvm/kvm/internal/supervisor"
)

const (
	failsafeDefaultLastCrashPath = "/userdata/jetkvm/crashdump/last-crash.log"
	failsafeFile                 = "/userdata/jetkvm/.enablefailsafe"
	failsafeLastCrashEnv         = "JETKVM_LAST_ERROR_PATH"
	failsafeEnv                  = "JETKVM_FORCE_FAILSAFE"
)

var (
	failsafeOnce       sync.Once
	failsafeCrashLog   = ""
	failsafeModeActive = false
	failsafeModeReason = ""
)

type FailsafeModeNotification struct {
	Active bool   `json:"active"`
	Reason string `json:"reason"`
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
