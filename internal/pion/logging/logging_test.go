// SPDX-FileCopyrightText: 2023 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package logging_test

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/pion/logging"
	"github.com/stretchr/testify/assert"
)

func testNoDebugLevel(t *testing.T, logger *logging.DefaultLeveledLogger) {
	t.Helper()

	var outBuf bytes.Buffer
	logger.WithOutput(&outBuf)

	logger.Debug("this shouldn't be logged")
	assert.GreaterOrEqual(t, 0, outBuf.Len(), "Debug was logged when it shouldn't have been")

	logger.Debugf("this shouldn't be logged")
	assert.GreaterOrEqual(t, 0, outBuf.Len(), "Debug was logged when it shouldn't have been")
}

func testDebugLevel(t *testing.T, logger *logging.DefaultLeveledLogger) {
	t.Helper()

	var outBuf bytes.Buffer
	logger.WithOutput(&outBuf)

	dbgMsg := "this is a debug message"
	logger.Debug(dbgMsg)
	assert.Truef(t, strings.Contains(outBuf.String(), dbgMsg),
		"Expected to find %q in %q, but didn't", dbgMsg, outBuf.String())
	assert.Truef(t, strings.Contains(outBuf.String(), dbgMsg),
		"Expected to find %q in %q, but didn't", dbgMsg, outBuf.String())

	logger.Debugf(dbgMsg) // nolint: govet
	assert.Truef(t, strings.Contains(outBuf.String(), dbgMsg),
		"Expected to find %q in %q, but didn't", dbgMsg, outBuf.String())
}

func testWarnLevel(t *testing.T, logger *logging.DefaultLeveledLogger) {
	t.Helper()

	var outBuf bytes.Buffer
	logger.WithOutput(&outBuf)

	warnMsg := "this is a warning message"
	logger.Warn(warnMsg)
	assert.Truef(t, strings.Contains(outBuf.String(), warnMsg),
		"Expected to find %q in %q, but didn't", warnMsg, outBuf.String())

	logger.Warnf(warnMsg) // nolint: govet
	assert.Truef(t, strings.Contains(outBuf.String(), warnMsg),
		"Expected to find %q in %q, but didn't", warnMsg, outBuf.String())
}

func testErrorLevel(t *testing.T, logger *logging.DefaultLeveledLogger) {
	t.Helper()

	var outBuf bytes.Buffer
	logger.WithOutput(&outBuf)

	errMsg := "this is an error message"
	logger.Error(errMsg)
	assert.Truef(t, strings.Contains(outBuf.String(), errMsg),
		"Expected to find %q in %q but didn't", errMsg, outBuf.String())

	logger.Errorf(errMsg) // nolint: govet
	assert.Truef(t, strings.Contains(outBuf.String(), errMsg),
		"Expected to find %q in %q but didn't", errMsg, outBuf.String())
}

func testTraceLevel(t *testing.T, logger *logging.DefaultLeveledLogger) {
	t.Helper()

	var outBuf bytes.Buffer
	logger.WithOutput(&outBuf)

	traceMsg := "trace message"
	logger.Trace(traceMsg)
	assert.Truef(t, strings.Contains(outBuf.String(), traceMsg),
		"Expected to find %q in %q but didn't", traceMsg, outBuf.String())

	logger.Tracef(traceMsg) // nolint: govet
	assert.Truef(t, strings.Contains(outBuf.String(), traceMsg),
		"Expected to find %q in %q but didn't", traceMsg, outBuf.String())
}

func testInfoLevel(t *testing.T, logger *logging.DefaultLeveledLogger) {
	t.Helper()

	var outBuf bytes.Buffer
	logger.WithOutput(&outBuf)

	infoMsg := "info message"
	logger.Info(infoMsg)
	assert.Truef(t, strings.Contains(outBuf.String(), infoMsg),
		"Expected to find %q in %q but didn't", infoMsg, outBuf.String())

	logger.Infof(infoMsg) // nolint: govet
	assert.Truef(t, strings.Contains(outBuf.String(), infoMsg),
		"Expected to find %q in %q but didn't", infoMsg, outBuf.String())
}

func testAllLevels(t *testing.T, logger *logging.DefaultLeveledLogger) {
	t.Helper()

	testDebugLevel(t, logger)
	testWarnLevel(t, logger)
	testErrorLevel(t, logger)
	testTraceLevel(t, logger)
	testInfoLevel(t, logger)
}

func TestDefaultLoggerFactory(t *testing.T) {
	factory := logging.DefaultLoggerFactory{
		Writer:          os.Stderr,
		DefaultLogLevel: logging.LogLevelWarn,
		ScopeLevels: map[string]logging.LogLevel{
			"foo": logging.LogLevelDebug,
		},
	}

	logger := factory.NewLogger("baz")
	bazLogger, ok := logger.(*logging.DefaultLeveledLogger)
	assert.True(t, ok, "Invalid logger type")

	testNoDebugLevel(t, bazLogger)
	testWarnLevel(t, bazLogger)

	logger = factory.NewLogger("foo")
	fooLogger, ok := logger.(*logging.DefaultLeveledLogger)
	assert.True(t, ok, "Invalid logger type")

	testDebugLevel(t, fooLogger)
}

func TestDefaultLogger(t *testing.T) {
	logger := logging.
		NewDefaultLeveledLoggerForScope("test1", logging.LogLevelWarn, os.Stderr)

	testNoDebugLevel(t, logger)
	testWarnLevel(t, logger)
	testErrorLevel(t, logger)
}

func TestNewDefaultLoggerFactory(t *testing.T) {
	factory := logging.NewDefaultLoggerFactory()

	disabled := factory.NewLogger("DISABLE")
	errorLevel := factory.NewLogger("ERROR")
	warnLevel := factory.NewLogger("WARN")
	infoLevel := factory.NewLogger("INFO")
	debugLevel := factory.NewLogger("DEBUG")
	traceLevel := factory.NewLogger("TRACE")

	disabledLogger, ok := disabled.(*logging.DefaultLeveledLogger)
	assert.True(t, ok, "Missing disabled logger")

	errorLogger, ok := errorLevel.(*logging.DefaultLeveledLogger)
	assert.True(t, ok, "Missing error logger")

	warnLogger, ok := warnLevel.(*logging.DefaultLeveledLogger)
	assert.True(t, ok, "Missing warn logger")

	infoLogger, ok := infoLevel.(*logging.DefaultLeveledLogger)
	assert.True(t, ok, "Missing info logger")

	debugLogger, ok := debugLevel.(*logging.DefaultLeveledLogger)
	assert.True(t, ok, "Missing debug logger")

	traceLogger, ok := traceLevel.(*logging.DefaultLeveledLogger)
	assert.True(t, ok, "Missing trace logger")

	testNoDebugLevel(t, disabledLogger)
	testNoDebugLevel(t, errorLogger)
	testNoDebugLevel(t, warnLogger)
	testNoDebugLevel(t, infoLogger)
	testNoDebugLevel(t, debugLogger)
	testNoDebugLevel(t, traceLogger)
}

func TestNewDefaultLoggerFactoryLogAll(t *testing.T) {
	t.Setenv("PION_LOG_ERROR", "all")
	t.Setenv("PION_LOG_WARN", "all")
	t.Setenv("PION_LOG_INFO", "all")
	t.Setenv("PION_LOG_DEBUG", "all")
	t.Setenv("PION_LOG_TRACE", "all")

	factory := logging.NewDefaultLoggerFactory()

	testAPI, ok := factory.NewLogger("test").(*logging.DefaultLeveledLogger)
	assert.True(t, ok, "Invalid logger factory type")

	testAllLevels(t, testAPI)
}

func TestNewDefaultLoggerFactorySpecifcScopes(t *testing.T) {
	t.Setenv("PION_LOG_DEBUG", "feature,rtp-logger")

	factory := logging.NewDefaultLoggerFactory()

	feature, ok := factory.NewLogger("feature").(*logging.DefaultLeveledLogger)
	assert.True(t, ok, "Invalid logger factory type")

	rtp, ok := factory.NewLogger("rtp-logger").(*logging.DefaultLeveledLogger)
	assert.True(t, ok, "Invalid logger factory type")

	noScope, ok := factory.NewLogger("no-scope").(*logging.DefaultLeveledLogger)
	assert.True(t, ok, "Invalid logger factory type")

	testDebugLevel(t, feature)
	testDebugLevel(t, rtp)
	testNoDebugLevel(t, noScope)
}

func TestSetLevel(t *testing.T) {
	logger := logging.
		NewDefaultLeveledLoggerForScope("testSetLevel", logging.LogLevelWarn, os.Stderr)

	testNoDebugLevel(t, logger)
	logger.SetLevel(logging.LogLevelDebug)
	testDebugLevel(t, logger)
}

func TestLogLevel(t *testing.T) {
	logLevel := logging.LogLevelDisabled

	logLevel.Set(logging.LogLevelError)
	assert.Equal(t, logging.LogLevelError, logLevel.Get(), "LogLevel was not set to LogLevelError")
}

func TestLogLevelString(t *testing.T) {
	expected := map[logging.LogLevel]string{
		logging.LogLevelDisabled: "Disabled",
		logging.LogLevelError:    "Error",
		logging.LogLevelWarn:     "Warn",
		logging.LogLevelInfo:     "Info",
		logging.LogLevelDebug:    "Debug",
		logging.LogLevelTrace:    "Trace",
		logging.LogLevel(999):    "UNKNOWN",
	}

	for level, expectedStr := range expected {
		assert.Equal(t, expectedStr, level.String())
	}
}

func TestNewDefaultLoggerStderr(t *testing.T) {
	logger := logging.NewDefaultLeveledLoggerForScope("test", logging.LogLevelWarn, nil)

	testNoDebugLevel(t, logger)
	testWarnLevel(t, logger)
	testErrorLevel(t, logger)
}
