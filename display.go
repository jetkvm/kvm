package kvm

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	halDisplay "github.com/jetkvm/kvm/internal/hal/display"
	halSystem "github.com/jetkvm/kvm/internal/hal/system"
	"github.com/prometheus/common/version"
)

// backlightState: 0 = NORMAL, 1 = DIMMED, 2 = OFF
var backlightState atomic.Int32

var (
	dimTicker       *time.Ticker
	offTicker       *time.Ticker
	backlightCancel context.CancelFunc // signals backlight goroutines to exit
)

func switchToMainScreen() {
	if networkManager == nil {
		nativeInstance.SwitchToScreenIfDifferent("no_network_screen")
		return
	}

	if networkManager.IsUp() {
		nativeInstance.SwitchToScreenIfDifferent("home_screen")
	} else {
		nativeInstance.SwitchToScreenIfDifferent("no_network_screen")
	}
}

func updateDisplayUsbState() {
	if getUsbState() == "configured" {
		nativeInstance.UpdateLabelIfChanged("usb_status_label", "Connected")
		_, _ = nativeInstance.UIObjAddState("usb_status_label", "LV_STATE_CHECKED")
	} else {
		nativeInstance.UpdateLabelIfChanged("usb_status_label", "Disconnected")
		_, _ = nativeInstance.UIObjClearState("usb_status_label", "LV_STATE_CHECKED")
	}
}

func updateDisplay() {
	if networkManager != nil {
		nativeInstance.UpdateLabelIfChanged("home_info_ipv4_addr", networkManager.IPv4String())
		nativeInstance.UpdateLabelAndChangeVisibility("home_info_ipv6_addr", networkManager.IPv6String())
		nativeInstance.UpdateLabelIfChanged("home_info_mac_addr", networkManager.MACString())
	}

	_, _ = nativeInstance.UIObjHide("menu_btn_network")
	_, _ = nativeInstance.UIObjHide("menu_btn_access")

	switch loadCfg().NetworkConfig.DHCPClient.String {
	case "jetdhcpc":
		nativeInstance.UpdateLabelIfChanged("dhcp_client_change_label", "Change to udhcpc")
	case "udhcpc":
		nativeInstance.UpdateLabelIfChanged("dhcp_client_change_label", "Change to JetKVM")
	}

	updateDisplayUsbState()

	if getLastVideoState().Ready {
		nativeInstance.UpdateLabelIfChanged("hdmi_status_label", "Connected")
		_, _ = nativeInstance.UIObjAddState("hdmi_status_label", "LV_STATE_CHECKED")
	} else {
		nativeInstance.UpdateLabelIfChanged("hdmi_status_label", "Disconnected")
		_, _ = nativeInstance.UIObjClearState("hdmi_status_label", "LV_STATE_CHECKED")
	}
	nativeInstance.UpdateLabelIfChanged("cloud_status_label", fmt.Sprintf("%d active", getActiveSessions()))

	if networkManager != nil && networkManager.IsUp() {
		nativeInstance.UISetVar("main_screen", "home_screen")
		nativeInstance.SwitchToScreenIf("home_screen", []string{"no_network_screen", "boot_screen"})
	} else {
		nativeInstance.UISetVar("main_screen", "no_network_screen")
		nativeInstance.SwitchToScreenIf("no_network_screen", []string{"home_screen", "boot_screen"})
	}

	connState := getCloudConnectionState()
	if connState == CloudConnectionStateNotConfigured {
		_, _ = nativeInstance.UIObjHide("cloud_status_icon")
	} else {
		_, _ = nativeInstance.UIObjShow("cloud_status_icon")
	}

	switch connState {
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
	blinkTimer := time.NewTimer(cloudBlinkDuration)
	blinkTimer.Stop()
	defer blinkTimer.Stop()

	for range cloudBlinkTicker.C {
		if getCloudConnectionState() != CloudConnectionStateConnecting {
			continue
		}

		_, _ = nativeInstance.UIObjFadeOut("ui_Home_Header_Cloud_Status_Icon", uint32(cloudBlinkDuration.Milliseconds()))

		blinkTimer.Reset(cloudBlinkDuration)
		select {
		case <-ctx.Done():
			return
		case <-blinkTimer.C:
		}

		_, _ = nativeInstance.UIObjFadeIn("ui_Home_Header_Cloud_Status_Icon", uint32(cloudBlinkDuration.Milliseconds()))

		blinkTimer.Reset(cloudBlinkDuration)
		select {
		case <-ctx.Done():
			return
		case <-blinkTimer.C:
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
	displayInited     atomic.Bool
	displayUpdateLock = sync.Mutex{}
	waitDisplayUpdate = sync.Mutex{}
)

func requestDisplayUpdate(shouldWakeDisplay bool, reason string) {
	displayUpdateLock.Lock()
	defer displayUpdateLock.Unlock()

	if !displayInited.Load() {
		displayLogger.Info().Msg("display not inited, skipping updates")
		return
	}
	go func() {
		defer func() {
			if r := recover(); r != nil {
				displayLogger.Error().Interface("panic", r).Msg("panic in display update")
			}
		}()
		if shouldWakeDisplay {
			wakeDisplay(false, reason)
		}
		displayLogger.Debug().Msg("display updating")
		// TODO: only run once regardless how many pending updates
		updateDisplay()
	}()
}

func waitCtrlAndRequestDisplayUpdate(shouldWakeDisplay bool, reason string) {
	waitDisplayUpdate.Lock()
	defer waitDisplayUpdate.Unlock()

	requestDisplayUpdate(shouldWakeDisplay, reason)
}

func updateStaticContents() {
	//contents that never change
	if networkManager != nil {
		nativeInstance.UpdateLabelIfChanged("home_info_mac_addr", networkManager.MACString())
	}

	// get cpu serial (JetKVM only)
	if serial, err := halSystem.ReadCPUSerialFromProc(); err == nil {
		nativeInstance.UpdateLabelAndChangeVisibility("cpu_serial", strings.TrimSpace(serial))
	}

	// get kernel version
	if kernelVersion, err := halSystem.ReadKernelVersionFromProc(); err == nil {
		nativeInstance.UpdateLabelAndChangeVisibility("kernel_version", kernelVersion)
	}

	nativeInstance.UpdateLabelAndChangeVisibility("build_branch", version.Branch)
	nativeInstance.UpdateLabelAndChangeVisibility("build_date", version.BuildDate)
	nativeInstance.UpdateLabelAndChangeVisibility("golang_version", version.GoVersion)

	// nativeInstance.UpdateLabelAndChangeVisibility("boot_screen_device_id", GetDeviceID())
}

// configureDisplayOnNativeRestart is called when the native process restarts
// it ensures the display is configured correctly after the restart
func configureDisplayOnNativeRestart() {
	displayLogger.Info().Msg("native restarted, configuring display")
	updateStaticContents()
	requestDisplayUpdate(true, "native_restart")
}

// setDisplayBrightness sets /sys/class/backlight/backlight/brightness to alter
// the backlight brightness of the JetKVM hardware's display.
func setDisplayBrightness(brightness int, reason string) error {
	if err := halDisplay.SetBacklightBrightness(brightness); err != nil {
		return err
	}

	displayLogger.Info().Int("brightness", brightness).Str("reason", reason).Msg("set brightness")
	return nil
}

// tickDisplayDim is called when the dim ticker expires, it simply reduces the brightness
// of the display by half of the max brightness.
func tickDisplayDim() {
	err := setDisplayBrightness(loadCfg().DisplayMaxBrightness/2, "tick_display_dim")
	if err != nil {
		displayLogger.Warn().Err(err).Msg("failed to dim display")
	}

	dimTicker.Stop()

	backlightState.Store(1)
}

// tickDisplayOff is called when the off ticker expires, it turns off the display
// by setting the brightness to zero.
func tickDisplayOff() {
	err := setDisplayBrightness(0, "tick_display_off")
	if err != nil {
		displayLogger.Warn().Err(err).Msg("failed to turn off display")
	}

	offTicker.Stop()

	backlightState.Store(2)
}

// wakeDisplay sets the display brightness back to config.DisplayMaxBrightness and stores the time the display
// last woke, ready for displayTimeoutTick to put the display back in the dim/off states.
// Set force to true to skip the backlight state check, this should be done if altering the tickers.
func wakeDisplay(force bool, reason string) {
	if backlightState.Load() == 0 && !force {
		return
	}

	// Don't try to wake up if the display is turned off.
	cfg := loadCfg()
	if cfg.DisplayMaxBrightness == 0 {
		return
	}

	if reason == "" {
		reason = "wake_display"
	}

	err := setDisplayBrightness(cfg.DisplayMaxBrightness, reason)
	if err != nil {
		displayLogger.Warn().Err(err).Msg("failed to wake display")
	}

	if cfg.DisplayDimAfterSec != 0 && dimTicker != nil {
		dimTicker.Reset(time.Duration(cfg.DisplayDimAfterSec) * time.Second)
	}

	if cfg.DisplayOffAfterSec != 0 && offTicker != nil {
		offTicker.Reset(time.Duration(cfg.DisplayOffAfterSec) * time.Second)
	}
	backlightState.Store(0)
}

// startBacklightTickers starts the two tickers for dimming and switching off the display
// if they're not already set. This is done separately to the init routine as the "never dim"
// option has the value set to zero, but time.NewTicker only accept positive values.
func startBacklightTickers() {
	// Don't start the tickers if the display is switched off.
	// Set the display to off if that's the case.
	cfg := loadCfg()
	if cfg.DisplayMaxBrightness == 0 {
		_ = setDisplayBrightness(0, "display_disabled")
		return
	}

	// Cancel previous goroutines and stop tickers before creating new ones.
	// Stopping a ticker does NOT close its channel, so we use a context to
	// signal the goroutines to exit.
	if backlightCancel != nil {
		backlightCancel()
	}
	if dimTicker != nil {
		dimTicker.Stop()
	}
	if offTicker != nil {
		offTicker.Stop()
	}

	ctx, cancel := context.WithCancel(context.Background())
	backlightCancel = cancel

	if cfg.DisplayDimAfterSec != 0 {
		displayLogger.Info().Msg("dim_ticker has started")
		dimTicker = time.NewTicker(time.Duration(cfg.DisplayDimAfterSec) * time.Second)

		go func() {
			defer func() {
				if r := recover(); r != nil {
					displayLogger.Error().Interface("panic", r).Msg("panic in dim ticker")
				}
			}()
			for {
				select {
				case <-ctx.Done():
					return
				case <-dimTicker.C:
					tickDisplayDim()
				}
			}
		}()
	}

	if cfg.DisplayOffAfterSec != 0 {
		displayLogger.Info().Msg("off_ticker has started")
		offTicker = time.NewTicker(time.Duration(cfg.DisplayOffAfterSec) * time.Second)

		go func() {
			defer func() {
				if r := recover(); r != nil {
					displayLogger.Error().Interface("panic", r).Msg("panic in off ticker")
				}
			}()
			for {
				select {
				case <-ctx.Done():
					return
				case <-offTicker.C:
					tickDisplayOff()
				}
			}
		}()
	}
}

func initDisplay() {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				displayLogger.Error().Interface("panic", r).Msg("panic in display init")
			}
		}()
		displayLogger.Info().Msg("setting initial display contents")
		time.Sleep(500 * time.Millisecond)
		updateStaticContents()
		updateDisplayUsbState()
		displayInited.Store(true)
		displayLogger.Info().Msg("display inited")
		startBacklightTickers()
		requestDisplayUpdate(true, "init_display")
	}()
}
