//go:build linux

package tailscale

const (
	ProviderName        = "tailscale"
	ProviderDisplayName = "Tailscale"
	DefaultVersion      = "1.92.3"
	BaseDownloadURL     = "https://pkgs.tailscale.com/stable"
	InstallBasePath     = "/userdata/meshvpn/tailscale"
	TailscaledPath      = "/userdata/meshvpn/tailscale/tailscaled"
	TailscalePath       = "/userdata/meshvpn/tailscale/tailscale"
	StatePath           = "/userdata/meshvpn/tailscale/state/tailscaled.state"
	SocketPath          = "/var/run/tailscale/tailscaled.sock"
)
