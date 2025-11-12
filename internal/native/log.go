package native

import (
	"os"

	"github.com/jetkvm/kvm/internal/logging"
	"github.com/rs/zerolog"
)

var nativeL = logging.GetSubsystemLogger("native").With().Int("pid", os.Getpid()).Logger()
var nativeLogger = &nativeL
var displayLogger = logging.GetSubsystemLogger("display")

type nativeLogMessage struct {
	Level    zerolog.Level
	Message  string
	File     string
	FuncName string
	Line     int
}
