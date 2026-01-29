package logging

import (
	"github.com/pion/logging"
)

// pionLogger dynamically looks up the current scope logger on each call,
// so log level changes via setLogLevel propagate immediately to pion subsystems.
type pionLogger struct {
	scope string
}

func (c pionLogger) Trace(msg string) {
	rootLogger.getLogger(c.scope).Trace().Str("scope", "pion").Msg(msg)
}
func (c pionLogger) Tracef(format string, args ...any) {
	rootLogger.getLogger(c.scope).Trace().Str("scope", "pion").Msgf(format, args...)
}

func (c pionLogger) Debug(msg string) {
	rootLogger.getLogger(c.scope).Debug().Str("scope", "pion").Msg(msg)
}
func (c pionLogger) Debugf(format string, args ...any) {
	rootLogger.getLogger(c.scope).Debug().Str("scope", "pion").Msgf(format, args...)
}
func (c pionLogger) Info(msg string) {
	rootLogger.getLogger(c.scope).Info().Str("scope", "pion").Msg(msg)
}
func (c pionLogger) Infof(format string, args ...any) {
	rootLogger.getLogger(c.scope).Info().Str("scope", "pion").Msgf(format, args...)
}
func (c pionLogger) Warn(msg string) {
	rootLogger.getLogger(c.scope).Warn().Str("scope", "pion").Msg(msg)
}
func (c pionLogger) Warnf(format string, args ...any) {
	rootLogger.getLogger(c.scope).Warn().Str("scope", "pion").Msgf(format, args...)
}
func (c pionLogger) Error(msg string) {
	rootLogger.getLogger(c.scope).Error().Str("scope", "pion").Msg(msg)
}
func (c pionLogger) Errorf(format string, args ...any) {
	rootLogger.getLogger(c.scope).Error().Str("scope", "pion").Msgf(format, args...)
}

// pionLoggerFactory satisfies the interface logging.LoggerFactory.
// Each pion subsystem gets a logger that dynamically resolves the
// current log level from the root logger on every call.
type pionLoggerFactory struct{}

func (c pionLoggerFactory) NewLogger(subsystem string) logging.LeveledLogger {
	return pionLogger{scope: subsystem}
}

var defaultLoggerFactory = &pionLoggerFactory{}

func GetPionDefaultLoggerFactory() logging.LoggerFactory {
	return defaultLoggerFactory
}
