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
