package link

const (
	// AfUnspec is the unspecified address family constant
	AfUnspec = 0
	// AfInet is the IPv4 address family constant
	AfInet = 2
	// AfInet6 is the IPv6 address family constant
	AfInet6 = 10

	sysctlBase     = "/proc/sys"
	sysctlFileMode = 0640
)
