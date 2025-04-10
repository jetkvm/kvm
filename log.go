package kvm

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/pion/logging"
	"github.com/rs/zerolog"
)

var (
	defaultLogOutput io.Writer = zerolog.ConsoleWriter{
		Out:           os.Stdout,
		TimeFormat:    time.RFC3339,
		PartsOrder:    []string{"time", "level", "scope", "component", "message"},
		FieldsExclude: []string{"scope", "component"},
		FormatPartValueByName: func(value interface{}, name string) string {
			val := fmt.Sprintf("%s", value)
			if name == "component" {
				if value == nil {
					return "-"
				}
			}
			return val
		},
	}
	defaultLogLevel = zerolog.ErrorLevel
	rootLogger      = zerolog.New(defaultLogOutput).With().
			Str("scope", "jetkvm").
			Timestamp().
			Stack().
			Logger()
)

var (
	scopeLevels     map[string]zerolog.Level
	scopeLevelMutex = sync.Mutex{}
)

var (
	logger          = getLogger("jetkvm")
	cloudLogger     = getLogger("cloud")
	websocketLogger = getLogger("websocket")
	nativeLogger    = getLogger("native")
	ntpLogger       = getLogger("ntp")
	displayLogger   = getLogger("display")
	usbLogger       = getLogger("usb")
	ginLogger       = getLogger("gin")
	websecureLogger = getLogger("websecure")
)

func updateLogLevel() {
	scopeLevelMutex.Lock()
	defer scopeLevelMutex.Unlock()

	logLevels := map[string]zerolog.Level{
		"DISABLE": zerolog.Disabled,
		"NOLEVEL": zerolog.NoLevel,
		"PANIC":   zerolog.PanicLevel,
		"FATAL":   zerolog.FatalLevel,
		"ERROR":   zerolog.ErrorLevel,
		"WARN":    zerolog.WarnLevel,
		"INFO":    zerolog.InfoLevel,
		"DEBUG":   zerolog.DebugLevel,
		"TRACE":   zerolog.TraceLevel,
	}

	scopeLevels = make(map[string]zerolog.Level)

	for name, level := range logLevels {
		env := os.Getenv(fmt.Sprintf("JETKVM_LOG_%s", name))

		if env == "" {
			env = os.Getenv(fmt.Sprintf("PION_LOG_%s", name))
		}

		if env == "" {
			env = os.Getenv(fmt.Sprintf("PIONS_LOG_%s", name))
		}

		if env == "" {
			continue
		}

		if strings.ToLower(env) == "all" {
			if defaultLogLevel > level {
				defaultLogLevel = level
			}

			continue
		}

		scopes := strings.Split(strings.ToLower(env), ",")
		for _, scope := range scopes {
			scopeLevels[scope] = level
		}
	}
}

func getLogger(scope string) zerolog.Logger {
	if scopeLevels == nil {
		updateLogLevel()
	}

	l := rootLogger.With().Str("component", scope).Logger()

	// if the scope is not in the map, use the default level from the root logger
	if level, ok := scopeLevels[scope]; ok {
		return l.Level(level)
	}

	return l.Level(defaultLogLevel)
}

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
	logger := getLogger(subsystem).With().
		Str("scope", "pion").
		Str("component", subsystem).
		Logger()

	return pionLogger{logger: &logger}
}

var defaultLoggerFactory = &pionLoggerFactory{}
