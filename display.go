package kvm

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/common/version"
)

var (
	backlightState = 0 // 0 - NORMAL, 1 - DIMMED, 2 - OFF
)

var (
	dimTicker *time.Ticker
	offTicker *time.Ticker
)

const (
	backlightControlClass string = "/sys/class/backlight/backlight/brightness"
)

func switchToMainScreen() {
	if networkState.IsUp() {
		nativeInstance.SwitchToScreenIfDifferent("home_screen")
	} else {
		nativeInstance.SwitchToScreenIfDifferent("no_network_screen")
	}
}

func updateDisplay() {
	nativeInstance.UpdateLabelIfChanged("home_info_ipv4_addr", networkState.IPv4String())
	nativeInstance.UpdateLabelAndChangeVisibility("home_info_ipv6_addr", networkState.IPv6String())

	_, _ = nativeInstance.UIObjHide("menu_btn_network")
	_, _ = nativeInstance.UIObjHide("menu_btn_access")

	nativeInstance.UpdateLabelIfChanged("home_info_mac_addr", networkState.MACString())

	if usbState == "configured" {
		nativeInstance.UpdateLabelIfChanged("usb_status_label", "Connected")
		_, _ = nativeInstance.UIObjAddState("usb_status_label", "LV_STATE_CHECKED")
	} else {
		nativeInstance.UpdateLabelIfChanged("usb_status_label", "Disconnected")
		_, _ = nativeInstance.UIObjClearState("usb_status_label", "LV_STATE_CHECKED")
	}
	if lastVideoState.Ready {
		nativeInstance.UpdateLabelIfChanged("hdmi_status_label", "Connected")
		_, _ = nativeInstance.UIObjAddState("hdmi_status_label", "LV_STATE_CHECKED")
	} else {
		nativeInstance.UpdateLabelIfChanged("hdmi_status_label", "Disconnected")
		_, _ = nativeInstance.UIObjClearState("hdmi_status_label", "LV_STATE_CHECKED")
	}
	nativeInstance.UpdateLabelIfChanged("cloud_status_label", fmt.Sprintf("%d active", actionSessions))

	if networkState.IsUp() {
		nativeInstance.UISetVar("main_screen", "home_screen")
		nativeInstance.SwitchToScreenIf("home_screen", []string{"no_network_screen", "boot_screen"})
	} else {
		nativeInstance.UISetVar("main_screen", "no_network_screen")
		nativeInstance.SwitchToScreenIf("no_network_screen", []string{"home_screen", "boot_screen"})
	}

	if cloudConnectionState == CloudConnectionStateNotConfigured {
		_, _ = nativeInstance.UIObjHide("cloud_status_icon")
	} else {
		_, _ = nativeInstance.UIObjShow("cloud_status_icon")
	}

	switch cloudConnectionState {
	case CloudConnectionStateDisconnected:
		_, _ = nativeInstance.UIObjSetImageSrc("cloud_status_icon", "cloud_disconnected")
		stopCloudBlink()
	case CloudConnectionStateConnecting:
		_, _ = nativeInstance.UIObjSetImageSrc("cloud_status_icon", "cloud")
		restartCloudBlink()
	case CloudConnectionStateConnected:
		_, _ = nativeInstance.UIObjSetImageSrc("cloud_status_icon", "cloud")
		stopCloudBlink()
	}
}

const (
	cloudBlinkInterval = 2 * time.Second
	cloudBlinkDuration = 1 * time.Second
)

var (
	cloudBlinkTicker *time.Ticker
	cloudBlinkCancel context.CancelFunc
	cloudBlinkLock   = sync.Mutex{}
)

func doCloudBlink(ctx context.Context) {
	for range cloudBlinkTicker.C {
		if cloudConnectionState != CloudConnectionStateConnecting {
			continue
		}

		_, _ = nativeInstance.UIObjFadeOut("ui_Home_Header_Cloud_Status_Icon", uint32(cloudBlinkDuration.Milliseconds()))

		select {
		case <-ctx.Done():
			return
		case <-time.After(cloudBlinkDuration):
		}

		_, _ = nativeInstance.UIObjFadeIn("ui_Home_Header_Cloud_Status_Icon", uint32(cloudBlinkDuration.Milliseconds()))

		select {
		case <-ctx.Done():
			return
		case <-time.After(cloudBlinkDuration):
		}
	}
}

func restartCloudBlink() {
	stopCloudBlink()
	startCloudBlink()
}

func startCloudBlink() {
	cloudBlinkLock.Lock()
	defer cloudBlinkLock.Unlock()

	if cloudBlinkTicker == nil {
		cloudBlinkTicker = time.NewTicker(cloudBlinkInterval)
	} else {
		cloudBlinkTicker.Reset(cloudBlinkInterval)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cloudBlinkCancel = cancel

	go doCloudBlink(ctx)
}

func stopCloudBlink() {
	cloudBlinkLock.Lock()
	defer cloudBlinkLock.Unlock()

	if cloudBlinkCancel != nil {
		cloudBlinkCancel()
		cloudBlinkCancel = nil
	}

	if cloudBlinkTicker != nil {
		cloudBlinkTicker.Stop()
	}
}

var (
	displayInited     = false
	displayUpdateLock = sync.Mutex{}
	waitDisplayUpdate = sync.Mutex{}
)

func requestDisplayUpdate(shouldWakeDisplay bool, reason string) {
	displayUpdateLock.Lock()
	defer displayUpdateLock.Unlock()

	if !displayInited {
		displayLogger.Info().Msg("display not inited, skipping updates")
		return
	}
	go func() {
		if shouldWakeDisplay {
			wakeDisplay(false, reason)
		}
		displayLogger.Debug().Msg("display updating")
		//TODO: only run once regardless how many pending updates
		updateDisplay()
	}()
}

func waitCtrlAndRequestDisplayUpdate(shouldWakeDisplay bool, reason string) {
	waitDisplayUpdate.Lock()
	defer waitDisplayUpdate.Unlock()

	// nativeInstance.WaitCtrlClientConnected()
	requestDisplayUpdate(shouldWakeDisplay, reason)
}

func updateStaticContents() {
	//contents that never change
	nativeInstance.UpdateLabelIfChanged("home_info_mac_addr", networkState.MACString())

	// get cpu info
	if cpuInfo, err := os.ReadFile("/proc/cpuinfo"); err == nil {
		// get the line starting with "Serial"
		for line := range strings.SplitSeq(string(cpuInfo), "\n") {
			if strings.HasPrefix(line, "Serial") {
				serial := strings.SplitN(line, ":", 2)[1]
				nativeInstance.UpdateLabelAndChangeVisibility("cpu_serial", strings.TrimSpace(serial))
				break
			}
		}
	}

	// get kernel version
	if kernelVersion, err := os.ReadFile("/proc/version"); err == nil {
		kernelVersion := strings.TrimPrefix(string(kernelVersion), "Linux version ")
		kernelVersion = strings.SplitN(kernelVersion, " ", 2)[0]
		nativeInstance.UpdateLabelAndChangeVisibility("kernel_version", kernelVersion)
	}

	nativeInstance.UpdateLabelAndChangeVisibility("build_branch", version.Branch)
	nativeInstance.UpdateLabelAndChangeVisibility("build_date", version.BuildDate)
	nativeInstance.UpdateLabelAndChangeVisibility("golang_version", version.GoVersion)

	// nativeInstance.UpdateLabelAndChangeVisibility("boot_screen_device_id", GetDeviceID())
}

// setDisplayBrightness sets /sys/class/backlight/backlight/brightness to alter
// the backlight brightness of the JetKVM hardware's display.
func setDisplayBrightness(brightness int, reason string) error {
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

	displayLogger.Info().Int("brightness", brightness).Str("reason", reason).Msg("set brightness")
	return nil
}

// tick_displayDim() is called when when dim ticker expires, it simply reduces the brightness
// of the display by half of the max brightness.
func tick_displayDim() {
	err := setDisplayBrightness(config.DisplayMaxBrightness/2, "tick_display_dim")
	if err != nil {
		displayLogger.Warn().Err(err).Msg("failed to dim display")
	}

	dimTicker.Stop()

	backlightState = 1
}

// tick_displayOff() is called when the off ticker expires, it turns off the display
// by setting the brightness to zero.
func tick_displayOff() {
	err := setDisplayBrightness(0, "tick_display_off")
	if err != nil {
		displayLogger.Warn().Err(err).Msg("failed to turn off display")
	}

	offTicker.Stop()

	backlightState = 2
}

// wakeDisplay sets the display brightness back to config.DisplayMaxBrightness and stores the time the display
// last woke, ready for displayTimeoutTick to put the display back in the dim/off states.
// Set force to true to skip the backlight state check, this should be done if altering the tickers.
func wakeDisplay(force bool, reason string) {
	if backlightState == 0 && !force {
		return
	}

	// Don't try to wake up if the display is turned off.
	if config.DisplayMaxBrightness == 0 {
		return
	}

	if reason == "" {
		reason = "wake_display"
	}

	err := setDisplayBrightness(config.DisplayMaxBrightness, reason)
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

// startBacklightTickers starts the two tickers for dimming and switching off the display
// if they're not already set. This is done separately to the init routine as the "never dim"
// option has the value set to zero, but time.NewTicker only accept positive values.
func startBacklightTickers() {
	// Don't start the tickers if the display is switched off.
	// Set the display to off if that's the case.
	if config.DisplayMaxBrightness == 0 {
		_ = setDisplayBrightness(0, "display_disabled")
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
		displayLogger.Info().Msg("setting initial display contents")
		time.Sleep(500 * time.Millisecond)
		updateStaticContents()
		displayInited = true
		displayLogger.Info().Msg("display inited")
		startBacklightTickers()
		requestDisplayUpdate(true, "init_display")
	}()
}
