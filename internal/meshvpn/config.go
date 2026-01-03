package meshvpn

import "fmt"

// TUNMode represents the networking mode for the VPN tunnel.
type TUNMode string

const (
	TUNModeUserspace TUNMode = "userspace"
	TUNModeKernel    TUNMode = "kernel"
)

// IsValid returns true if the TUN mode is a valid value.
func (t TUNMode) IsValid() bool {
	return t == TUNModeUserspace || t == TUNModeKernel
}

// ParseTUNMode parses a string into a TUNMode, returning an error for invalid values.
// Empty string defaults to TUNModeUserspace.
func ParseTUNMode(s string) (TUNMode, error) {
	if s == "" || s == string(TUNModeUserspace) {
		return TUNModeUserspace, nil
	}
	if s == string(TUNModeKernel) {
		return TUNModeKernel, nil
	}
	return "", fmt.Errorf("invalid TUN mode: %q (must be %q or %q)", s, TUNModeUserspace, TUNModeKernel)
}

// TailscaleConfig holds persistent settings for the Tailscale provider.
type TailscaleConfig struct {
	Enabled                bool    `json:"enabled"`
	ControlServer          string  `json:"control_server,omitempty"`
	AuthKey                string  `json:"auth_key,omitempty"`
	ExitNode               string  `json:"exit_node,omitempty"`
	ExitNodeAllowLANAccess bool    `json:"exit_node_allow_lan_access,omitempty"`
	AdvertiseExitNode      bool    `json:"advertise_exit_node,omitempty"`
	TUNMode                TUNMode `json:"tun_mode,omitempty"`
}

// Config contains the mesh VPN configuration.
type Config struct {
	ActiveProvider string           `json:"active_provider,omitempty"`
	Tailscale      *TailscaleConfig `json:"tailscale,omitempty"`
}

// IsProviderEnabled returns whether a provider is enabled for auto-start.
func (c *Config) IsProviderEnabled(name string) bool {
	if c == nil {
		return false
	}
	switch name {
	case "tailscale":
		return c.Tailscale != nil && c.Tailscale.Enabled
	default:
		return false
	}
}
