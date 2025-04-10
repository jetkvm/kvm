// This is an internal package to patch the Pion logging library to use the
// zerolog instead of the log package.
package logging

import (
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

var defaultOutput io.Writer = zerolog.ConsoleWriter{
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

func SetDefaultOutput(output io.Writer) {
	defaultOutput = output
}

// Use this abstraction to ensure thread-safe access to the logger's io.Writer.
// (which could change at runtime).
type loggerWriter struct {
	sync.RWMutex
	output io.Writer
}

func (lw *loggerWriter) SetOutput(output io.Writer) {
	lw.Lock()
	defer lw.Unlock()
	lw.output = output
}

func (lw *loggerWriter) Write(data []byte) (int, error) {
	lw.RLock()
	defer lw.RUnlock()

	return lw.output.Write(data)
}

type zerologEventLogger func() *zerolog.Event

// DefaultLeveledLogger encapsulates functionality for providing logging at.
// user-defined levels.
type DefaultLeveledLogger struct {
	level  LogLevel
	writer *zerolog.Logger
	trace  zerologEventLogger
	debug  zerologEventLogger
	info   zerologEventLogger
	warn   zerologEventLogger
	err    zerologEventLogger
}

func (ll *DefaultLeveledLogger) GetLogger() *zerolog.Logger {
	return ll.writer
}

// WithTraceLogger is a chainable configuration function which sets the
// Trace-level logger.
func (ll *DefaultLeveledLogger) WithTraceLogger(log zerologEventLogger) *DefaultLeveledLogger {
	ll.trace = log

	return ll
}

// WithDebugLogger is a chainable configuration function which sets the
// Debug-level logger.
func (ll *DefaultLeveledLogger) WithDebugLogger(log zerologEventLogger) *DefaultLeveledLogger {
	ll.debug = log

	return ll
}

// WithInfoLogger is a chainable configuration function which sets the
// Info-level logger.
func (ll *DefaultLeveledLogger) WithInfoLogger(log zerologEventLogger) *DefaultLeveledLogger {
	ll.info = log

	return ll
}

// WithWarnLogger is a chainable configuration function which sets the
// Warn-level logger.
func (ll *DefaultLeveledLogger) WithWarnLogger(log zerologEventLogger) *DefaultLeveledLogger {
	ll.warn = log

	return ll
}

// WithErrorLogger is a chainable configuration function which sets the
// Error-level logger.
func (ll *DefaultLeveledLogger) WithErrorLogger(log zerologEventLogger) *DefaultLeveledLogger {
	ll.err = log

	return ll
}

// WithOutput is a chainable configuration function which sets the logger's
// logging output to the supplied io.Writer.
func (ll *DefaultLeveledLogger) WithOutput(output io.Writer) *DefaultLeveledLogger {
	ll.writer.Output(output)

	return ll
}

// SetLevel sets the logger's logging level.
func (ll *DefaultLeveledLogger) SetLevel(newLevel LogLevel) {
	ll.level.Set(newLevel)
}

// Trace emits the preformatted message if the logger is at or below LogLevelTrace.
func (ll *DefaultLeveledLogger) Trace(msg string) {
	ll.trace().Msgf(msg)
}

// Tracef formats and emits a message if the logger is at or below LogLevelTrace.
func (ll *DefaultLeveledLogger) Tracef(format string, args ...interface{}) {
	ll.trace().Msgf(format, args...)
}

// Debug emits the preformatted message if the logger is at or below LogLevelDebug.
func (ll *DefaultLeveledLogger) Debug(msg string) {
	ll.debug().Msgf(msg)
}

// Debugf formats and emits a message if the logger is at or below LogLevelDebug.
func (ll *DefaultLeveledLogger) Debugf(format string, args ...interface{}) {
	ll.debug().Msgf(format, args...)
}

// Info emits the preformatted message if the logger is at or below LogLevelInfo.
func (ll *DefaultLeveledLogger) Info(msg string) {
	ll.info().Msgf(msg)
}

// Infof formats and emits a message if the logger is at or below LogLevelInfo.
func (ll *DefaultLeveledLogger) Infof(format string, args ...interface{}) {
	ll.info().Msgf(format, args...)
}

// Warn emits the preformatted message if the logger is at or below LogLevelWarn.
func (ll *DefaultLeveledLogger) Warn(msg string) {
	ll.warn().Msgf(msg)
}

// Warnf formats and emits a message if the logger is at or below LogLevelWarn.
func (ll *DefaultLeveledLogger) Warnf(format string, args ...interface{}) {
	ll.warn().Msgf(format, args...)
}

// Error emits the preformatted message if the logger is at or below LogLevelError.
func (ll *DefaultLeveledLogger) Error(msg string) {
	ll.err().Msgf(msg)
}

// Errorf formats and emits a message if the logger is at or below LogLevelError.
func (ll *DefaultLeveledLogger) Errorf(format string, args ...interface{}) {
	ll.err().Msgf(format, args...)
}

// NewDefaultLeveledLoggerForScope returns a configured LeveledLogger.
func NewDefaultLeveledLoggerForScope(scope string, level LogLevel, writer io.Writer) *DefaultLeveledLogger {
	if writer == nil {
		writer = defaultOutput
	}

	z := zerolog.New(writer).Level(toZerologLevel(level)).With().Timestamp()

	// scope will be changed to the component name if it's from the pion library
	_, file, _, _ := runtime.Caller(2)
	if strings.Contains(file, "github.com/pion/") {
		z = z.Str("scope", "pion").Str("component", scope)
	} else {
		z = z.Str("scope", scope)
	}

	zerologWriter := z.Logger()

	logger := &DefaultLeveledLogger{
		writer: &zerologWriter,
		level:  level,
	}

	return logger.
		WithTraceLogger(zerologWriter.Trace).
		WithDebugLogger(zerologWriter.Debug).
		WithInfoLogger(zerologWriter.Info).
		WithWarnLogger(zerologWriter.Warn).
		WithErrorLogger(zerologWriter.Error)
}

// DefaultLoggerFactory define levels by scopes and creates new DefaultLeveledLogger.
type DefaultLoggerFactory struct {
	Writer          io.Writer
	DefaultLogLevel LogLevel
	ScopeLevels     map[string]LogLevel
}

// NewDefaultLoggerFactory creates a new DefaultLoggerFactory.
func NewDefaultLoggerFactory() *DefaultLoggerFactory {
	factory := DefaultLoggerFactory{}
	factory.DefaultLogLevel = LogLevelError
	factory.ScopeLevels = make(map[string]LogLevel)
	factory.Writer = defaultOutput

	logLevels := map[string]LogLevel{
		"DISABLE": LogLevelDisabled,
		"ERROR":   LogLevelError,
		"WARN":    LogLevelWarn,
		"INFO":    LogLevelInfo,
		"DEBUG":   LogLevelDebug,
		"TRACE":   LogLevelTrace,
	}

	for name, level := range logLevels {
		env := os.Getenv(fmt.Sprintf("PION_LOG_%s", name))

		if env == "" {
			env = os.Getenv(fmt.Sprintf("PIONS_LOG_%s", name))
		}

		if env == "" {
			continue
		}

		if strings.ToLower(env) == "all" {
			if factory.DefaultLogLevel < level {
				factory.DefaultLogLevel = level
			}

			continue
		}

		scopes := strings.Split(strings.ToLower(env), ",")
		for _, scope := range scopes {
			factory.ScopeLevels[scope] = level
		}
	}

	return &factory
}

// NewLogger returns a configured LeveledLogger for the given, argsscope.
func (f *DefaultLoggerFactory) NewLogger(scope string) LeveledLogger {
	logLevel := f.DefaultLogLevel
	if f.ScopeLevels != nil {
		scopeLevel, found := f.ScopeLevels[scope]

		if found {
			logLevel = scopeLevel
		}
	}

	return NewDefaultLeveledLoggerForScope(scope, logLevel, f.Writer)
}
