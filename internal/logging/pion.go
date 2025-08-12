package logging

import (
	"github.com/pion/logging"
	"github.com/rs/zerolog"
)

type pionLogger struct {
	logger *zerolog.Logger
}

// Print all messages except trace.
func (c pionLogger) Trace(msg string) {
	c.logger.Trace().Msg(msg)
}
func (c pionLogger) Tracef(format string, args ...interface{}) {
	c.logger.Trace().Msgf(format, args...)
}

func (c pionLogger) Debug(msg string) {
	c.logger.Debug().Msg(msg)
}
func (c pionLogger) Debugf(format string, args ...interface{}) {
	c.logger.Debug().Msgf(format, args...)
}
func (c pionLogger) Info(msg string) {
	c.logger.Info().Msg(msg)
}
func (c pionLogger) Infof(format string, args ...interface{}) {
	c.logger.Info().Msgf(format, args...)
}
func (c pionLogger) Warn(msg string) {
	c.logger.Warn().Msg(msg)
}
func (c pionLogger) Warnf(format string, args ...interface{}) {
	c.logger.Warn().Msgf(format, args...)
}
func (c pionLogger) Error(msg string) {
	c.logger.Error().Msg(msg)
}
func (c pionLogger) Errorf(format string, args ...interface{}) {
	c.logger.Error().Msgf(format, args...)
}

// customLoggerFactory satisfies the interface logging.LoggerFactory
// This allows us to create different loggers per subsystem. So we can
// add custom behavior.
type pionLoggerFactory struct{}

func (c pionLoggerFactory) NewLogger(subsystem string) logging.LeveledLogger {
	logger := rootLogger.getLogger(subsystem).With().
		Str("scope", "pion").
		Str("component", subsystem).
		Logger()

	return pionLogger{logger: &logger}
}

var defaultLoggerFactory = &pionLoggerFactory{}

func GetPionDefaultLoggerFactory() logging.LoggerFactory {
	return defaultLoggerFactory
}
