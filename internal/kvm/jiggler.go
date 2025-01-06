package kvm

import (
	"time"

	"github.com/jetkvm/kvm/internal/logging"
)

var lastUserInput = time.Now()

func ResetUserInputTime() {
	lastUserInput = time.Now()
}

var jigglerEnabled = false

func RPCSetJigglerState(enabled bool) {
	jigglerEnabled = enabled
}
func RPCGetJigglerState() bool {
	return jigglerEnabled
}

func init() {
	go runJiggler()
}

func runJiggler() {
	for {
		if jigglerEnabled {
			if time.Since(lastUserInput) > 20*time.Second {
				//TODO: change to rel mouse
				err := RPCAbsMouseReport(1, 1, 0)
				if err != nil {
					logging.Logger.Warnf("Failed to jiggle mouse: %v", err)
				}
				err = RPCAbsMouseReport(0, 0, 0)
				if err != nil {
					logging.Logger.Warnf("Failed to reset mouse position: %v", err)
				}
			}
		}
		time.Sleep(20 * time.Second)
	}
}
