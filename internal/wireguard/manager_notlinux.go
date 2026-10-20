//go:build !linux

package wireguard

import (
	"errors"

	"github.com/rs/zerolog"
)

// InterfaceName is the WireGuard interface the device creates.
const InterfaceName = "wg0"

// Supported reports whether the kernel has WireGuard; only Linux does.
func Supported() bool { return false }

// Manager is a stub on platforms without kernel WireGuard.
type Manager struct{}

// NewManager returns a Manager.
func NewManager(_ *zerolog.Logger) *Manager { return &Manager{} }

// Apply fails for an enabled config: WireGuard needs Linux.
func (m *Manager) Apply(cfg Config) error {
	if cfg.Enabled {
		return errors.New("WireGuard is only supported on Linux")
	}
	return nil
}

// Status reports that WireGuard is not supported.
func (m *Manager) Status() Status {
	return Status{Peers: []PeerStatus{}}
}
