package kvm

import (
	"time"
)

var lastUserInput = time.Now()

var jigglerEnabled = false

func rpcSetJigglerState(enabled bool) {
	jigglerEnabled = enabled
}
func rpcGetJigglerState() bool {
	return jigglerEnabled
}

func init() {
	ensureConfigLoaded()

	go runJiggler()
}

func runJiggler() {
	for {
		if jigglerEnabled {
			if time.Since(lastUserInput) > 20*time.Second {
				//TODO: change to rel mouse
				err := rpcAbsMouseReport(1, 1, 0)
				if err != nil {
					logger.Warnf("Failed to jiggle mouse: %v", err)
				}
				err = rpcAbsMouseReport(0, 0, 0)
				if err != nil {
					logger.Warnf("Failed to reset mouse position: %v", err)
				}
			}
		}
		time.Sleep(20 * time.Second)
	}
}
