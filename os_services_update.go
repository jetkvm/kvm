package kvm

import (
	"errors"

	"github.com/jetkvm/kvm/internal/osservices"
)

// osServicesUpdateState is the browser's guestUpdate: the newest release and
// the update of the connected sender to it, or nil while no release is known.
func osServicesUpdateState(osservices.State) any { return nil }

func rpcGuestUpdateStart() error {
	return errors.New("no OS Services release is known")
}

func rpcMountBuiltinInstaller() error {
	return errors.New("the OS Services installer is not available")
}
