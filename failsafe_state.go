package kvm

import "github.com/jetkvm/kvm/internal/failsafe"

var (
	failsafeCrashLog   = ""
	failsafeModeActive = false
	failsafeModeReason = ""
)

type FailsafeModeNotification struct {
	Active bool   `json:"active"`
	Reason string `json:"reason"`
}

func initFailsafe() {
	result := failsafe.Check()
	failsafeCrashLog = result.CrashLog
	failsafeModeActive = result.Active
	failsafeModeReason = result.Reason
}

func notifyFailsafeMode(session *Session) {
	if !failsafeModeActive || session == nil {
		return
	}

	jsonRpcLogger.Info().Str("reason", failsafeModeReason).Msg("sending failsafe mode notification")

	writeJSONRPCEvent("failsafeMode", FailsafeModeNotification{
		Active: true,
		Reason: failsafeModeReason,
	}, session)
}
