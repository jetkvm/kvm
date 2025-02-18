package kvm

import "github.com/pion/logging"

// we use logging framework from pion
// ref: https://github.com/pion/webrtc/wiki/Debugging-WebRTC
var logger = logging.NewDefaultLoggerFactory().NewLogger("jetkvm")
var usbLogger = logging.NewDefaultLoggerFactory().NewLogger("usb")
var cloudLogger = logging.NewDefaultLoggerFactory().NewLogger("cloud")
