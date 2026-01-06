package logging

import "github.com/rs/zerolog"

var (
	rootLogger = NewLogger(
		zerolog.New(defaultLogOutput).
			With().
			Str("scope", "jetkvm").
			Timestamp().
			Logger())
)

func GetRootLogger() *Logger {
	return rootLogger
}

func GetSubsystemLogger(subsystem string) *zerolog.Logger {
	return rootLogger.getLogger(subsystem)
}
