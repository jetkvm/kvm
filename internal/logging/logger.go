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

	loggerMutex  sync.Mutex
	scopeLevels  map[string]zerolog.Level
	scopeLoggers map[string]*zerolog.Logger

	defaultLogLevelFromEnv    zerolog.Level
	defaultLogLevelFromConfig zerolog.Level
	defaultLogLevel           zerolog.Level
}

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
		"PANIC":   zerolog.PanicLevel,
		"FATAL":   zerolog.FatalLevel,
		"ERROR":   zerolog.ErrorLevel,
		"WARN":    zerolog.WarnLevel,
		"INFO":    zerolog.InfoLevel,
		"DEBUG":   zerolog.DebugLevel,
		"TRACE":   zerolog.TraceLevel,
		"UNSET":   -2,
	}
)

func NewLogger(zerologLogger zerolog.Logger) *Logger {
	return &Logger{
		defaultLogger:             &zerologLogger,
		loggerMutex:               sync.Mutex{},
		scopeLevels:               make(map[string]zerolog.Level),
		scopeLoggers:              make(map[string]*zerolog.Logger),
		defaultLogLevelFromEnv:    -2,
		defaultLogLevelFromConfig: -2,
		defaultLogLevel:           zerolog.ErrorLevel,
	}
}

func (l *Logger) updateLogLevels(newConfigLevel zerolog.Level) {
	loggingContext := l.defaultLogger.With().Interface("newConfigLevel", newConfigLevel)
	logger := loggingContext.Logger()

	l.defaultLogLevelFromConfig = newConfigLevel
	l.scopeLevels = make(map[string]zerolog.Level)

	for name, envLevel := range zerologLevels {
		env := strings.ToLower(os.Getenv(fmt.Sprintf("JETKVM_LOG_%s", name)))

		if env != "" {
			loggingContext := l.defaultLogger.With().Str("name", name).Str("env", env).Interface("envLevel", envLevel)
			logger := loggingContext.Logger()

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

	l.defaultLogLevel = zerolog.ErrorLevel
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
	if l.defaultLogLevelFromConfig != -2 {
		return l.defaultLogLevelFromConfig
	}

	if l.defaultLogLevelFromEnv != -2 {
		return l.defaultLogLevelFromEnv
	}

	return l.defaultLogLevel
}

func (l *Logger) getLogger(scope string) *zerolog.Logger {
	var ok bool
	var logger *zerolog.Logger
	if logger, ok = l.scopeLoggers[scope]; !ok || logger == nil {
		l.loggerMutex.Lock()
		defer l.loggerMutex.Unlock()

		if logger, ok = l.scopeLoggers[scope]; !ok || logger == nil {
			scopeLevel := l.getScopeLoggerLevel(scope)
			newLogger := l.defaultLogger.Level(scopeLevel).With().Str("component", scope).Logger()
			l.scopeLoggers[scope] = &newLogger
			logger = &newLogger
		}
	}

	return logger
}

func (l *Logger) UpdateConfigLogLevel(logLevelConfig string) {
	var newConfigLevel zerolog.Level

	logLevelConfig = strings.ToUpper(logLevelConfig)
	loggingContext := l.defaultLogger.With().Str("logLevelConfig", logLevelConfig)
	logger := loggingContext.Logger()

	if logLevelConfig != "" {
		if logLevel, ok := zerologLevels[logLevelConfig]; ok {
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
