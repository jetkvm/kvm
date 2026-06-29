package failsafe

import (
	"strings"

	"github.com/jetkvm/kvm/internal/supervisor"
)

const (
	ReasonNative  = "native"
	ReasonUnknown = "unknown"
)

func ClassifyCrashLog(crashLog string) (string, bool) {
	if strings.Contains(crashLog, supervisor.FailsafeReasonNativeMaxRestartAttemptsReached) {
		return ReasonNative, true
	}

	return ReasonUnknown, false
}
