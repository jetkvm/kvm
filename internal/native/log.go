package native

import (
	"github.com/jetkvm/kvm/internal/logging"
	"github.com/rs/zerolog"
)

var nativeLogger = logging.GetSubsystemLogger("native")
var displayLogger = logging.GetSubsystemLogger("display")

// refreshLoggers re-fetches the loggers from the logging package.
// This must be called after logging.ReconfigureToStderr() to ensure
// the native package uses the reconfigured loggers.
func refreshLoggers() {
	nativeLogger = logging.GetSubsystemLogger("native")
	displayLogger = logging.GetSubsystemLogger("display")
}

type nativeLogMessage struct {
	Level    zerolog.Level
	Message  string
	File     string
	FuncName string
	Line     int
}
