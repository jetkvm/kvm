package logging

import "github.com/rs/zerolog"

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

// RegisterDerivedLogger tracks a .With().Logger() copy so its level stays in sync
// with its parent scope when dynamic log levels change.
func RegisterDerivedLogger(scope string, logger *zerolog.Logger) {
	rootLogger.registerDerivedLogger(scope, logger)
}

// UnregisterDerivedLogger removes a previously registered derived logger.
func UnregisterDerivedLogger(logger *zerolog.Logger) {
	rootLogger.unregisterDerivedLogger(logger)
}
