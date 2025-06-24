package kvm

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"
)

var currentScreen = "ui_Boot_Screen"
var backlightState = 0 // 0 - NORMAL, 1 - DIMMED, 2 - OFF

var (
	dimTicker *time.Ticker
	offTicker *time.Ticker
)

const (
	touchscreenDevice     string = "/dev/input/event1"
	backlightControlClass string = "/sys/class/backlight/backlight/brightness"
)

func switchToScreen(screen string) {
	_, err := CallCtrlAction("lv_scr_load", map[string]interface{}{"obj": screen})
	if err != nil {
		displayLogger.Warn().Err(err).Str("screen", screen).Msg("failed to switch to screen")
		return
	}
	currentScreen = screen
}

var displayedTexts = make(map[string]string)

func lvObjSetState(objName string, state string) (*CtrlResponse, error) {
	return CallCtrlAction("lv_obj_set_state", map[string]interface{}{"obj": objName, "state": state})
}

func lvObjAddFlag(objName string, flag string) (*CtrlResponse, error) {
	return CallCtrlAction("lv_obj_add_flag", map[string]interface{}{"obj": objName, "flag": flag})
}

func lvObjClearFlag(objName string, flag string) (*CtrlResponse, error) {
	return CallCtrlAction("lv_obj_clear_flag", map[string]interface{}{"obj": objName, "flag": flag})
}

func lvObjHide(objName string) (*CtrlResponse, error) {
	return lvObjAddFlag(objName, "LV_OBJ_FLAG_HIDDEN")
}

func lvObjShow(objName string) (*CtrlResponse, error) {
	return lvObjClearFlag(objName, "LV_OBJ_FLAG_HIDDEN")
}

func lvObjSetOpacity(objName string, opacity int) (*CtrlResponse, error) { // nolint:unused
	return CallCtrlAction("lv_obj_set_style_opa_layered", map[string]interface{}{"obj": objName, "opa": opacity})
}

func lvObjFadeIn(objName string, duration uint32) (*CtrlResponse, error) {
	return CallCtrlAction("lv_obj_fade_in", map[string]interface{}{"obj": objName, "time": duration})
}

func lvObjFadeOut(objName string, duration uint32) (*CtrlResponse, error) {
	return CallCtrlAction("lv_obj_fade_out", map[string]interface{}{"obj": objName, "time": duration})
}

func lvLabelSetText(objName string, text string) (*CtrlResponse, error) {
	return CallCtrlAction("lv_label_set_text", map[string]interface{}{"obj": objName, "text": text})
}

func lvImgSetSrc(objName string, src string) (*CtrlResponse, error) {
	return CallCtrlAction("lv_img_set_src", map[string]interface{}{"obj": objName, "src": src})
}

func lvDispSetRotation(rotation string) (*CtrlResponse, error) {
	return CallCtrlAction("lv_disp_set_rotation", map[string]interface{}{"rotation": rotation})
}

func updateLabelIfChanged(objName string, newText string) {
	if newText != "" && newText != displayedTexts[objName] {
		_, _ = lvLabelSetText(objName, newText)
		displayedTexts[objName] = newText
	}
}

func switchToScreenIfDifferent(screenName string) {
	if currentScreen != screenName {
		displayLogger.Info().Str("from", currentScreen).Str("to", screenName).Msg("switching screen")
		switchToScreen(screenName)
	}
}

var (
	cloudBlinkLock    sync.Mutex = sync.Mutex{}
	cloudBlinkStopped bool
	cloudBlinkTicker  *time.Ticker
)

func updateDisplay() {
	updateLabelIfChanged("ui_Home_Content_Ip", networkState.IPv4String())
	if usbState == "configured" {
		updateLabelIfChanged("ui_Home_Footer_Usb_Status_Label", "Connected")
		_, _ = lvObjSetState("ui_Home_Footer_Usb_Status_Label", "LV_STATE_DEFAULT")
	} else {
		updateLabelIfChanged("ui_Home_Footer_Usb_Status_Label", "Disconnected")
		_, _ = lvObjSetState("ui_Home_Footer_Usb_Status_Label", "LV_STATE_USER_2")
	}
	if lastVideoState.Ready {
		updateLabelIfChanged("ui_Home_Footer_Hdmi_Status_Label", "Connected")
		_, _ = lvObjSetState("ui_Home_Footer_Hdmi_Status_Label", "LV_STATE_DEFAULT")
	} else {
		updateLabelIfChanged("ui_Home_Footer_Hdmi_Status_Label", "Disconnected")
		_, _ = lvObjSetState("ui_Home_Footer_Hdmi_Status_Label", "LV_STATE_USER_2")
	}
	updateLabelIfChanged("ui_Home_Header_Cloud_Status_Label", fmt.Sprintf("%d active", actionSessions))

	if networkState.IsUp() {
		switchToScreenIfDifferent("ui_Home_Screen")
	} else {
		switchToScreenIfDifferent("ui_No_Network_Screen")
	}

	if cloudConnectionState == CloudConnectionStateNotConfigured {
		_, _ = lvObjHide("ui_Home_Header_Cloud_Status_Icon")
	} else {
		_, _ = lvObjShow("ui_Home_Header_Cloud_Status_Icon")
	}

	switch cloudConnectionState {
	case CloudConnectionStateDisconnected:
		_, _ = lvImgSetSrc("ui_Home_Header_Cloud_Status_Icon", "cloud_disconnected.png")
		stopCloudBlink()
	case CloudConnectionStateConnecting:
		_, _ = lvImgSetSrc("ui_Home_Header_Cloud_Status_Icon", "cloud.png")
		startCloudBlink()
	case CloudConnectionStateConnected:
		_, _ = lvImgSetSrc("ui_Home_Header_Cloud_Status_Icon", "cloud.png")
		stopCloudBlink()
	}
}

func startCloudBlink() {
	if cloudBlinkTicker == nil {
		cloudBlinkTicker = time.NewTicker(2 * time.Second)
	} else {
		// do nothing if the blink isn't stopped
		if cloudBlinkStopped {
			cloudBlinkLock.Lock()
			defer cloudBlinkLock.Unlock()

			cloudBlinkStopped = false
			cloudBlinkTicker.Reset(2 * time.Second)
		}
	}

	go func() {
		for range cloudBlinkTicker.C {
			if cloudConnectionState != CloudConnectionStateConnecting {
				continue
			}
			_, _ = lvObjFadeOut("ui_Home_Header_Cloud_Status_Icon", 1000)
			time.Sleep(1000 * time.Millisecond)
			_, _ = lvObjFadeIn("ui_Home_Header_Cloud_Status_Icon", 1000)
			time.Sleep(1000 * time.Millisecond)
		}
	}()
}

func stopCloudBlink() {
	if cloudBlinkTicker != nil {
		cloudBlinkTicker.Stop()
	}

	cloudBlinkLock.Lock()
	defer cloudBlinkLock.Unlock()
	cloudBlinkStopped = true
}

var (
	displayInited     = false
	displayUpdateLock = sync.Mutex{}
	waitDisplayUpdate = sync.Mutex{}
)

func requestDisplayUpdate(shouldWakeDisplay bool) {
	displayUpdateLock.Lock()
	defer displayUpdateLock.Unlock()

	if !displayInited {
		displayLogger.Info().Msg("display not inited, skipping updates")
		return
	}
	go func() {
		if shouldWakeDisplay {
			wakeDisplay(false)
		}
		displayLogger.Debug().Msg("display updating")
		//TODO: only run once regardless how many pending updates
		updateDisplay()
	}()
}

func waitCtrlAndRequestDisplayUpdate(shouldWakeDisplay bool) {
	waitDisplayUpdate.Lock()
	defer waitDisplayUpdate.Unlock()

	waitCtrlClientConnected()
	requestDisplayUpdate(shouldWakeDisplay)
}

func updateStaticContents() {
	//contents that never change
	updateLabelIfChanged("ui_Home_Content_Mac", networkState.MACString())
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
	if _, err := os.Stat(backlightControlClass); errors.Is(err, os.ErrNotExist) {
		return errors.New("brightness value cannot be set, possibly not running on JetKVM hardware")
	}

	// Set the value
	bs := []byte(strconv.Itoa(brightness))
	err := os.WriteFile(backlightControlClass, bs, 0644)
	if err != nil {
		return err
	}

	displayLogger.Info().Int("brightness", brightness).Msg("set brightness")
	return nil
}

// tick_displayDim() is called when when dim ticker expires, it simply reduces the brightness
// of the display by half of the max brightness.
func tick_displayDim() {
	err := setDisplayBrightness(config.DisplayMaxBrightness / 2)
	if err != nil {
		displayLogger.Warn().Err(err).Msg("failed to dim display")
	}

	dimTicker.Stop()

	backlightState = 1
}

// tick_displayOff() is called when the off ticker expires, it turns off the display
// by setting the brightness to zero.
func tick_displayOff() {
	err := setDisplayBrightness(0)
	if err != nil {
		displayLogger.Warn().Err(err).Msg("failed to turn off display")
	}

	offTicker.Stop()

	backlightState = 2
}

// wakeDisplay sets the display brightness back to config.DisplayMaxBrightness and stores the time the display
// last woke, ready for displayTimeoutTick to put the display back in the dim/off states.
// Set force to true to skip the backlight state check, this should be done if altering the tickers.
func wakeDisplay(force bool) {
	if backlightState == 0 && !force {
		return
	}

	// Don't try to wake up if the display is turned off.
	if config.DisplayMaxBrightness == 0 {
		return
	}

	err := setDisplayBrightness(config.DisplayMaxBrightness)
	if err != nil {
		displayLogger.Warn().Err(err).Msg("failed to wake display")
	}

	if config.DisplayDimAfterSec != 0 {
		dimTicker.Reset(time.Duration(config.DisplayDimAfterSec) * time.Second)
	}

	if config.DisplayOffAfterSec != 0 {
		offTicker.Reset(time.Duration(config.DisplayOffAfterSec) * time.Second)
	}
	backlightState = 0
}

// watchTsEvents monitors the touchscreen for events and simply calls wakeDisplay() to ensure the
// touchscreen interface still works even with LCD dimming/off.
// TODO: This is quite a hack, really we should be getting an event from jetkvm_native, or the whole display backlight
// control should be hoisted up to jetkvm_native.
func watchTsEvents() {
	ts, err := os.OpenFile(touchscreenDevice, os.O_RDONLY, 0666)
	if err != nil {
		displayLogger.Warn().Err(err).Msg("failed to open touchscreen device")
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
			displayLogger.Warn().Err(err).Msg("failed to read from touchscreen device")
			return
		}

		wakeDisplay(false)
	}
}

// startBacklightTickers starts the two tickers for dimming and switching off the display
// if they're not already set. This is done separately to the init routine as the "never dim"
// option has the value set to zero, but time.NewTicker only accept positive values.
func startBacklightTickers() {
	// Don't start the tickers if the display is switched off.
	// Set the display to off if that's the case.
	if config.DisplayMaxBrightness == 0 {
		_ = setDisplayBrightness(0)
		return
	}

	// Stop existing tickers to prevent multiple active instances on repeated calls
	if dimTicker != nil {
		dimTicker.Stop()
	}

	if offTicker != nil {
		offTicker.Stop()
	}

	if config.DisplayDimAfterSec != 0 {
		displayLogger.Info().Msg("dim_ticker has started")
		dimTicker = time.NewTicker(time.Duration(config.DisplayDimAfterSec) * time.Second)

		go func() {
			for { //nolint:staticcheck
				select {
				case <-dimTicker.C:
					tick_displayDim()
				}
			}
		}()
	}

	if config.DisplayOffAfterSec != 0 {
		displayLogger.Info().Msg("off_ticker has started")
		offTicker = time.NewTicker(time.Duration(config.DisplayOffAfterSec) * time.Second)

		go func() {
			for { //nolint:staticcheck
				select {
				case <-offTicker.C:
					tick_displayOff()
				}
			}
		}()
	}
}

func initDisplay() {
	go func() {
		waitCtrlClientConnected()
		displayLogger.Info().Msg("setting initial display contents")
		time.Sleep(500 * time.Millisecond)
		_, _ = lvDispSetRotation(config.DisplayRotation)
		updateStaticContents()
		displayInited = true
		displayLogger.Info().Msg("display inited")
		startBacklightTickers()
		wakeDisplay(true)
		requestDisplayUpdate(true)
	}()

	go watchTsEvents()
}
