package logging

import (
	"os"
	"time"

	"github.com/rs/zerolog"
)

var (
	rootZerologLogger = zerolog.New(defaultLogOutput).With().
				Str("scope", "jetkvm").
				Timestamp().
				Stack().
				Logger()
	rootLogger = NewLogger(rootZerologLogger)
)

func GetRootLogger() *Logger {
	return rootLogger
}

func GetSubsystemLogger(subsystem string) *zerolog.Logger {
	return rootLogger.getLogger(subsystem)
}

// ReconfigureToStderr reinitializes the root logger to write to stderr instead of stdout.
// This is used by child processes (like the native subprocess) where stdout is used for IPC
// and logs need to go to stderr to be visible.
func ReconfigureToStderr() {
	stderrOutput := zerolog.ConsoleWriter{
		Out:           os.Stderr,
		TimeFormat:    time.RFC3339,
		PartsOrder:    []string{"time", "level", "scope", "component", "message"},
		FieldsExclude: []string{"scope", "component"},
	}

	rootZerologLogger = zerolog.New(stderrOutput).With().
		Str("scope", "jetkvm").
		Timestamp().
		Stack().
		Logger()
	rootLogger = NewLogger(rootZerologLogger)
}
