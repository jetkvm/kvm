package hidrpc

import (
	"github.com/jetkvm/kvm/internal/logging"
	"github.com/rs/zerolog"
)

func GetHidRpcLogger() *zerolog.Logger {
	return logging.GetSubsystemLogger("hidrpc")
}
