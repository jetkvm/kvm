package hidrpc

import (
	"github.com/jetkvm/kvm/internal/logging"
)

func GetHidRpcLoggingContext() *logging.Context {
	return logging.NewContext(logging.GetSubsystemLogger("hidrpc"))
}
