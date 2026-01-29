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

	// Config-based overrides (highest priority)
	configSubsystemLevels map[string]zerolog.Level
	configGlobalLevel     zerolog.Level
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

	// subsystemDefaultLevels defines default log levels for specific subsystems
	// that should always log at a certain level regardless of the global default.
	subsystemDefaultLevels = map[string]zerolog.Level{
		"diagnostics": zerolog.InfoLevel,
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
		configSubsystemLevels:     make(map[string]zerolog.Level),
		configGlobalLevel:         -2, // -2 means unset
	}
}

// updateLogLevelLocked reads env vars and populates scopeLevels.
// Must be called with scopeLevelMutex held.
func (l *Logger) updateLogLevelLocked() {
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

		scopes := strings.SplitSeq(strings.ToLower(env), ",")
		for scope := range scopes {
			l.scopeLevels[scope] = level
		}
	}

	l.defaultLogLevel = finalDefaultLogLevel
}

// getScopeLoggerLevel returns the log level for a scope with proper synchronization.
// Acquires scopeLevelMutex internally. Callers that already hold the lock
// must use getScopeLoggerLevelLocked instead.
func (l *Logger) getScopeLoggerLevel(scope string) zerolog.Level {
	l.scopeLevelMutex.Lock()
	defer l.scopeLevelMutex.Unlock()
	return l.getScopeLoggerLevelLocked(scope)
}

// newScopeLogger creates a scoped logger.
// Must be called with scopeLevelMutex held.
func (l *Logger) newScopeLogger(scope string) zerolog.Logger {
	scopeLevel := l.getScopeLoggerLevelLocked(scope)
	return l.l.Level(scopeLevel).With().Str("component", scope).Logger()
}

func (l *Logger) getLogger(scope string) *zerolog.Logger {
	l.scopeLevelMutex.Lock()
	defer l.scopeLevelMutex.Unlock()

	logger, ok := l.scopeLoggers[scope]
	if !ok || logger == nil {
		scopeLogger := l.newScopeLogger(scope)
		l.scopeLoggers[scope] = &scopeLogger
	}
	return l.scopeLoggers[scope]
}

func (l *Logger) UpdateLogLevel(configDefaultLogLevel string) {
	l.scopeLevelMutex.Lock()
	defer l.scopeLevelMutex.Unlock()

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

	l.updateLogLevelLocked()

	if needUpdate {
		l.refreshAllLoggersLocked()
	}
}

// SetSubsystemLevels parses and applies log level overrides from config.
// Format: "LEVEL" for global, or "subsystem:LEVEL,subsystem:LEVEL,..." for per-subsystem.
// Examples: "DEBUG", "rdp:TRACE,vnc:DEBUG", "INFO,rdp:TRACE"
func (l *Logger) SetSubsystemLevels(overrides string) {
	l.scopeLevelMutex.Lock()

	// Ensure scopeLevels is initialized to avoid deadlock in refreshAllLoggers
	if l.scopeLevels == nil {
		l.scopeLevels = make(map[string]zerolog.Level)
	}

	// Reset config-based overrides
	l.configSubsystemLevels = make(map[string]zerolog.Level)
	l.configGlobalLevel = -2

	if overrides == "" {
		l.refreshAllLoggersLocked()
		l.scopeLevelMutex.Unlock()
		return
	}

	// Parse the overrides string
	parts := strings.Split(overrides, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		// Check if it's a subsystem:level pair
		if strings.Contains(part, ":") {
			kv := strings.SplitN(part, ":", 2)
			if len(kv) == 2 {
				subsystem := strings.TrimSpace(strings.ToLower(kv[0]))
				levelStr := strings.TrimSpace(strings.ToUpper(kv[1]))
				if level, ok := zerologLevels[levelStr]; ok {
					l.configSubsystemLevels[subsystem] = level
				}
			}
		} else {
			// It's a global level
			levelStr := strings.ToUpper(part)
			if level, ok := zerologLevels[levelStr]; ok {
				l.configGlobalLevel = level
			}
		}
	}

	l.refreshAllLoggersLocked()
	l.scopeLevelMutex.Unlock()
}

// refreshAllLoggersLocked updates all existing scope loggers to use the new levels.
// Must be called with scopeLevelMutex held.
func (l *Logger) refreshAllLoggersLocked() {
	for scope, logger := range l.scopeLoggers {
		currentLevel := logger.GetLevel()
		targetLevel := l.getScopeLoggerLevelLocked(scope)
		if currentLevel != targetLevel {
			*logger = l.newScopeLogger(scope)
		}
	}
}

// getScopeLoggerLevelLocked returns the log level for a scope.
// Must be called with scopeLevelMutex held.
func (l *Logger) getScopeLoggerLevelLocked(scope string) zerolog.Level {
	// Priority (from lowest to highest):
	// 1. Hardcoded global default
	// 2. Hardcoded subsystem default
	// 3. Env var global
	// 4. Env var subsystem override
	// 5. Config global level
	// 6. Config subsystem override (highest)

	// Start with hardcoded global default
	scopeLevel := l.defaultLogLevel
	if l.defaultLogLevelFromConfig != -2 {
		scopeLevel = l.defaultLogLevelFromConfig
	}

	// Check if this subsystem has a hardcoded default level
	if subsystemLevel, ok := subsystemDefaultLevels[scope]; ok {
		// Use the more verbose level (lower value = more verbose)
		if subsystemLevel < scopeLevel {
			scopeLevel = subsystemLevel
		}
	}

	// Apply env var global level
	if l.defaultLogLevelFromEnv != -2 {
		scopeLevel = l.defaultLogLevelFromEnv
	}

	// Apply env var subsystem override
	if l.scopeLevels != nil {
		if level, ok := l.scopeLevels[scope]; ok {
			scopeLevel = level
		}
	}

	// Apply config global level (takes priority over env vars)
	if l.configGlobalLevel != -2 {
		scopeLevel = l.configGlobalLevel
	}

	// Apply config subsystem override (highest priority)
	if level, ok := l.configSubsystemLevels[scope]; ok {
		scopeLevel = level
	}

	return scopeLevel
}

// GetSubsystemLevels returns current effective log levels for all registered subsystems.
func (l *Logger) GetSubsystemLevels() map[string]string {
	l.scopeLevelMutex.Lock()
	defer l.scopeLevelMutex.Unlock()

	// Ensure scopeLevels is initialized to avoid nil check issues
	if l.scopeLevels == nil {
		l.scopeLevels = make(map[string]zerolog.Level)
	}

	result := make(map[string]string)
	for scope := range l.scopeLoggers {
		level := l.getScopeLoggerLevelLocked(scope)
		result[scope] = levelToString(level)
	}
	return result
}

// GetAvailableSubsystems returns a list of all registered subsystem names.
func (l *Logger) GetAvailableSubsystems() []string {
	l.scopeLevelMutex.Lock()
	defer l.scopeLevelMutex.Unlock()

	subsystems := make([]string, 0, len(l.scopeLoggers))
	for scope := range l.scopeLoggers {
		subsystems = append(subsystems, scope)
	}
	return subsystems
}

// GetAvailableLevels returns the list of available log level names.
func (l *Logger) GetAvailableLevels() []string {
	return []string{"DISABLE", "ERROR", "WARN", "INFO", "DEBUG", "TRACE"}
}

// GetConfigOverrides returns the current log level overrides string for UI display.
func (l *Logger) GetConfigOverrides() string {
	l.scopeLevelMutex.Lock()
	defer l.scopeLevelMutex.Unlock()

	var parts []string

	// Add global level if set
	if l.configGlobalLevel != -2 {
		parts = append(parts, levelToString(l.configGlobalLevel))
	}

	// Add subsystem overrides
	for subsystem, level := range l.configSubsystemLevels {
		parts = append(parts, subsystem+":"+levelToString(level))
	}

	return strings.Join(parts, ",")
}

// levelToString converts a zerolog.Level to its string representation
func levelToString(level zerolog.Level) string {
	switch level {
	case zerolog.Disabled:
		return "DISABLE"
	case zerolog.PanicLevel:
		return "PANIC"
	case zerolog.FatalLevel:
		return "FATAL"
	case zerolog.ErrorLevel:
		return "ERROR"
	case zerolog.WarnLevel:
		return "WARN"
	case zerolog.InfoLevel:
		return "INFO"
	case zerolog.DebugLevel:
		return "DEBUG"
	case zerolog.TraceLevel:
		return "TRACE"
	default:
		return "ERROR"
	}
}
