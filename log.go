package kvm

import (
	"github.com/jetkvm/kvm/internal/logging"
	"github.com/rs/zerolog"
)

const (
	defaultLogLevel = zerolog.ErrorLevel
)

func ErrorfL(l *zerolog.Logger, format string, err error, args ...interface{}) error {
	return logging.ErrorfL(l, format, err, args...)
}

var (
	rootLogger      = logging.GetRootLogger()
	logger          = logging.GetSubsystemLogger("jetkvm")
	networkLogger   = logging.GetSubsystemLogger("network")
	cloudLogger     = logging.GetSubsystemLogger("cloud")
	websocketLogger = logging.GetSubsystemLogger("websocket")
	webrtcLogger    = logging.GetSubsystemLogger("webrtc")
	nativeLogger    = logging.GetSubsystemLogger("native")
	nbdLogger       = logging.GetSubsystemLogger("nbd")
	timesyncLogger  = logging.GetSubsystemLogger("timesync")
	jsonRpcLogger   = logging.GetSubsystemLogger("jsonrpc")
	watchdogLogger  = logging.GetSubsystemLogger("watchdog")
	websecureLogger = logging.GetSubsystemLogger("websecure")
	otaLogger       = logging.GetSubsystemLogger("ota")
	serialLogger    = logging.GetSubsystemLogger("serial")
	terminalLogger  = logging.GetSubsystemLogger("terminal")
	displayLogger   = logging.GetSubsystemLogger("display")
	wolLogger       = logging.GetSubsystemLogger("wol")
	usbLogger       = logging.GetSubsystemLogger("usb")
	// external components
	ginLogger = logging.GetSubsystemLogger("gin")
)
