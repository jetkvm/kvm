package timesync

import (
	"github.com/jetkvm/kvm/internal/logging"
)

func GetTimesyncLoggingContext() *logging.Context {
	return logging.NewContext(logging.GetSubsystemLogger("timesync"))
}
