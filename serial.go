package kvm

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/pion/webrtc/v4"
	"go.bug.st/serial"
)

const serialPortPath = "/dev/ttyS3"

var port serial.Port
var serialMux *SerialMux
var consoleBroker *ConsoleBroker

func mountATXControl() error {
	_ = port.SetMode(defaultMode)
	go runATXControl()

	return nil
}

func unmountATXControl() error {
	_ = reopenSerialPort()
	return nil
}

var (
	ledHDDState bool
	ledPWRState bool
	btnRSTState bool
	btnPWRState bool
)

func runATXControl() {
	scopedLogger := serialLogger.With().Str("service", "atx_control").Logger()

	reader := bufio.NewReader(port)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			scopedLogger.Warn().Err(err).Msg("Error reading from serial port")
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

		if currentSession != nil {
			writeJSONRPCEvent("atxState", ATXState{
				Power: newLedPWRState,
				HDD:   newLedHDDState,
			}, currentSession)
		}

		if newLedHDDState != ledHDDState ||
			newLedPWRState != ledPWRState ||
			newBtnRSTState != btnRSTState ||
			newBtnPWRState != btnPWRState {
			scopedLogger.Debug().
				Bool("hdd", newLedHDDState).
				Bool("pwr", newLedPWRState).
				Bool("rst", newBtnRSTState).
				Bool("pwr", newBtnPWRState).
				Msg("Status changed")

			// Update states
			ledHDDState = newLedHDDState
			ledPWRState = newLedPWRState
			btnRSTState = newBtnRSTState
			btnPWRState = newBtnPWRState
		}
	}
}

func pressATXPowerButton(duration time.Duration) error {
	_, err := port.Write([]byte("\n"))
	if err != nil {
		return err
	}

	_, err = port.Write([]byte("BTN_PWR_ON\n"))
	if err != nil {
		return err
	}

	time.Sleep(duration)

	_, err = port.Write([]byte("BTN_PWR_OFF\n"))
	if err != nil {
		return err
	}

	return nil
}

func pressATXResetButton(duration time.Duration) error {
	_, err := port.Write([]byte("\n"))
	if err != nil {
		return err
	}

	_, err = port.Write([]byte("BTN_RST_ON\n"))
	if err != nil {
		return err
	}

	time.Sleep(duration)

	_, err = port.Write([]byte("BTN_RST_OFF\n"))
	if err != nil {
		return err
	}

	return nil
}

func mountDCControl() error {
	_ = port.SetMode(defaultMode)
	registerDCMetrics()
	go runDCControl()
	return nil
}

func unmountDCControl() error {
	_ = reopenSerialPort()
	return nil
}

var dcState DCPowerState

func runDCControl() {
	scopedLogger := serialLogger.With().Str("service", "dc_control").Logger()
	reader := bufio.NewReader(port)
	hasRestoreFeature := false
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			scopedLogger.Warn().Err(err).Msg("Error reading from serial port")
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
		dcState.IsOn = powerState == 1
		if hasRestoreFeature {
			restoreState, err := strconv.Atoi(parts[4])
			if err != nil {
				scopedLogger.Warn().Err(err).Msg("Invalid restore state")
				continue
			}
			dcState.RestoreState = restoreState
		} else {
			// -1 means not supported
			dcState.RestoreState = -1
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

		dcState.Voltage = volts
		dcState.Current = amps
		dcState.Power = watts

		// Update Prometheus metrics
		updateDCMetrics(dcState)

		if currentSession != nil {
			writeJSONRPCEvent("dcState", dcState, currentSession)
		}
	}
}

func setDCPowerState(on bool) error {
	_, err := port.Write([]byte("\n"))
	if err != nil {
		return err
	}
	command := "PWR_OFF\n"
	if on {
		command = "PWR_ON\n"
	}
	_, err = port.Write([]byte(command))
	if err != nil {
		return err
	}
	return nil
}

func setDCRestoreState(state int) error {
	_, err := port.Write([]byte("\n"))
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
	_, err = port.Write([]byte(command))
	if err != nil {
		return err
	}
	return nil
}

func sendCustomCommand(command string) error {
	scopedLogger := serialLogger.With().Str("service", "custom_buttons_tx").Logger()
	scopedLogger.Debug().Msgf("Sending custom command: %q", command)
	if serialMux == nil {
		return fmt.Errorf("serial mux not initialized")
	}
	payload := []byte(command)
	serialMux.Enqueue(payload, "button", true, TXUser) // echo if enabled
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
		return serialConfig, err
	}
	defer file.Close()

	// load and merge the default config with the user config
	var loadedConfig SerialSettings
	if err := json.NewDecoder(file).Decode(&loadedConfig); err != nil {
		logger.Warn().Err(err).Msg("SerialSettings file JSON parsing failed")
		return serialConfig, nil
	}

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

	_ = port.SetMode(serialPortMode)

	if serialMux != nil {
		serialMux.SetEchoEnabled(serialConfig.EnableEcho)
	}

	var normalizeMode NormalizeMode
	switch serialConfig.NormalizeMode {
	case "caret":
		normalizeMode = ModeCaret
	case "names":
		normalizeMode = ModeNames
	case "hex":
		normalizeMode = ModeHex
	default:
		normalizeMode = ModeNames
	}

	var crlfMode LineEndingMode
	switch serialConfig.NormalizeLineEnd {
	case "keep":
		crlfMode = LineEnding_AsIs
	case "lf":
		crlfMode = LineEnding_LF
	case "cr":
		crlfMode = LineEnding_CR
	case "crlf":
		crlfMode = LineEnding_CRLF
	case "lfcr":
		crlfMode = LineEnding_LFCR
	default:
		crlfMode = LineEnding_AsIs
	}

	if consoleBroker != nil {
		norm := NormalizationOptions{
			Mode: normalizeMode, LineEnding: crlfMode, TabRender: serialConfig.TabRender, PreserveANSI: serialConfig.PreserveANSI, ShowNLTag: serialConfig.ShowNLTag,
		}
		consoleBroker.SetNormOptions(norm)
	}

	return loadedConfig, nil
}

func setSerialSettings(newSettings SerialSettings) error {
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

	_ = port.SetMode(serialPortMode)

	serialConfig = newSettings // Update global config

	if serialMux != nil {
		serialMux.SetEchoEnabled(serialConfig.EnableEcho)
	}

	var normalizeMode NormalizeMode
	switch serialConfig.NormalizeMode {
	case "caret":
		normalizeMode = ModeCaret
	case "names":
		normalizeMode = ModeNames
	case "hex":
		normalizeMode = ModeHex
	default:
		normalizeMode = ModeNames
	}

	var crlfMode LineEndingMode
	switch serialConfig.NormalizeLineEnd {
	case "keep":
		crlfMode = LineEnding_AsIs
	case "lf":
		crlfMode = LineEnding_LF
	case "cr":
		crlfMode = LineEnding_CR
	case "crlf":
		crlfMode = LineEnding_CRLF
	case "lfcr":
		crlfMode = LineEnding_LFCR
	default:
		crlfMode = LineEnding_AsIs
	}

	if consoleBroker != nil {
		norm := NormalizationOptions{
			Mode: normalizeMode, LineEnding: crlfMode, TabRender: serialConfig.TabRender, PreserveANSI: serialConfig.PreserveANSI, ShowNLTag: serialConfig.ShowNLTag,
		}
		consoleBroker.SetNormOptions(norm)
	}

	return nil
}

func setTerminalPaused(paused bool) {
	if consoleBroker != nil {
		consoleBroker.SetTerminalPaused(paused)
	}
}

func initSerialPort() {
	_ = reopenSerialPort()
	switch config.ActiveExtension {
	case "atx-power":
		_ = mountATXControl()
	case "dc-power":
		_ = mountDCControl()
	}
}

func reopenSerialPort() error {
	if port != nil {
		port.Close()
	}
	var err error
	port, err = serial.Open(serialPortPath, defaultMode)
	if err != nil {
		serialLogger.Error().
			Err(err).
			Str("path", serialPortPath).
			Interface("mode", defaultMode).
			Msg("Error opening serial port")
		return err
	}

	// new broker (no sink yet—set it in handleSerialChannel.OnOpen)
	norm := NormalizationOptions{
		Mode: ModeNames, LineEnding: LineEnding_LF, TabRender: "", PreserveANSI: true,
	}
	if consoleBroker != nil {
		consoleBroker.Close()
	}
	consoleBroker = NewConsoleBroker(nil, norm)
	consoleBroker.Start()

	// new mux
	if serialMux != nil {
		serialMux.Close()
	}
	serialMux = NewSerialMux(port, consoleBroker)
	serialMux.SetEchoEnabled(serialConfig.EnableEcho) // honor your setting
	serialMux.Start()

	return nil
}

func handleSerialChannel(dataChannel *webrtc.DataChannel) {
	scopedLogger := serialLogger.With().
		Uint16("data_channel_id", *dataChannel.ID()).Str("service", "serial terminal channel").Logger()

	dataChannel.OnOpen(func() {
		// Plug the terminal sink into the broker
		scopedLogger.Info().Msg("Opening serial channel from console broker")
		if consoleBroker != nil {
			consoleBroker.SetSink(dataChannelSink{dataChannel: dataChannel})
			consoleBroker.Enqueue(consoleEvent{
				kind: evRX,
				data: []byte("[serial attached]\n"),
			})
			scopedLogger.Info().Msg("Serial channel is now active")
		}
	})

	dataChannel.OnMessage(func(msg webrtc.DataChannelMessage) {
		if serialMux == nil || consoleBroker == nil {
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
					scopedLogger.Warn().Err(err).Msg("Failed to decode serial payload string")
					return
				}

				// Write to UART (echo controlled by serialConfig.EnableEcho inside mux)
				serialMux.Enqueue([]byte(s), "webrtc", true, TXUser)

			case "system":
				// Display in terminal for debugging, but do NOT write to UART
				// Show raw JSON that arrived
				consoleBroker.Enqueue(consoleEvent{
					kind:   evTX,
					data:   append([]byte(nil), msg.Data...),
					origin: TXSystem,
				})

				// Optional: handle known system message
				if m.Name == "term.size" {
					scopedLogger.Trace().RawJSON("msg", msg.Data).Msg("Terminal size message received on serial channel")
				}

			default:
				// Unknown envelope type: show it as system/debug
				consoleBroker.Enqueue(consoleEvent{
					kind:   evTX,
					data:   append([]byte(nil), msg.Data...),
					origin: TXSystem,
				})
			}
			return
		}

		// Backward compatibility: treat non-envelope as raw serial bytes
		serialMux.Enqueue(msg.Data, "webrtc-raw", true, TXUser)
	})

	dataChannel.OnError(func(err error) {
		scopedLogger.Warn().Err(err).Msg("Serial channel error")
	})

	dataChannel.OnClose(func() {
		scopedLogger.Info().Msg("Serial channel closed")

		if consoleBroker != nil {
			consoleBroker.SetSink(nil)
		}
	})
}
