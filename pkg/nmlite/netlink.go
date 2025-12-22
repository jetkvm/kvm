package nmlite

import (
	"github.com/jetkvm/kvm/pkg/nmlite/link"
	"github.com/rs/zerolog"
)

func getNetlinkManager(logger *zerolog.Logger) *link.NetlinkManager {
	return link.GetNetlinkManager(logger)
}
