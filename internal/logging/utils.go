package logging

import (
	"fmt"
	"sync"

	"github.com/rs/zerolog"
)

func ErrorfL(logger *zerolog.Logger, format string, err error, args ...any) error {
	logger.Error().Err(err).Msgf(format, args...)

	if err == nil {
		return fmt.Errorf(format, args...)
	}

	err_msg := err.Error() + ": %w"
	err_args := append(args, err)

	return fmt.Errorf(err_msg, err_args...)
}

func AddOptionalError(context zerolog.Context, err error) zerolog.Context {
	if err == nil {
		return context
	}
	augmentedContext := context.Err(err)
	return augmentedContext
}

func AddRequiredError(context zerolog.Context, err error, msg string, args ...any) (zerolog.Context, error) {
	if err == nil {
		err = fmt.Errorf(msg, args...)
	}
	return AddOptionalError(context, err), err
}

func LogTrace(context zerolog.Context, msg string, args ...any) {
	logger := context.Logger()
	logger.Trace().Msgf(msg, args...)
}

func LogTraceE(context zerolog.Context, err error, msg string, args ...any) error {
	logger := AddOptionalError(context, err).Logger()
	logger.Trace().Msgf(msg, args...)
	return err
}

func LogDebug(context zerolog.Context, msg string, args ...any) {
	logger := context.Logger()
	logger.Debug().Msgf(msg, args...)
}

func LogErrorDebug(context zerolog.Context, err error, msg string, args ...any) error {
	logger := AddOptionalError(context, err).Logger()
	logger.Debug().Msgf(msg, args...)
	return err
}

func LogInfo(context zerolog.Context, msg string, args ...any) {
	logger := context.Logger()
	logger.Info().Msgf(msg, args...)
}

func LogInfoE(context zerolog.Context, err error, msg string, args ...any) error {
	logger := AddOptionalError(context, err).Logger()
	logger.Info().Msgf(msg, args...)
	return err
}

func LogWarn(context zerolog.Context, msg string, args ...any) {
	logger := context.Logger()
	logger.Warn().Msgf(msg, args...)
}

func LogWarnE(context zerolog.Context, err error, msg string, args ...any) error {
	logger := AddOptionalError(context, err).Logger()
	logger.Warn().Msgf(msg, args...)
	return err
}

func LogError(context zerolog.Context, err error, msg string, args ...any) error {
	context, err = AddRequiredError(context, err, msg, args...)
	logger := context.Logger()
	logger.Error().Msgf(msg, args...)
	return err
}

func LogFatal(context zerolog.Context, err error, msg string, args ...any) error {
	context, err = AddRequiredError(context, err, msg, args...)
	logger := context.Logger()
	logger.Fatal().Msgf(msg, args...)
	return err
}

func LogPanic(context zerolog.Context, err error, msg string, args ...any) error {
	context, err = AddRequiredError(context, err, msg, args...)
	logger := context.Logger()
	logger.Panic().Msgf(msg, args...)
	return err
}

func UnlockWithTraceLog(context zerolog.Context, lock *sync.Mutex, msg string, args ...any) {
	LogTrace(context, msg, args...)
	lock.Unlock()
}
