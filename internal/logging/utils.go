package logging

import (
	"fmt"
	"sync"
)

func (context *Context) UnlockWithTraceLog(lock *sync.Mutex, msg string, args ...any) {
	defer lock.Unlock()
	context.Trace().Msgf(msg, args...)
}

func (context *Context) ErrorfL(format string, err error, args ...any) error {
	context.Error().Err(err).Msgf(format, args...)
	err_msg := err.Error() + ": %w"
	err_args := append(args, err)
	return fmt.Errorf(err_msg, err_args...)
}
