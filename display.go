package kvm

import (
	"fmt"
	"log"
	"time"
	"os"
	"errors"
	"strconv"
)

var currentScreen = "ui_Boot_Screen"

func switchToScreen(screen string) {
	_, err := CallCtrlAction("lv_scr_load", map[string]interface{}{"obj": screen})
	if err != nil {
		log.Printf("failed to switch to screen %s: %v", screen, err)
		return
	}
	currentScreen = screen
}

var displayedTexts = make(map[string]string)

func updateLabelIfChanged(objName string, newText string) {
	if newText != "" && newText != displayedTexts[objName] {
		_, _ = CallCtrlAction("lv_label_set_text", map[string]interface{}{"obj": objName, "text": newText})
		displayedTexts[objName] = newText
	}
}

func switchToScreenIfDifferent(screenName string) {
	fmt.Println("switching screen from", currentScreen, screenName)
	if currentScreen != screenName {
		switchToScreen(screenName)
	}
}

func updateDisplay() {
	updateLabelIfChanged("ui_Home_Content_Ip", networkState.IPv4)
	if usbState == "configured" {
		updateLabelIfChanged("ui_Home_Footer_Usb_Status_Label", "Connected")
		_, _ = CallCtrlAction("lv_obj_set_state", map[string]interface{}{"obj": "ui_Home_Footer_Usb_Status_Label", "state": "LV_STATE_DEFAULT"})
	} else {
		updateLabelIfChanged("ui_Home_Footer_Usb_Status_Label", "Disconnected")
		_, _ = CallCtrlAction("lv_obj_set_state", map[string]interface{}{"obj": "ui_Home_Footer_Usb_Status_Label", "state": "LV_STATE_USER_2"})
	}
	if lastVideoState.Ready {
		updateLabelIfChanged("ui_Home_Footer_Hdmi_Status_Label", "Connected")
		_, _ = CallCtrlAction("lv_obj_set_state", map[string]interface{}{"obj": "ui_Home_Footer_Hdmi_Status_Label", "state": "LV_STATE_DEFAULT"})
	} else {
		updateLabelIfChanged("ui_Home_Footer_Hdmi_Status_Label", "Disconnected")
		_, _ = CallCtrlAction("lv_obj_set_state", map[string]interface{}{"obj": "ui_Home_Footer_Hdmi_Status_Label", "state": "LV_STATE_USER_2"})
	}
	updateLabelIfChanged("ui_Home_Header_Cloud_Status_Label", fmt.Sprintf("%d active", actionSessions))
	if networkState.Up {
		switchToScreenIfDifferent("ui_Home_Screen")
	} else {
		switchToScreenIfDifferent("ui_No_Network_Screen")
	}
}

var displayInited = false

func requestDisplayUpdate() {
	if !displayInited {
		fmt.Println("display not inited, skipping updates")
		return
	}
	go func() {
		fmt.Println("display updating........................")
		//TODO: only run once regardless how many pending updates
		updateDisplay()
	}()
}

func updateStaticContents() {
	//contents that never change
	updateLabelIfChanged("ui_Home_Content_Mac", networkState.MAC)
	systemVersion, appVersion, err := GetLocalVersion()
	if err == nil {
		updateLabelIfChanged("ui_About_Content_Operating_System_Version_ContentLabel", systemVersion.String())
		updateLabelIfChanged("ui_About_Content_App_Version_Content_Label", appVersion.String())
	}

	updateLabelIfChanged("ui_Status_Content_Device_Id_Content_Label", GetDeviceID())
}

// setDisplayBrightness sets /sys/class/backlight/backlight/brightness to alter
// the backlight brightness of the JetKVM hardware's display.
func setDisplayBrightness(brightness int) (error) {
	if brightness > 100 || brightness < 0 {
		return errors.New("brightness value out of bounds, must be between 0 and 100")
	}

	// Check the display backlight class is available
	if _, err := os.Stat("/sys/class/backlight/backlight/brightness"); errors.Is(err, os.ErrNotExist) {
		return errors.New("brightness value cannot be set, possibly not running on JetKVM hardware.")
	}

	// Set the value
	bs := []byte(strconv.Itoa(brightness))
	err := os.WriteFile("/sys/class/backlight/backlight/brightness", bs, 0644)
	if err != nil {
		return err
	}

	fmt.Print("display: set brightness to %v", brightness)
	return nil
}

func init() {
	go func() {
		waitCtrlClientConnected()
		fmt.Println("setting initial display contents")
		time.Sleep(500 * time.Millisecond)
		updateStaticContents()
		displayInited = true
		fmt.Println("display inited")
		requestDisplayUpdate()
	}()
}
