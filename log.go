package kvm

import (
	"github.com/jetkvm/kvm/internal/logging"
)

var (
	logger          = logging.GetSubsystemLogger("jetkvm")
	failsafeLogger  = logging.GetSubsystemLogger("failsafe")
	networkLogger   = logging.GetSubsystemLogger("network")
	cloudLogger     = logging.GetSubsystemLogger("cloud")
	websocketLogger = logging.GetSubsystemLogger("websocket")
	webrtcLogger    = logging.GetSubsystemLogger("webrtc")
	nativeLogger    = logging.GetSubsystemLogger("native")
	nbdLogger       = logging.GetSubsystemLogger("nbd")
	timesyncLogger  = logging.GetSubsystemLogger("timesync")
	jsonRpcLogger   = logging.GetSubsystemLogger("jsonrpc")
	hidRPCLogger    = logging.GetSubsystemLogger("hidrpc")
	watchdogLogger  = logging.GetSubsystemLogger("watchdog")
	websecureLogger = logging.GetSubsystemLogger("websecure")
	otaLogger       = logging.GetSubsystemLogger("ota")
	serialLogger    = logging.GetSubsystemLogger("serial")
	terminalLogger  = logging.GetSubsystemLogger("terminal")
	displayLogger   = logging.GetSubsystemLogger("display")
	wolLogger       = logging.GetSubsystemLogger("wol")
	usbLogger       = logging.GetSubsystemLogger("usb")
	powerLogger     = logging.GetSubsystemLogger("dcpower")
	// external components
	ginLogger = logging.GetSubsystemLogger("gin")
)
