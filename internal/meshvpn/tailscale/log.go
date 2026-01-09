//go:build linux

package tailscale

import (
	"github.com/jetkvm/kvm/internal/logging"
)

var logger = logging.GetSubsystemLogger("meshvpn.tailscale")
