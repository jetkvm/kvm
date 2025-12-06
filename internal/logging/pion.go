package logging

import (
	"github.com/pion/logging"
	"github.com/rs/zerolog"
)

type pionLogger struct {
	logger func() *zerolog.Logger
}

func (c pionLogger) Trace(msg string) {
	c.logger().Trace().Msg(msg)
}
func (c pionLogger) Tracef(format string, args ...any) {
	c.logger().Trace().Msgf(format, args...)
}

func (c pionLogger) Debug(msg string) {
	c.logger().Debug().Msg(msg)
}
func (c pionLogger) Debugf(format string, args ...any) {
	c.logger().Debug().Msgf(format, args...)
}

func (c pionLogger) Info(msg string) {
	c.logger().Info().Msg(msg)
}
func (c pionLogger) Infof(format string, args ...any) {
	c.logger().Info().Msgf(format, args...)
}

func (c pionLogger) Warn(msg string) {
	c.logger().Warn().Msg(msg)
}
func (c pionLogger) Warnf(format string, args ...any) {
	c.logger().Warn().Msgf(format, args...)
}

func (c pionLogger) Error(msg string) {
	c.logger().Error().Msg(msg)
}
func (c pionLogger) Errorf(format string, args ...any) {
	c.logger().Error().Msgf(format, args...)
}

// customLoggerFactory satisfies the interface logging.LoggerFactory
// This allows us to create different loggers per subsystem. So we can
// add custom behavior.
type pionLoggerFactory struct {
	subcomponent string
}

func (c pionLoggerFactory) NewLogger(subcomponent string) logging.LeveledLogger {
	return pionLogger{logger: func() *zerolog.Logger {
		logger := GetSubsystemLogger("pion").With().Str("subcomponent", subcomponent).Logger()
		return &logger
	}}
}

func GetPionLoggerFactory(subcomponent string) logging.LoggerFactory {
	return &pionLoggerFactory{subcomponent: subcomponent}
}
