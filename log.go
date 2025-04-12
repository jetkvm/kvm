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

type Logger struct {
	l               *zerolog.Logger
	scopeLoggers    map[string]*zerolog.Logger
	scopeLevels     map[string]zerolog.Level
	scopeLevelMutex sync.Mutex

	defaultLogLevelFromEnv    zerolog.Level
	defaultLogLevelFromConfig zerolog.Level
	defaultLogLevel           zerolog.Level
}

const (
	defaultLogLevel = zerolog.ErrorLevel
)

type logOutput struct {
	mu *sync.Mutex
}

func (w *logOutput) Write(p []byte) (n int, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	// TODO: write to file or syslog

	return len(p), nil
}

var (
	consoleLogOutput io.Writer = zerolog.ConsoleWriter{
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
	fileLogOutput    io.Writer = &logOutput{mu: &sync.Mutex{}}
	defaultLogOutput           = zerolog.MultiLevelWriter(consoleLogOutput, fileLogOutput)

	zerologLevels = map[string]zerolog.Level{
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

	rootZerologLogger = zerolog.New(defaultLogOutput).With().
				Str("scope", "jetkvm").
				Timestamp().
				Stack().
				Logger()
	rootLogger = NewLogger(rootZerologLogger)
)

func NewLogger(zerologLogger zerolog.Logger) *Logger {
	return &Logger{
		l:                         &zerologLogger,
		scopeLoggers:              make(map[string]*zerolog.Logger),
		scopeLevels:               make(map[string]zerolog.Level),
		scopeLevelMutex:           sync.Mutex{},
		defaultLogLevelFromEnv:    -2,
		defaultLogLevelFromConfig: -2,
		defaultLogLevel:           defaultLogLevel,
	}
}

func (l *Logger) updateLogLevel() {
	l.scopeLevelMutex.Lock()
	defer l.scopeLevelMutex.Unlock()

	l.scopeLevels = make(map[string]zerolog.Level)

	finalDefaultLogLevel := l.defaultLogLevel

	for name, level := range zerologLevels {
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
			l.defaultLogLevelFromEnv = level

			if finalDefaultLogLevel > level {
				finalDefaultLogLevel = level
			}

			continue
		}

		scopes := strings.Split(strings.ToLower(env), ",")
		for _, scope := range scopes {
			l.scopeLevels[scope] = level
		}
	}

	l.defaultLogLevel = finalDefaultLogLevel
}

func (l *Logger) getScopeLoggerLevel(scope string) zerolog.Level {
	if l.scopeLevels == nil {
		l.updateLogLevel()
	}

	var scopeLevel zerolog.Level
	if l.defaultLogLevelFromConfig != -2 {
		scopeLevel = l.defaultLogLevelFromConfig
	}
	if l.defaultLogLevelFromEnv != -2 {
		scopeLevel = l.defaultLogLevelFromEnv
	}

	// if the scope is not in the map, use the default level from the root logger
	if level, ok := l.scopeLevels[scope]; ok {
		scopeLevel = level
	}

	return scopeLevel
}

func (l *Logger) newScopeLogger(scope string) zerolog.Logger {
	scopeLevel := l.getScopeLoggerLevel(scope)
	logger := l.l.Level(scopeLevel).With().Str("component", scope).Logger()

	return logger
}

func (l *Logger) getLogger(scope string) *zerolog.Logger {
	logger, ok := l.scopeLoggers[scope]
	if !ok || logger == nil {
		scopeLogger := l.newScopeLogger(scope)
		l.scopeLoggers[scope] = &scopeLogger
	}

	return l.scopeLoggers[scope]
}

func (l *Logger) UpdateLogLevel() {
	needUpdate := false

	if config != nil && config.DefaultLogLevel != "" {
		if logLevel, ok := zerologLevels[config.DefaultLogLevel]; ok {
			l.defaultLogLevelFromConfig = logLevel
		} else {
			l.l.Warn().Str("logLevel", config.DefaultLogLevel).Msg("invalid defaultLogLevel from config, using ERROR")
		}

		if l.defaultLogLevelFromConfig != l.defaultLogLevel {
			needUpdate = true
		}
	}

	l.updateLogLevel()

	if needUpdate {
		for scope, logger := range l.scopeLoggers {
			currentLevel := logger.GetLevel()
			targetLevel := l.getScopeLoggerLevel(scope)
			if currentLevel != targetLevel {
				*logger = l.newScopeLogger(scope)
			}
		}
	}
}

func ErrorfL(l *zerolog.Logger, format string, err error, args ...interface{}) error {
	if l == nil {
		l = rootLogger.getLogger("jetkvm")
	}

	l.Error().Err(err).Msgf(format, args...)

	if err == nil {
		return fmt.Errorf(format, args...)
	}

	err_msg := err.Error() + ": %v"
	err_args := append(args, err)

	return fmt.Errorf(err_msg, err_args...)
}

var (
	logger          = rootLogger.getLogger("jetkvm")
	cloudLogger     = rootLogger.getLogger("cloud")
	websocketLogger = rootLogger.getLogger("websocket")
	webrtcLogger    = rootLogger.getLogger("webrtc")
	nativeLogger    = rootLogger.getLogger("native")
	nbdLogger       = rootLogger.getLogger("nbd")
	ntpLogger       = rootLogger.getLogger("ntp")
	jsonRpcLogger   = rootLogger.getLogger("jsonrpc")
	watchdogLogger  = rootLogger.getLogger("watchdog")
	websecureLogger = rootLogger.getLogger("websecure")
	otaLogger       = rootLogger.getLogger("ota")
	serialLogger    = rootLogger.getLogger("serial")
	terminalLogger  = rootLogger.getLogger("terminal")
	displayLogger   = rootLogger.getLogger("display")
	wolLogger       = rootLogger.getLogger("wol")
	usbLogger       = rootLogger.getLogger("usb")
	// external components
	ginLogger = rootLogger.getLogger("gin")
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
