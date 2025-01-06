package logging

import "github.com/pion/logging"

// we use logging framework from pion
// ref: https://github.com/pion/webrtc/wiki/Debugging-WebRTC
var Logger = logging.NewDefaultLoggerFactory().NewLogger("jetkvm")
var UsbLogger = logging.NewDefaultLoggerFactory().NewLogger("usb")
