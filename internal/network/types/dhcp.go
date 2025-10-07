package types

// DHCPClient is the interface for a DHCP client.
type DHCPClient interface {
	Domain() string
	Lease4() *DHCPLease
	Lease6() *DHCPLease
	Renew() error
	Release() error
	SetIPv4(enabled bool)
	SetIPv6(enabled bool)
	SetOnLeaseChange(callback func(lease *DHCPLease))
	Start() error
	Stop() error
}
