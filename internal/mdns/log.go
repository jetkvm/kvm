package mdns

import (
	"github.com/jetkvm/kvm/internal/logging"
	"github.com/rs/zerolog"
)

func (m *MDNS) getLoggingContext() zerolog.Context {
	context := logging.GetSubsystemLogger("usbgadget").
		With().
		Interface("local_names", m.localNames).
		Bool("ipv4", m.listenOptions.IPv4).
		Bool("ipv6", m.listenOptions.IPv6)
	return context
}
