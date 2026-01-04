//go:build linux

package zerotier

const (
	ProviderName        = "zerotier"
	ProviderDisplayName = "ZeroTier"

	// Binary version - official ZeroTier releases from download.zerotier.com
	DefaultVersion = "1.16.0"

	// Download URL for static ARM32 binary (extracted from official Debian packages)
	BaseDownloadURL = "https://download.zerotier.com/RELEASES"

	// Installation paths
	InstallBasePath  = "/userdata/meshvpn/zerotier"
	ZeroTierOnePath  = "/userdata/meshvpn/zerotier/zerotier-one"
	ZeroTierCliPath  = "/userdata/meshvpn/zerotier/zerotier-cli"
	WorkingDirectory = "/userdata/meshvpn/zerotier"

	// State files created by zerotier-one
	IdentityPublicPath = "/userdata/meshvpn/zerotier/identity.public"
	IdentitySecretPath = "/userdata/meshvpn/zerotier/identity.secret"
	AuthTokenPath      = "/userdata/meshvpn/zerotier/authtoken.secret"
	NetworksDirectory  = "/userdata/meshvpn/zerotier/networks.d"
	PortFilePath       = "/userdata/meshvpn/zerotier/zerotier-one.port"
	PidFilePath        = "/userdata/meshvpn/zerotier/zerotier-one.pid"

	// Default API port
	DefaultAPIPort = 9993
)
