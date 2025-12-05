package mdns

import (
	"github.com/jetkvm/kvm/internal/logging"
)

func (m *MDNS) getMdnsLogger() *logging.Context {
	return logging.GetSubsystemLogger("mdns").
		Strs("local_names", m.localNames).
		Bool("ipv4", m.listenOptions.IPv4).
		Bool("ipv6", m.listenOptions.IPv6)
}
