package network

type DhcpTargetState int

const (
	DhcpTargetStateDoNothing DhcpTargetState = iota
	DhcpTargetStateStart
	DhcpTargetStateStop
	DhcpTargetStateRenew
	DhcpTargetStateRelease
)
