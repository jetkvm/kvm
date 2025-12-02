package native

import (
	"github.com/jetkvm/kvm/internal/logging"
	"github.com/rs/zerolog"
)

var nativeLogger = logging.GetSubsystemLogger("native")

func GetNativeLoggingContext() *logging.Context {
	return logging.NewContext(logging.GetSubsystemLogger("native"))
}

func GetDisplayLoggingContext() *logging.Context {
	return logging.NewContext(logging.GetSubsystemLogger("display"))
}

type nativeLogMessage struct {
	Level    zerolog.Level
	Message  string
	File     string
	FuncName string
	Line     int
}
