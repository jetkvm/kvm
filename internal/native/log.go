package native

import (
	"os"

	"github.com/jetkvm/kvm/internal/logging"
	"github.com/rs/zerolog"
)

var pid = os.Getpid()

func GetNativeLogger() *logging.Context {
	return logging.GetSubsystemLogger("native").Int("pid", pid)
}

func GetDisplayLogger() *logging.Context {
	return logging.GetSubsystemLogger("display").Int("pid", pid)
}

type nativeLogMessage struct {
	Level    zerolog.Level
	Message  string
	File     string
	FuncName string
	Line     int
}
