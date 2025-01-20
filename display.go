package kvm

import (
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"
)

var currentScreen = "ui_Boot_Screen"
var backlightState = 0 // 0 - NORMAL, 1 - DIMMED, 2 - OFF

var dim_ticker *time.Ticker
var off_ticker *time.Ticker

const (
	TOUCHSCREEN_DEVICE      string = "/dev/input/event1"
	BACKLIGHT_CONTROL_CLASS string = "/sys/class/backlight/backlight/brightness"
)

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
		wakeDisplay()
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
func setDisplayBrightness(brightness int) error {
	// NOTE: The actual maximum value for this is 255, but out-of-the-box, the value is set to 64.
	// The maximum set here is set to 100 to reduce the risk of drawing too much power (and besides, 255 is very bright!).
	if brightness > 100 || brightness < 0 {
		return errors.New("brightness value out of bounds, must be between 0 and 100")
	}

	// Check the display backlight class is available
	if _, err := os.Stat(BACKLIGHT_CONTROL_CLASS); errors.Is(err, os.ErrNotExist) {
		return errors.New("brightness value cannot be set, possibly not running on JetKVM hardware")
	}

	// Set the value
	bs := []byte(strconv.Itoa(brightness))
	err := os.WriteFile(BACKLIGHT_CONTROL_CLASS, bs, 0644)
	if err != nil {
		return err
	}

	fmt.Printf("display: set brightness to %v\n", brightness)
	return nil
}

// tick_displayDim() is called when when dim ticker expires, it simply reduces the brightness
// of the display by half of the max brightness.
func tick_displayDim() {
	err := setDisplayBrightness(config.DisplayMaxBrightness / 2)
	if err != nil {
		fmt.Printf("display: failed to dim display: %s\n", err)
	}

	dim_ticker.Stop()

	backlightState = 1
}

// tick_displayOff() is called when the off ticker expires, it turns off the display
// by setting the brightness to zero.
func tick_displayOff() {
	err := setDisplayBrightness(0)
	if err != nil {
		fmt.Printf("display: failed to turn off display: %s\n", err)
	}

	off_ticker.Stop()

	backlightState = 2
}

// wakeDisplay sets the display brightness back to config.DisplayMaxBrightness and stores the time the display
// last woke, ready for displayTimeoutTick to put the display back in the dim/off states.
func wakeDisplay() {
	if backlightState == 0 {
		return
	}

	if config.DisplayMaxBrightness == 0 {
		config.DisplayMaxBrightness = 100
	}

	err := setDisplayBrightness(config.DisplayMaxBrightness)
	if err != nil {
		fmt.Printf("display wake failed, %s\n", err)
	}

	dim_ticker.Reset(time.Duration(config.DisplayDimAfterMs) * time.Millisecond)
	off_ticker.Reset(time.Duration(config.DisplayOffAfterMs) * time.Millisecond)
	backlightState = 0
}

// watchTsEvents monitors the touchscreen for events and simply calls wakeDisplay() to ensure the
// touchscreen interface still works even with LCD dimming/off.
// TODO: This is quite a hack, really we should be getting an event from jetkvm_native, or the whole display backlight
// control should be hoisted up to jetkvm_native.
func watchTsEvents() {
	ts, err := os.OpenFile(TOUCHSCREEN_DEVICE, os.O_RDONLY, 0666)
	if err != nil {
		fmt.Printf("display: failed to open touchscreen device: %s\n", err)
		return
	}

	defer ts.Close()

	// This buffer is set to 24 bytes as that's the normal size of events on /dev/input
	// Reference: https://www.kernel.org/doc/Documentation/input/input.txt
	// This could potentially be set higher, to require multiple events to wake the display.
	buf := make([]byte, 24)
	for {
		_, err := ts.Read(buf)
		if err != nil {
			fmt.Printf("display: failed to read from touchscreen device: %s\n", err)
			return
		}

		wakeDisplay()
	}
}

func init() {
	go func() {
		waitCtrlClientConnected()
		fmt.Println("setting initial display contents")
		time.Sleep(500 * time.Millisecond)
		updateStaticContents()
		displayInited = true
		fmt.Println("display inited")
		wakeDisplay()
		requestDisplayUpdate()
	}()

	go func() {
		LoadConfig()
		// Start display auto-sleeping tickers
		dim_ticker = time.NewTicker(time.Duration(config.DisplayDimAfterMs) * time.Millisecond)
		defer dim_ticker.Stop()

		off_ticker = time.NewTicker(time.Duration(config.DisplayOffAfterMs) * time.Millisecond)
		defer off_ticker.Stop()

		for {
			select {
			case <-dim_ticker.C:
				tick_displayDim()
			case <-off_ticker.C:
				tick_displayOff()
			}
		}
	}()

	go watchTsEvents()
}
