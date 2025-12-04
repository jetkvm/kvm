package ota

import (
	"github.com/jetkvm/kvm/internal/logging"
)

func GetOtaLoggingContext() *logging.Context {
	return logging.GetSubsystemLogger("ota")
}
