package kvm

import (
	"kvm/internal/usbgadget"
	"log"
	"time"
)

var gadget *usbgadget.UsbGadget

// initUsbGadget initializes the USB gadget.
// call it only after the config is loaded.
func initUsbGadget() {
	gadget = usbgadget.NewUsbGadget(
		"jetkvm",
		config.UsbDevices,
		config.UsbConfig,
		&logger,
	)

	go func() {
		for {
			checkUSBState()
			time.Sleep(500 * time.Millisecond)
		}
	}()
}

func rpcKeyboardReport(modifier uint8, keys []uint8) error {
	return gadget.KeyboardReport(modifier, keys)
}

func rpcAbsMouseReport(x, y int, buttons uint8) error {
	return gadget.AbsMouseReport(x, y, buttons)
}

func rpcWheelReport(wheelY int8) error {
	return gadget.AbsMouseWheelReport(wheelY)
}

func rpcRelMouseReport(mx, my int8, buttons uint8) error {
	return gadget.RelMouseReport(mx, my, buttons)
}

var usbState = "unknown"

func rpcGetUSBState() (state string) {
	return gadget.GetUsbState()
}

func triggerUSBStateUpdate() {
	go func() {
		if currentSession == nil {
			log.Println("No active RPC session, skipping update state update")
			return
		}
		writeJSONRPCEvent("usbState", usbState, currentSession)
	}()
}

func checkUSBState() {
	newState := gadget.GetUsbState()
	if newState == usbState {
		return
	}
	usbState = newState

	logger.Infof("USB state changed from %s to %s", usbState, newState)
	requestDisplayUpdate()
	triggerUSBStateUpdate()
}
