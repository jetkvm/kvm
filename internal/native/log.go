package native

import (
	"os"

	"github.com/jetkvm/kvm/internal/logging"
	"github.com/rs/zerolog"
)

var pid = os.Getpid()

func GetNativeLogger() *zerolog.Logger {
	logger := logging.GetSubsystemLogger("native").
		With().
		Int("pid", pid).
		Logger()
	return &logger
}

func GetDisplayLogger() *zerolog.Logger {
	logging := logging.GetSubsystemLogger("display").
		With().
		Int("pid", pid).
		Logger()
	return &logging
}

type nativeLogMessage struct {
	Level    zerolog.Level
	Message  string
	File     string
	FuncName string
	Line     int
}
