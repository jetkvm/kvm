package kvm

import "github.com/pion/logging"

// we use logging framework from pion
// ref: https://github.com/pion/webrtc/wiki/Debugging-WebRTC
var logger = logging.NewDefaultLoggerFactory().NewLogger("jetkvm/jetkvm")
var cloudLogger = logging.NewDefaultLoggerFactory().NewLogger("jetkvm/cloud")
var websocketLogger = logging.NewDefaultLoggerFactory().NewLogger("jetkvm/websocket")
var nativeLogger = logging.NewDefaultLoggerFactory().NewLogger("jetkvm/native")
