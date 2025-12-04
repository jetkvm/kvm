package timesync

import (
	"github.com/jetkvm/kvm/internal/logging"
)

func GetTimesyncLogger() *logging.Context {
	return logging.GetSubsystemLogger("timesync")
}
