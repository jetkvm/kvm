package meshvpn

import (
	"context"
)

// ProviderState represents the current state of a VPN provider
type ProviderState string

const (
	StateNotInstalled ProviderState = "not_installed"
	StateInstalling   ProviderState = "installing"
	StateStopped      ProviderState = "stopped"
	StateConnecting   ProviderState = "connecting"
	StateNeedsAuth    ProviderState = "needs_auth"
	StateConnected    ProviderState = "connected"
	StateError        ProviderState = "error"
)

// ConnectOptions contains options for connecting to a VPN
type ConnectOptions struct {
	ControlServer string // Custom server URL (Headscale, self-hosted)
	AuthKey       string // Pre-auth key for non-interactive setup
}

// ConnectResult contains the result of a connect attempt
type ConnectResult struct {
	AuthURL string // URL for browser-based auth (if needed)
}

// ProviderStatus represents the current status of a VPN provider
type ProviderStatus struct {
	State         ProviderState `json:"state"`
	Installed     bool          `json:"installed"`
	Running       bool          `json:"running"`
	IP            string        `json:"ip,omitempty"`
	Hostname      string        `json:"hostname,omitempty"`
	AuthURL       string        `json:"authUrl,omitempty"`
	ExitNode      string        `json:"exitNode,omitempty"`
	ControlServer string        `json:"controlServer,omitempty"`
	BackendState  string        `json:"backendState,omitempty"`
	ErrorMessage  string        `json:"errorMessage,omitempty"`
	Version       string        `json:"version,omitempty"`
}

// ExitNode represents an exit node in the VPN network
type ExitNode struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	HostName string `json:"hostName"`
	IP       string `json:"ip"`
	Country  string `json:"country,omitempty"`
	City     string `json:"city,omitempty"`
	Online   bool   `json:"online"`
}

// ProviderInfo contains static information about a provider
type ProviderInfo struct {
	Name                 string `json:"name"`
	DisplayName          string `json:"displayName"`
	Installed            bool   `json:"installed"`
	SupportsExitNodes    bool   `json:"supportsExitNodes"`
	SupportsCustomServer bool   `json:"supportsCustomServer"`
	SupportsAuthKey      bool   `json:"supportsAuthKey"`
}

// VersionInfo contains version information for a provider
type VersionInfo struct {
	CurrentVersion  string `json:"currentVersion"`
	LatestVersion   string `json:"latestVersion"`
	UpdateAvailable bool   `json:"updateAvailable"`
}

// ProgressFunc is a callback for reporting progress during operations
type ProgressFunc func(progress float64)

// StatusChangeFunc is a callback for status changes
type StatusChangeFunc func(status ProviderStatus)

// TUNModeProvider is an optional interface for providers that support TUN mode configuration
type TUNModeProvider interface {
	GetTUNMode() TUNMode
	SetTUNMode(ctx context.Context, mode TUNMode) error
}

// ExitNodeAdvertiser is an optional interface for providers that can advertise as an exit node
type ExitNodeAdvertiser interface {
	SetAdvertiseExitNode(ctx context.Context, advertise bool) error
}

// Provider is the interface that all VPN providers must implement
type Provider interface {
	// Identity
	Name() string
	DisplayName() string

	// Feature flags
	SupportsExitNodes() bool
	SupportsCustomServer() bool
	SupportsAuthKey() bool

	// Lifecycle
	Install(ctx context.Context, progress ProgressFunc) error
	Uninstall(ctx context.Context) error
	IsInstalled() bool

	// Connection
	Connect(ctx context.Context, opts ConnectOptions) (*ConnectResult, error)
	Disconnect(ctx context.Context) error
	Logout(ctx context.Context) error

	// Status
	GetStatus(ctx context.Context) (*ProviderStatus, error)
	StartStatusMonitor(ctx context.Context, onChange StatusChangeFunc)
	StopStatusMonitor()

	// Exit nodes (optional - return ErrNotSupported if not supported)
	ListExitNodes(ctx context.Context) ([]ExitNode, error)
	SetExitNode(ctx context.Context, hostname string, allowLAN bool) error
	ClearExitNode(ctx context.Context) error

	// Provider info
	GetInfo() ProviderInfo
}
