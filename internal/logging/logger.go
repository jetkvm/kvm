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

func (l *Logger) UpdateLogLevel(configDefaultLogLevel string) {
	needUpdate := false

	if configDefaultLogLevel != "" {
		if logLevel, ok := zerologLevels[configDefaultLogLevel]; ok {
			l.defaultLogLevelFromConfig = logLevel
		} else {
			l.l.Warn().Str("logLevel", configDefaultLogLevel).Msg("invalid defaultLogLevel from config, using ERROR")
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
