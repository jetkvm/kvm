package ota

import (
	"github.com/jetkvm/kvm/internal/logging"
	"github.com/rs/zerolog"
)

func GetOtaLogger() *zerolog.Logger {
	return logging.GetSubsystemLogger("ota")
}
