package timesync

import (
	"github.com/jetkvm/kvm/internal/logging"
	"github.com/rs/zerolog"
)

func GetTimesyncLogger() *zerolog.Logger {
	return logging.GetSubsystemLogger("timesync")
}
