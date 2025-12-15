package mdns

import (
	"github.com/jetkvm/kvm/internal/logging"
	"github.com/rs/zerolog"
)

func (m *MDNS) getMdnsLogger() *zerolog.Logger {
	logger := logging.GetSubsystemLogger("mdns").
		With().
		Strs("local_names", m.localNames).
		Bool("ipv4", m.listenOptions.IPv4).
		Bool("ipv6", m.listenOptions.IPv6).
		Logger()
	return &logger
}
