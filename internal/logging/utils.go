package logging

import (
	"fmt"
	"os"

	"github.com/rs/zerolog"
)

var defaultLogger = zerolog.New(os.Stdout).Level(zerolog.InfoLevel)

func GetDefaultLogger() *zerolog.Logger {
	return &defaultLogger
}

func ErrorfL(l *zerolog.Logger, format string, err error, args ...interface{}) error {
	// TODO: move rootLogger to logging package
	if l == nil {
		l = &defaultLogger
	}

	l.Error().Err(err).Msgf(format, args...)

	if err == nil {
		return fmt.Errorf(format, args...)
	}

	err_msg := err.Error() + ": %v"
	err_args := append(args, err)

	return fmt.Errorf(err_msg, err_args...)
}
