package kvm

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/pion/webrtc/v4"
	"go.bug.st/serial"
)

const serialPortPath = "/dev/ttyS3"

var port serial.Port

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

func mountSerialButtons() error {
	_ = port.SetMode(defaultMode)
	startSerialButtonsRxLoop(currentSession)
	return nil
}

func unmountSerialButtons() error {
	stopSerialButtonsRxLoop()
	_ = reopenSerialPort()
	return nil
}

// ---- Serial Buttons RX fan-out (JSON-RPC events) ----
var serialButtonsRXStopCh chan struct{}

func startSerialButtonsRxLoop(session *Session) {
	scopedLogger := serialLogger.With().Str("service", "custom_buttons_rx").Logger()
	scopedLogger.Debug().Msg("Attempting to start RX reader.")
	// Stop previous loop if running
	if serialButtonsRXStopCh != nil {
		stopSerialButtonsRxLoop()
	}
	serialButtonsRXStopCh = make(chan struct{})

	go func() {
		buf := make([]byte, 4096)
		scopedLogger.Debug().Msg("Starting loop")

		for {
			select {
			case <-serialButtonsRXStopCh:
				return
			default:
				if currentSession == nil {
					time.Sleep(500 * time.Millisecond)
					continue
				}
				n, err := port.Read(buf)
				if err != nil {
					if err != io.EOF {
						scopedLogger.Debug().Err(err).Msg("serial RX read error")
					}
					time.Sleep(50 * time.Millisecond)
					continue
				}
				if n == 0 {
					continue
				}
				// Safe for any bytes: wrap in Base64
				b64 := base64.StdEncoding.EncodeToString(buf[:n])
				writeJSONRPCEvent("serial.rx", map[string]any{
					"base64": b64,
				}, currentSession)
			}
		}
	}()
}

func stopSerialButtonsRxLoop() {
	scopedLogger := serialLogger.With().Str("service", "custom_buttons_rx").Logger()
	scopedLogger.Debug().Msg("Stopping RX reader.")
	if serialButtonsRXStopCh != nil {
		close(serialButtonsRXStopCh)
		serialButtonsRXStopCh = nil
	}
}

func sendCustomCommand(command string, terminator string) error {
	scopedLogger := serialLogger.With().Str("service", "custom_buttons_tx").Logger()
	scopedLogger.Info().Str("Command", command).Msg("Sending custom command.")
	_, err := port.Write([]byte(terminator))
	if err != nil {
		scopedLogger.Warn().Err(err).Msg("Failed to send terminator")
		return err
	}
	_, err = port.Write([]byte(command))
	if err != nil {
		scopedLogger.Warn().Err(err).Str("Command", command).Msg("Failed to send serial command")
		return err
	}
	return nil
}

var defaultMode = &serial.Mode{
	BaudRate: 115200,
	DataBits: 8,
	Parity:   serial.NoParity,
	StopBits: serial.OneStopBit,
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
type CustomButtonSettings struct {
	BaudRate           string        `json:"baudRate"`           // The serial port bitrate (aka Baudrate)
	DataBits           string        `json:"dataBits"`           // Size of the character (must be 5, 6, 7 or 8)
	Parity             string        `json:"parity"`             // Parity (see Parity type for more info)
	StopBits           string        `json:"stopBits"`           // Stop bits (see StopBits type for more info)
	Terminator         Terminator    `json:"terminator"`         // Terminator to send after each command
	LineMode           bool          `json:"lineMode"`           // Whether to send each line when Enter is pressed, or each character immediately
	HideSerialSettings bool          `json:"hideSerialSettings"` // Whether to hide the serial settings in the UI
	EnableEcho         bool          `json:"enableEcho"`         // Whether to echo received characters back to the sender
	Buttons            []QuickButton `json:"buttons"`            // Custom quick buttons
}

func getSerialSettings() (CustomButtonSettings, error) {
	config := CustomButtonSettings{
		BaudRate:           strconv.Itoa(defaultMode.BaudRate),
		DataBits:           strconv.Itoa(defaultMode.DataBits),
		Parity:             "none",
		StopBits:           "1",
		Terminator:         Terminator{Label: "CR (\\r)", Value: "\r"},
		LineMode:           true,
		HideSerialSettings: false,
		EnableEcho:         false,
		Buttons:            []QuickButton{},
	}

	switch defaultMode.StopBits {
	case serial.OneStopBit:
		config.StopBits = "1"
	case serial.OnePointFiveStopBits:
		config.StopBits = "1.5"
	case serial.TwoStopBits:
		config.StopBits = "2"
	}

	switch defaultMode.Parity {
	case serial.NoParity:
		config.Parity = "none"
	case serial.OddParity:
		config.Parity = "odd"
	case serial.EvenParity:
		config.Parity = "even"
	case serial.MarkParity:
		config.Parity = "mark"
	case serial.SpaceParity:
		config.Parity = "space"
	}

	file, err := os.Open(serialSettingsPath)
	if err != nil {
		logger.Debug().Msg("SerialButtons config file doesn't exist, using default")
		return config, err
	}
	defer file.Close()

	// load and merge the default config with the user config
	var loadedConfig CustomButtonSettings
	if err := json.NewDecoder(file).Decode(&loadedConfig); err != nil {
		logger.Warn().Err(err).Msg("SerialButtons config file JSON parsing failed")
		return config, nil
	}

	return loadedConfig, nil
}

func setSerialSettings(newSettings CustomButtonSettings) error {
	logger.Trace().Str("path", serialSettingsPath).Msg("Saving config")

	file, err := os.Create(serialSettingsPath)
	if err != nil {
		return fmt.Errorf("failed to create SerialButtons config file: %w", err)
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(newSettings); err != nil {
		return fmt.Errorf("failed to encode SerialButtons config: %w", err)
	}

	baudRate, err := strconv.Atoi(newSettings.BaudRate)
	if err != nil {
		return fmt.Errorf("invalid baud rate: %v", err)
	}
	dataBits, err := strconv.Atoi(newSettings.DataBits)
	if err != nil {
		return fmt.Errorf("invalid data bits: %v", err)
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
		BaudRate: baudRate,
		DataBits: dataBits,
		StopBits: stopBits,
		Parity:   parity,
	}

	_ = port.SetMode(serialPortMode)

	return nil
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
	}
	return nil
}

func handleSerialChannel(d *webrtc.DataChannel) {
	scopedLogger := serialLogger.With().
		Uint16("data_channel_id", *d.ID()).Logger()

	d.OnOpen(func() {
		go func() {
			buf := make([]byte, 1024)
			for {
				n, err := port.Read(buf)
				if err != nil {
					if err != io.EOF {
						scopedLogger.Warn().Err(err).Msg("Failed to read from serial port")
					}
					break
				}
				err = d.Send(buf[:n])
				if err != nil {
					scopedLogger.Warn().Err(err).Msg("Failed to send serial output")
					break
				}
			}
		}()
	})

	d.OnMessage(func(msg webrtc.DataChannelMessage) {
		if port == nil {
			return
		}
		_, err := port.Write(msg.Data)
		if err != nil {
			scopedLogger.Warn().Err(err).Msg("Failed to write to serial")
		}
	})

	d.OnError(func(err error) {
		scopedLogger.Warn().Err(err).Msg("Serial channel error")
	})

	d.OnClose(func() {
		scopedLogger.Info().Msg("Serial channel closed")
	})
}
