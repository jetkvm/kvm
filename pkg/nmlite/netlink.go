package nmlite

import "github.com/jetkvm/kvm/pkg/nmlite/link"

func getNetlinkManager() *link.NetlinkManager {
	return link.GetNetlinkManager()
}
