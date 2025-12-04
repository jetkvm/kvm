package logging

import (
	"github.com/rs/zerolog"
)

var (
	rootLogger = NewLogger(
		zerolog.New(defaultLogOutput).
			With().
			Str("scope", "jetkvm").
			Timestamp().
			Logger())
)

func UpdateConfigLogLevel(logLevel string) {
	rootLogger.UpdateConfigLogLevel(logLevel)
}

func GetSubsystemLogger(subsystem string) *Context {
	return NewContext(rootLogger.getLogger(subsystem))
}
