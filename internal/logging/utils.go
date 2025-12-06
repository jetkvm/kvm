package logging

import (
	"fmt"
	"sync"

	"github.com/rs/zerolog"
)

func UnlockWithTraceLog(logger *zerolog.Logger, lock *sync.Mutex, msg string, args ...any) {
	defer lock.Unlock()
	logger.Trace().Msgf(msg, args...)
}

func ErrorfL(logger *zerolog.Logger, format string, err error, args ...any) error {
	logger.Error().Err(err).Msgf(format, args...)

	if err == nil {
		return fmt.Errorf(format, args...)
	}

	err_msg := err.Error() + ": %w"
	err_args := append(args, err)

	return fmt.Errorf(err_msg, err_args...)
}

func IsDebugLevel(logger *zerolog.Logger) bool {
	return logger.GetLevel() <= zerolog.DebugLevel
}

func IsTraceLevel(logger *zerolog.Logger) bool {
	return logger.GetLevel() <= zerolog.TraceLevel
}
