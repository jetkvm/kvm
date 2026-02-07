package logging

import (
	"runtime/debug"

	"github.com/rs/zerolog"
)

// SafeGo launches a goroutine with panic recovery. If fn panics, the panic is
// logged with a full stack trace and the goroutine exits without crashing the process.
func SafeGo(logger *zerolog.Logger, tag string, fn func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Error().
					Interface("panic", r).
					Str("stack", string(debug.Stack())).
					Str("errorId", tag).
					Msg("goroutine panicked - this is a bug, please report")
			}
		}()
		fn()
	}()
}
