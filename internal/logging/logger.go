package logging

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

type Logger struct {
	defaultLogger *zerolog.Logger
	scopeLoggers  map[string]*zerolog.Logger
	scopeLevels   map[string]zerolog.Level
	loggerMutex   sync.Mutex

	defaultLogLevelFromEnv    zerolog.Level
	defaultLogLevelFromConfig zerolog.Level
	defaultLogLevel           zerolog.Level
}

const (
	defaultLogLevel = zerolog.ErrorLevel
	unset           = -2
)

type logOutput struct {
	mu *sync.Mutex
}

func (w *logOutput) Write(p []byte) (n int, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	// TODO: write to file or syslog
	if sseServer != nil {
		// use a goroutine to avoid blocking the Write method
		go func() {
			sseServer.Message <- string(p)
		}()
	}
	return len(p), nil
}

var (
	consoleLogOutput io.Writer = zerolog.ConsoleWriter{
		Out:           os.Stdout,
		TimeFormat:    time.RFC3339,
		PartsOrder:    []string{"time", "level", "scope", "component", "message"},
		FieldsExclude: []string{"scope", "component"},
		FormatPartValueByName: func(value any, name string) string {
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
)

func NewLogger(zerologLogger zerolog.Logger) *Logger {
	return &Logger{
		defaultLogger:             &zerologLogger,
		scopeLoggers:              make(map[string]*zerolog.Logger),
		scopeLevels:               make(map[string]zerolog.Level),
		loggerMutex:               sync.Mutex{},
		defaultLogLevelFromEnv:    unset,
		defaultLogLevelFromConfig: unset,
		defaultLogLevel:           defaultLogLevel,
	}
}

func (l *Logger) updateLogLevels(newConfigLevel zerolog.Level) {
	loggingContext := l.defaultLogger.With().Interface("newConfigLevel", newConfigLevel)
	logger := loggingContext.Logger()

	l.defaultLogLevelFromConfig = newConfigLevel
	l.scopeLevels = make(map[string]zerolog.Level)

	for name, envLevel := range zerologLevels {
		env := os.Getenv(fmt.Sprintf("JETKVM_LOG_%s", name))

		if env != "" {
			env = strings.ToLower(env)
			loggingContext = l.defaultLogger.With().Str("name", name).Str("env", env).Interface("envLevel", envLevel)
			logger = loggingContext.Logger()

			if env == "all" {
				logger.Info().Msg("setting log level for ALL scopes from environment")
				l.defaultLogLevelFromEnv = envLevel
			} else {
				// if not "all", parse as comma-separated list of scopes
				scopes := strings.SplitSeq(env, ",")
				for scope := range scopes {
					logger.Info().Msgf("setting log level for scope %s from environment", scope)
					l.scopeLevels[scope] = envLevel
				}
			}
		}
	}

	l.defaultLogLevel = defaultLogLevel
	logger.Info().Msgf("default log level starts at %v", l.defaultLogLevelFromEnv)

	if l.defaultLogLevel > l.defaultLogLevelFromEnv {
		logger.Info().Msgf("default log level from env %v", l.defaultLogLevelFromEnv)
		l.defaultLogLevel = l.defaultLogLevelFromEnv
	}

	if l.defaultLogLevel > l.defaultLogLevelFromConfig {
		logger.Info().Msgf("default log level from config %v", l.defaultLogLevelFromConfig)
		l.defaultLogLevel = l.defaultLogLevelFromConfig
	}

	logger.Info().Msgf("default log level %v", l.defaultLogLevel)
}

func (l *Logger) getScopeLoggerLevel(scope string) zerolog.Level {
	if level, ok := l.scopeLevels[scope]; ok {
		return level
	}

	// if the scope is not in the map, use the default level from the root logger
	if l.defaultLogLevelFromConfig != unset {
		return l.defaultLogLevelFromConfig
	}

	if l.defaultLogLevelFromEnv != unset {
		return l.defaultLogLevelFromEnv
	}

	return l.defaultLogLevel
}

func (l *Logger) getLogger(scope string) *zerolog.Logger {
	if logger, ok := l.scopeLoggers[scope]; ok && logger != nil {
		return logger
	}

	l.loggerMutex.Lock()
	defer l.loggerMutex.Unlock()

	// double-check after acquiring the lock
	if logger, ok := l.scopeLoggers[scope]; ok && logger != nil {
		return logger
	}

	scopeLevel := l.getScopeLoggerLevel(scope)
	logger := l.defaultLogger.Level(scopeLevel).With().Str("component", scope).Logger()
	l.scopeLoggers[scope] = &logger
	return &logger
}

func (l *Logger) UpdateConfigLogLevel(configDefaultLogLevel string) {
	var newConfigLevel zerolog.Level

	configDefaultLogLevel = strings.ToUpper(configDefaultLogLevel)
	loggingContext := l.defaultLogger.With().Str("configDefaultLogLevel", configDefaultLogLevel)
	logger := loggingContext.Logger()

	if configDefaultLogLevel != "" {
		if logLevel, ok := zerologLevels[configDefaultLogLevel]; ok {
			logger.Debug().Msgf("setting config log level to %v", logLevel)
			newConfigLevel = logLevel
		} else {
			logger.Warn().Msg("invalid log level from config")
			return
		}
	} else {
		newConfigLevel = -2
	}

	l.loggerMutex.Lock()
	defer l.loggerMutex.Unlock()

	l.updateLogLevels(newConfigLevel)

	for scope, logger := range l.scopeLoggers {
		currentLevel := logger.GetLevel()
		targetLevel := l.getScopeLoggerLevel(scope)

		if currentLevel != targetLevel {
			debugLogger := loggingContext.Stringer("currentLevel", currentLevel).Stringer("targetLevel", targetLevel).Logger()
			debugLogger.Info().Msgf("updating log level for scope %s", scope)

			// update the chosen level by replacing the logger
			// with a new one at the target level
			newLogger := logger.Level(targetLevel).With().Logger()
			l.scopeLoggers[scope] = &newLogger
		}
	}
}
