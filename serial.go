package kvm

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/jetkvm/kvm/internal/sync"
	"github.com/pion/webrtc/v4"
	"go.bug.st/serial"
)

const serialPortPath = "/dev/ttyS3"

var (
	ledHDDState atomic.Bool
	ledPWRState atomic.Bool
	btnRSTState bool
	btnPWRState bool
)

const atxSilenceTimeout = 10 * time.Second

type atxTimer interface {
	Stop() bool
}

var (
	atxBoardSilent atomic.Bool
	atxAfterFunc   = func(d time.Duration, f func()) atxTimer { return time.AfterFunc(d, f) }
)

func broadcastATXState(state ATXState) {
	if currentSession != nil {
		writeJSONRPCEvent("atxState", state, currentSession)
	}
	if mqttManager != nil {
		mqttManager.publishATXState(state)
	}
}

func currentATXState() ATXState {
	return ATXState{
		Power:      ledPWRState.Load(),
		HDD:        ledHDDState.Load(),
		Responding: !atxBoardSilent.Load(),
	}
}

type atxLink struct {
	mu      sync.Mutex
	timer   atxTimer
	gen     uint64
	stopped bool

	sendMu sync.Mutex
}

func startATXLink() *atxLink {
	l := &atxLink{}
	l.mu.Lock()
	defer l.mu.Unlock()
	atxBoardSilent.Store(false)
	l.armLocked()
	return l
}

func (l *atxLink) armLocked() {
	if l.timer != nil {
		l.timer.Stop()
	}
	l.gen++
	gen := l.gen
	l.timer = atxAfterFunc(atxSilenceTimeout, func() { l.expire(gen) })
}

func (l *atxLink) expire(gen uint64) {
	l.mu.Lock()
	if l.stopped || gen != l.gen {
		l.mu.Unlock()
		return
	}
	atxBoardSilent.Store(true)
	l.mu.Unlock()

	serialLogger.Warn().Dur("timeout", atxSilenceTimeout).Msg("ATX extension board sends no status")
	l.broadcast(gen)
}

func (l *atxLink) received() {
	l.mu.Lock()
	if l.stopped {
		l.mu.Unlock()
		return
	}
	l.armLocked()
	gen := l.gen
	cameBack := atxBoardSilent.Swap(false)
	l.mu.Unlock()

	if cameBack {
		serialLogger.Info().Msg("ATX extension board sends status again")
	}
	l.broadcast(gen)
}

func (l *atxLink) broadcast(gen uint64) {
	l.sendMu.Lock()
	defer l.sendMu.Unlock()

	l.mu.Lock()
	current := !l.stopped && gen == l.gen
	l.mu.Unlock()

	if current {
		broadcastATXState(currentATXState())
	}
}

func (l *atxLink) stop() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.stopped = true
	l.timer.Stop()
	atxBoardSilent.Store(false)
}

func runATXControl(port io.Reader, stop <-chan struct{}, link *atxLink) {
	scopedLogger := serialLogger.With().Str("service", "atx_control").Logger()

	reader := bufio.NewReader(port)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			select {
			case <-stop:
			default:
				scopedLogger.Warn().Err(err).Msg("Error reading from serial port")
			}
			return
		}

		// Each line should be 4 binary digits + newline
		if len(line) != 5 {
			scopedLogger.Warn().Int("length", len(line)).Msg("Invalid line length")
			continue
		}

		// Parse new states
		newLedHDDState := line[0] == '0'
		newLedPWRState := line[1] == '0'
		newBtnRSTState := line[2] == '1'
		newBtnPWRState := line[3] == '1'

		if newLedHDDState != ledHDDState.Load() ||
			newLedPWRState != ledPWRState.Load() ||
			newBtnRSTState != btnRSTState ||
			newBtnPWRState != btnPWRState {
			scopedLogger.Debug().
				Bool("hdd", newLedHDDState).
				Bool("pwr", newLedPWRState).
				Bool("rst", newBtnRSTState).
				Bool("pwr", newBtnPWRState).
				Msg("Status changed")

			// Update states
			ledHDDState.Store(newLedHDDState)
			ledPWRState.Store(newLedPWRState)
			btnRSTState = newBtnRSTState
			btnPWRState = newBtnPWRState
		}

		link.received()
	}
}

func pressATXPowerButton(duration time.Duration) error {
	err := extPort.write([]byte("\n"))
	if err != nil {
		return err
	}

	err = extPort.write([]byte("BTN_PWR_ON\n"))
	if err != nil {
		return err
	}

	time.Sleep(duration)

	err = extPort.write([]byte("BTN_PWR_OFF\n"))
	if err != nil {
		return err
	}

	return nil
}

func pressATXResetButton(duration time.Duration) error {
	err := extPort.write([]byte("\n"))
	if err != nil {
		return err
	}

	err = extPort.write([]byte("BTN_RST_ON\n"))
	if err != nil {
		return err
	}

	time.Sleep(duration)

	err = extPort.write([]byte("BTN_RST_OFF\n"))
	if err != nil {
		return err
	}

	return nil
}

var (
	dcState   DCPowerState
	dcStateMu sync.RWMutex
)

func getDCState() DCPowerState {
	dcStateMu.RLock()
	defer dcStateMu.RUnlock()
	return dcState
}

func runDCControl(port io.Reader, stop <-chan struct{}) {
	scopedLogger := serialLogger.With().Str("service", "dc_control").Logger()
	reader := bufio.NewReader(port)
	hasRestoreFeature := false
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			select {
			case <-stop:
			default:
				scopedLogger.Warn().Err(err).Msg("Error reading from serial port")
			}
			return
		}

		// Split the line by semicolon
		parts := strings.Split(strings.TrimSpace(line), ";")
		if len(parts) == 5 {
			scopedLogger.Debug().Str("line", line).Msg("Detected DC extension with restore feature")
			hasRestoreFeature = true
		} else if len(parts) == 4 {
			scopedLogger.Debug().Str("line", line).Msg("Detected DC extension without restore feature")
			hasRestoreFeature = false
		} else {
			scopedLogger.Warn().Str("line", line).Msg("Invalid line")
			continue
		}

		// Parse new states
		powerState, err := strconv.Atoi(parts[0])
		if err != nil {
			scopedLogger.Warn().Err(err).Msg("Invalid power state")
			continue
		}

		var restoreState int
		if hasRestoreFeature {
			rs, err := strconv.Atoi(parts[4])
			if err != nil {
				scopedLogger.Warn().Err(err).Msg("Invalid restore state")
				continue
			}
			restoreState = rs
		} else {
			// -1 means not supported
			restoreState = -1
		}

		milliVolts, err := strconv.ParseFloat(parts[1], 64)
		if err != nil {
			scopedLogger.Warn().Err(err).Msg("Invalid voltage")
			continue
		}
		volts := milliVolts / 1000 // Convert mV to V

		milliAmps, err := strconv.ParseFloat(parts[2], 64)
		if err != nil {
			scopedLogger.Warn().Err(err).Msg("Invalid current")
			continue
		}
		amps := milliAmps / 1000 // Convert mA to A

		milliWatts, err := strconv.ParseFloat(parts[3], 64)
		if err != nil {
			scopedLogger.Warn().Err(err).Msg("Invalid power")
			continue
		}
		watts := milliWatts / 1000 // Convert mW to W

		dcStateMu.Lock()
		dcState.IsOn = powerState == 1
		dcState.RestoreState = restoreState
		dcState.Voltage = volts
		dcState.Current = amps
		dcState.Power = watts
		snapshot := dcState
		dcStateMu.Unlock()

		// Update Prometheus metrics
		updateDCMetrics(snapshot)

		if currentSession != nil {
			writeJSONRPCEvent("dcState", snapshot, currentSession)
		}

		if mqttManager != nil {
			mqttManager.publishDCState(snapshot)
		}
	}
}

func setDCPowerState(on bool) error {
	err := extPort.write([]byte("\n"))
	if err != nil {
		return err
	}
	command := "PWR_OFF\n"
	if on {
		command = "PWR_ON\n"
	}
	err = extPort.write([]byte(command))
	if err != nil {
		return err
	}
	return nil
}

func setDCRestoreState(state int) error {
	err := extPort.write([]byte("\n"))
	if err != nil {
		return err
	}
	command := "RESTORE_MODE_OFF\n"
	switch state {
	case 1:
		command = "RESTORE_MODE_ON\n"
	case 2:
		command = "RESTORE_MODE_LAST_STATE\n"
	}
	err = extPort.write([]byte(command))
	if err != nil {
		return err
	}
	return nil
}

func sendCustomCommand(command string) error {
	scopedLogger := serialLogger.With().Str("service", "custom_buttons_tx").Logger()
	scopedLogger.Debug().Msgf("Sending custom command: %q", command)
	mux, _ := extPort.console()
	if mux == nil {
		return fmt.Errorf("serial console is not active")
	}
	mux.Enqueue([]byte(command), "button", true, TXUser)
	return nil
}

var defaultMode = &serial.Mode{
	BaudRate: 115200,
	DataBits: 8,
	Parity:   serial.NoParity,
	StopBits: serial.OneStopBit,
}

var serialPortMode = defaultMode

var serialConfig = SerialSettings{
	BaudRate:           defaultMode.BaudRate,
	DataBits:           defaultMode.DataBits,
	Parity:             "none",
	StopBits:           "1",
	Terminator:         Terminator{Label: "LF (\\n)", Value: "\n"},
	HideSerialSettings: false,
	EnableEcho:         false,
	NormalizeMode:      "names",
	NormalizeLineEnd:   "keep",
	TabRender:          "",
	PreserveANSI:       true,
	ShowNLTag:          false,
	Buttons:            []QuickButton{},
	NetworkMode:        SerialNetworkModeDisabled,
	Ser2NetPort:        defaultSer2NetPort,
	Ser2NetProtocol:    Ser2NetProtocolRaw,
	NetworkMaxClients:  defaultSerialNetworkMaxClients,
}

const serialSettingsPath = "/userdata/serialSettings.json"

type Terminator struct {
	Label string `json:"label"` // Terminator label
	Value string `json:"value"` // Terminator value
}

type QuickButton struct {
	Id         string     `json:"id"`         // Unique identifier
	Label      string     `json:"label"`      // Button label
	Command    string     `json:"command"`    // Command to send, raw command to send (without auto-terminator)
	Terminator Terminator `json:"terminator"` // Terminator to use: None/CR/LF/CRLF/LFCR
	Sort       int        `json:"sort"`       // Sort order
}

// Mode describes a serial port configuration.
type SerialSettings struct {
	BaudRate           int           `json:"baudRate"`           // The serial port bitrate (aka Baudrate)
	DataBits           int           `json:"dataBits"`           // Size of the character (must be 5, 6, 7 or 8)
	Parity             string        `json:"parity"`             // Parity (see Parity type for more info)
	StopBits           string        `json:"stopBits"`           // Stop bits (see StopBits type for more info)
	Terminator         Terminator    `json:"terminator"`         // Terminator to send after each command
	HideSerialSettings bool          `json:"hideSerialSettings"` // Whether to hide the serial settings in the UI
	EnableEcho         bool          `json:"enableEcho"`         // Whether to echo received characters back to the sender
	NormalizeMode      string        `json:"normalizeMode"`      // Normalization mode: "carret", "names", "hex"
	NormalizeLineEnd   string        `json:"normalizeLineEnd"`   // Line ending normalization: "keep", "lf", "cr", "crlf", "lfcr"
	TabRender          string        `json:"tabRender"`          // How to render tabs: "spaces", "arrow", "pipe"
	PreserveANSI       bool          `json:"preserveANSI"`       // Whether to preserve ANSI escape codes
	ShowNLTag          bool          `json:"showNLTag"`          // Whether to show a special tag for new lines
	Buttons            []QuickButton `json:"buttons"`            // Custom quick buttons
	NetworkMode        string        `json:"networkMode"`        // Network access: "disabled", "ser2net", "web"
	Ser2NetPort        int           `json:"ser2netPort"`        // TCP port for ser2net mode
	Ser2NetProtocol    string        `json:"ser2netProtocol"`    // ser2net protocol: "raw", "rfc2217"
	NetworkMaxClients  int           `json:"networkMaxClients"`  // Concurrent network clients allowed
}

type DCMsg struct {
	Type string          `json:"type"`           // "serial" | "system"
	Name string          `json:"name,omitempty"` // e.g. "term.size"
	Data json.RawMessage `json:"data"`           // string for "serial", object for "system"
}

func getSerialSettings() (SerialSettings, error) {
	switch defaultMode.StopBits {
	case serial.OneStopBit:
		serialConfig.StopBits = "1"
	case serial.OnePointFiveStopBits:
		serialConfig.StopBits = "1.5"
	case serial.TwoStopBits:
		serialConfig.StopBits = "2"
	}

	switch defaultMode.Parity {
	case serial.NoParity:
		serialConfig.Parity = "none"
	case serial.OddParity:
		serialConfig.Parity = "odd"
	case serial.EvenParity:
		serialConfig.Parity = "even"
	case serial.MarkParity:
		serialConfig.Parity = "mark"
	case serial.SpaceParity:
		serialConfig.Parity = "space"
	}

	file, err := os.Open(serialSettingsPath)
	if err != nil {
		logger.Info().Msg("SerialSettings file doesn't exist, using default")
		return serialConfig, nil
	}
	defer file.Close()

	// load and merge the default config with the user config
	var loadedConfig SerialSettings
	if err := json.NewDecoder(file).Decode(&loadedConfig); err != nil {
		logger.Warn().Err(err).Msg("SerialSettings file JSON parsing failed")
		return serialConfig, nil
	}

	loadedConfig.normalizeNetwork()
	serialConfig = loadedConfig // Update global config

	// Apply settings to serial port, when opening the extension
	var stopBits serial.StopBits
	switch serialConfig.StopBits {
	case "1":
		stopBits = serial.OneStopBit
	case "1.5":
		stopBits = serial.OnePointFiveStopBits
	case "2":
		stopBits = serial.TwoStopBits
	}

	var parity serial.Parity
	switch serialConfig.Parity {
	case "none":
		parity = serial.NoParity
	case "odd":
		parity = serial.OddParity
	case "even":
		parity = serial.EvenParity
	case "mark":
		parity = serial.MarkParity
	case "space":
		parity = serial.SpaceParity
	}

	serialPortMode = &serial.Mode{
		BaudRate: serialConfig.BaudRate,
		DataBits: serialConfig.DataBits,
		StopBits: stopBits,
		Parity:   parity,
	}

	applyConsoleSettings()

	return loadedConfig, nil
}

func setSerialSettings(newSettings SerialSettings) error {
	newSettings.normalizeNetwork()
	if err := newSettings.validateNetwork(); err != nil {
		return err
	}

	logger.Trace().Str("path", serialSettingsPath).Msg("Saving config")

	file, err := os.Create(serialSettingsPath)
	if err != nil {
		return fmt.Errorf("failed to create SerialSettings file: %w", err)
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(newSettings); err != nil {
		return fmt.Errorf("failed to encode SerialSettings: %w", err)
	}

	var stopBits serial.StopBits
	switch newSettings.StopBits {
	case "1":
		stopBits = serial.OneStopBit
	case "1.5":
		stopBits = serial.OnePointFiveStopBits
	case "2":
		stopBits = serial.TwoStopBits
	default:
		return fmt.Errorf("invalid stop bits: %s", newSettings.StopBits)
	}

	var parity serial.Parity
	switch newSettings.Parity {
	case "none":
		parity = serial.NoParity
	case "odd":
		parity = serial.OddParity
	case "even":
		parity = serial.EvenParity
	case "mark":
		parity = serial.MarkParity
	case "space":
		parity = serial.SpaceParity
	default:
		return fmt.Errorf("invalid parity: %s", newSettings.Parity)
	}
	serialPortMode = &serial.Mode{
		BaudRate: newSettings.BaudRate,
		DataBits: newSettings.DataBits,
		StopBits: stopBits,
		Parity:   parity,
	}

	serialConfig = newSettings // Update global config

	applyConsoleSettings()

	return applySerialNetwork(serialConfig)
}

func applyConsoleSettings() {
	extPort.setConsoleSettings(consoleSettings{
		mode: serialPortMode,
		echo: serialConfig.EnableEcho,
		norm: normalizationOptions(serialConfig),
	})
}

func normalizationOptions(settings SerialSettings) NormalizationOptions {
	norm := NormalizationOptions{
		Mode:         ModeNames,
		LineEnding:   LineEnding_AsIs,
		TabRender:    settings.TabRender,
		PreserveANSI: settings.PreserveANSI,
		ShowNLTag:    settings.ShowNLTag,
	}

	switch settings.NormalizeMode {
	case "caret":
		norm.Mode = ModeCaret
	case "hex":
		norm.Mode = ModeHex
	}

	switch settings.NormalizeLineEnd {
	case "lf":
		norm.LineEnding = LineEnding_LF
	case "cr":
		norm.LineEnding = LineEnding_CR
	case "crlf":
		norm.LineEnding = LineEnding_CRLF
	case "lfcr":
		norm.LineEnding = LineEnding_LFCR
	}

	return norm
}

func setTerminalPaused(paused bool) {
	if _, broker := extPort.console(); broker != nil {
		broker.SetTerminalPaused(paused)
	}
}

func initSerialPort() {
	registerSerialMetrics()
	_ = extPort.activate(config.ActiveExtension)
	if config.ActiveExtension == "serial-console" {
		_ = mountSerialConsole()
	}
}

func handleSerialChannel(dataChannel *webrtc.DataChannel) {
	scopedLogger := serialLogger.With().
		Uint16("data_channel_id", *dataChannel.ID()).Str("service", "serial terminal channel").Logger()

	dataChannel.OnOpen(func() {
		scopedLogger.Info().Msg("Serial channel opened")
		extPort.attachSink(dataChannelSink{dataChannel: dataChannel})
	})

	dataChannel.OnMessage(handleSerialChannelMessage)

	dataChannel.OnError(func(err error) {
		scopedLogger.Warn().Err(err).Msg("Serial channel error")
	})

	dataChannel.OnClose(func() {
		scopedLogger.Info().Msg("Serial channel closed")

		// A replacement channel may already own the sink: this close can
		// run after the new session's OnOpen when the old peer went away
		// without a clean shutdown.
		extPort.detachSink(dataChannelSink{dataChannel: dataChannel})
	})
}

func handleSerialChannelMessage(msg webrtc.DataChannelMessage) {
	mux, broker := extPort.console()
	if mux == nil {
		return
	}

	// Try parse as our JSON envelope
	var m DCMsg
	if msg.IsString && json.Unmarshal(msg.Data, &m) == nil && m.Type != "" {
		switch m.Type {
		case "serial":
			// data is a JSON string (what the user typed)
			var s string
			if err := json.Unmarshal(m.Data, &s); err != nil {
				serialLogger.Warn().Err(err).Msg("Failed to decode serial payload string")
				return
			}

			// Write to UART (echo controlled by serialConfig.EnableEcho inside mux)
			mux.Enqueue([]byte(s), "webrtc", true, TXUser)

		default:
			// Display in terminal for debugging, but do NOT write to UART
			broker.Enqueue(consoleEvent{
				kind:   evTX,
				data:   append([]byte(nil), msg.Data...),
				origin: TXSystem,
			})
		}
		return
	}

	// Backward compatibility: treat non-envelope as raw serial bytes
	mux.Enqueue(msg.Data, "webrtc-raw", true, TXUser)
}
