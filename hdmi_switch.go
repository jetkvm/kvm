package kvm

import (
	"fmt"
	"sync"
	"time"

	"github.com/jetkvm/kvm/internal/logging"
	"github.com/jetkvm/kvm/internal/tesmart"
)

// HDMI switch extension: control a TESmart HDMI switch over LAN from the web
// UI. The protocol client lives in internal/tesmart; this file wires it into
// the extension lifecycle (config, RPC, polling, state push).
const (
	hdmiSwitchPollInterval = 10 * time.Second
)

var (
	hdmiSwitchLogger = logging.GetSubsystemLogger("hdmi_switch")

	hdmiSwitchMu     sync.Mutex // serializes commands to the switch and state updates
	hdmiSwitchState  tesmart.State
	hdmiSwitchStopCh chan struct{}
)

func getHdmiSwitchState() tesmart.State {
	hdmiSwitchMu.Lock()
	defer hdmiSwitchMu.Unlock()
	return hdmiSwitchState
}

// notifyHdmiSwitchState pushes the current switch state to the active session.
func notifyHdmiSwitchState() {
	state := getHdmiSwitchState()
	writeJSONRPCEvent("hdmiSwitchState", state, currentSession)
}

// setHdmiSwitchStateLocked updates the state and reports whether it changed.
// hdmiSwitchMu must be held.
func setHdmiSwitchStateLocked(state tesmart.State) bool {
	changed := hdmiSwitchState != state
	hdmiSwitchState = state
	return changed
}

// pollHdmiSwitchState reads the active input from the switch once and pushes
// an update if anything changed. It is safe to call when unconfigured.
func pollHdmiSwitchState() {
	cfg := config.HdmiSwitch
	if cfg == nil || cfg.Host == "" {
		hdmiSwitchMu.Lock()
		changed := setHdmiSwitchStateLocked(tesmart.State{ActiveInput: -1, Online: false})
		hdmiSwitchMu.Unlock()
		if changed {
			notifyHdmiSwitchState()
		}
		return
	}

	resp, err := tesmart.SendCommand(cfg.Host, cfg.Port, tesmart.CmdReadActive, 0x00)
	if err != nil {
		hdmiSwitchLogger.Warn().Err(err).Msg("failed to poll HDMI switch")
		hdmiSwitchMu.Lock()
		changed := setHdmiSwitchStateLocked(tesmart.State{ActiveInput: -1, Online: false, PortCount: cfg.PortCount})
		hdmiSwitchMu.Unlock()
		if changed {
			notifyHdmiSwitchState()
		}
		return
	}

	active, err := tesmart.ParseActiveInput(resp)
	if err != nil {
		hdmiSwitchLogger.Warn().Err(err).Str("response", fmt.Sprintf("% x", resp)).Msg("invalid HDMI switch response")
		hdmiSwitchMu.Lock()
		changed := setHdmiSwitchStateLocked(tesmart.State{ActiveInput: -1, Online: false, PortCount: cfg.PortCount})
		hdmiSwitchMu.Unlock()
		if changed {
			notifyHdmiSwitchState()
		}
		return
	}

	hdmiSwitchMu.Lock()
	changed := setHdmiSwitchStateLocked(tesmart.State{ActiveInput: active, Online: true, PortCount: cfg.PortCount})
	hdmiSwitchMu.Unlock()
	if changed {
		hdmiSwitchLogger.Info().Int("activeInput", active).Msg("HDMI switch active input changed")
		notifyHdmiSwitchState()
	}
}

func runHdmiSwitchControl(stopCh <-chan struct{}) {
	scopedLogger := hdmiSwitchLogger.With().Str("service", "hdmi_switch").Logger()
	scopedLogger.Info().Msg("Starting HDMI switch control")

	pollHdmiSwitchState()

	ticker := time.NewTicker(hdmiSwitchPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-stopCh:
			scopedLogger.Info().Msg("Stopping HDMI switch control")
			return
		case <-ticker.C:
			pollHdmiSwitchState()
		}
	}
}

func mountHdmiSwitchControl() error {
	hdmiSwitchMu.Lock()
	defer hdmiSwitchMu.Unlock()
	if hdmiSwitchStopCh != nil {
		return nil // already running
	}
	hdmiSwitchStopCh = make(chan struct{})
	go runHdmiSwitchControl(hdmiSwitchStopCh)
	return nil
}

func unmountHdmiSwitchControl() error {
	hdmiSwitchMu.Lock()
	defer hdmiSwitchMu.Unlock()
	if hdmiSwitchStopCh != nil {
		close(hdmiSwitchStopCh)
		hdmiSwitchStopCh = nil
	}
	return nil
}

// switchHdmiSwitchInput switches to the given 0-based input and refreshes the
// state immediately. The wire command is 1-based, hence the +1.
func switchHdmiSwitchInput(input int) error {
	cfg := config.HdmiSwitch
	if cfg == nil || cfg.Host == "" {
		return fmt.Errorf("HDMI switch is not configured")
	}
	if input < 0 || input >= cfg.PortCount {
		return fmt.Errorf("input %d out of range (0-%d)", input, cfg.PortCount-1)
	}

	_, err := tesmart.SendCommand(cfg.Host, cfg.Port, tesmart.CmdSwitch, byte(input+1))
	if err != nil {
		return fmt.Errorf("failed to switch HDMI switch input: %w", err)
	}

	hdmiSwitchLogger.Info().Int("input", input).Msg("Switched HDMI switch input")

	// The switch applies the change asynchronously; give it a moment to
	// settle before reading back, otherwise the poll would report the
	// previous input (kvmd does the same).
	time.Sleep(tesmart.SwitchSettleDelay)
	pollHdmiSwitchState()
	return nil
}

func rpcGetHdmiSwitchState() (tesmart.State, error) {
	return getHdmiSwitchState(), nil
}

func rpcSetHdmiSwitchInput(input int) error {
	return switchHdmiSwitchInput(input)
}

func rpcGetHdmiSwitchConfig() (tesmart.Config, error) {
	cfg := tesmart.Config{}
	if config.HdmiSwitch != nil {
		cfg = *config.HdmiSwitch
	}
	tesmart.NormalizeConfig(&cfg)
	return cfg, nil
}

func rpcSetHdmiSwitchConfig(cfg tesmart.Config) (tesmart.Config, error) {
	tesmart.NormalizeConfig(&cfg)

	LoadConfig()
	config.HdmiSwitch = &cfg
	if err := SaveConfig(); err != nil {
		return cfg, fmt.Errorf("failed to save config: %w", err)
	}

	hdmiSwitchLogger.Info().Str("host", cfg.Host).Int("port", cfg.Port).Int("portCount", cfg.PortCount).Msg("HDMI switch config updated")

	// Refresh immediately so the UI sees the new host/port right away.
	pollHdmiSwitchState()
	return cfg, nil
}
