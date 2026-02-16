package kvm

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	halSerial "github.com/jetkvm/kvm/internal/hal/serial"
	"github.com/pion/webrtc/v4"
)

var (
	serialMu sync.Mutex // protects port, serialPortMode, and all port.Write/SetMode calls
	port     halSerial.Port
)

// atxLedState stores the last known ATX LED state for lock-free reads from RPC handlers.
var atxLedState atomic.Pointer[ATXState]

// dcPowerState stores the last known DC power state for lock-free reads from RPC handlers.
var dcPowerState atomic.Pointer[DCPowerState]

func mountATXControl() error {
	serialMu.Lock()
	if port == nil {
		serialMu.Unlock()
		return fmt.Errorf("serial port not open")
	}
	if err := port.SetMode(defaultMode); err != nil {
		serialMu.Unlock()
		return fmt.Errorf("failed to set serial mode: %w", err)
	}
	p := port // capture reference for goroutine
	serialMu.Unlock()
	go runATXControl(p)
	return nil
}

func unmountATXControl() error {
	return reopenSerialPort()
}

func runATXControl(p halSerial.Port) {
	scopedLogger := serialLogger.With().Str("service", "atx_control").Logger()

	reader := bufio.NewReader(p)

	// Local state for change detection (only accessed by this goroutine).
	var ledHDD, ledPWR, btnRST, btnPWR bool

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

		// Store atomically for RPC readers
		atxLedState.Store(&ATXState{
			Power: newLedPWRState,
			HDD:   newLedHDDState,
		})

		if s := currentSession.Load(); s != nil {
			writeJSONRPCEvent("atxState", ATXState{
				Power: newLedPWRState,
				HDD:   newLedHDDState,
			}, s)
		}

		if newLedHDDState != ledHDD ||
			newLedPWRState != ledPWR ||
			newBtnRSTState != btnRST ||
			newBtnPWRState != btnPWR {
			scopedLogger.Debug().
				Bool("hdd", newLedHDDState).
				Bool("pwr", newLedPWRState).
				Bool("rst", newBtnRSTState).
				Bool("pwr", newBtnPWRState).
				Msg("Status changed")

			// Update local state
			ledHDD = newLedHDDState
			ledPWR = newLedPWRState
			btnRST = newBtnRSTState
			btnPWR = newBtnPWRState
		}
	}
}

func pressATXPowerButton(duration time.Duration) error {
	serialMu.Lock()
	defer serialMu.Unlock()

	if port == nil {
		return fmt.Errorf("serial port not open")
	}

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
	serialMu.Lock()
	defer serialMu.Unlock()

	if port == nil {
		return fmt.Errorf("serial port not open")
	}

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
	serialMu.Lock()
	if port == nil {
		serialMu.Unlock()
		return fmt.Errorf("serial port not open")
	}
	if err := port.SetMode(defaultMode); err != nil {
		serialMu.Unlock()
		return fmt.Errorf("failed to set serial mode: %w", err)
	}
	p := port // capture reference for goroutine
	serialMu.Unlock()
	registerDCMetrics()
	go runDCControl(p)
	return nil
}

func unmountDCControl() error {
	return reopenSerialPort()
}

func runDCControl(p halSerial.Port) {
	scopedLogger := serialLogger.With().Str("service", "dc_control").Logger()
	reader := bufio.NewReader(p)
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

		state := DCPowerState{
			IsOn: powerState == 1,
		}

		if hasRestoreFeature {
			restoreState, err := strconv.Atoi(parts[4])
			if err != nil {
				scopedLogger.Warn().Err(err).Msg("Invalid restore state")
				continue
			}
			state.RestoreState = restoreState
		} else {
			// -1 means not supported
			state.RestoreState = -1
		}

		milliVolts, err := strconv.ParseFloat(parts[1], 64)
		if err != nil {
			scopedLogger.Warn().Err(err).Msg("Invalid voltage")
			continue
		}
		state.Voltage = milliVolts / 1000 // Convert mV to V

		milliAmps, err := strconv.ParseFloat(parts[2], 64)
		if err != nil {
			scopedLogger.Warn().Err(err).Msg("Invalid current")
			continue
		}
		state.Current = milliAmps / 1000 // Convert mA to A

		milliWatts, err := strconv.ParseFloat(parts[3], 64)
		if err != nil {
			scopedLogger.Warn().Err(err).Msg("Invalid power")
			continue
		}
		state.Power = milliWatts / 1000 // Convert mW to W

		// Store atomically for RPC readers
		dcPowerState.Store(&state)

		// Update Prometheus metrics
		updateDCMetrics(state)

		if s := currentSession.Load(); s != nil {
			writeJSONRPCEvent("dcState", state, s)
		}
	}
}

func setDCPowerState(on bool) error {
	serialMu.Lock()
	defer serialMu.Unlock()

	if port == nil {
		return fmt.Errorf("serial port not open")
	}

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
	serialMu.Lock()
	defer serialMu.Unlock()

	if port == nil {
		return fmt.Errorf("serial port not open")
	}

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

var defaultMode = halSerial.DefaultMode

func initSerialPort() {
	if err := reopenSerialPort(); err != nil {
		serialLogger.Error().Err(err).Msg("failed to open serial port during init")
	}
	switch loadCfg().ActiveExtension {
	case "atx-power":
		if err := mountATXControl(); err != nil {
			serialLogger.Error().Err(err).Msg("failed to mount ATX control")
		}
	case "dc-power":
		if err := mountDCControl(); err != nil {
			serialLogger.Error().Err(err).Msg("failed to mount DC control")
		}
	}
}

func reopenSerialPort() error {
	serialMu.Lock()
	defer serialMu.Unlock()

	if port != nil {
		_ = port.Close()
	}
	var err error
	port, err = halSerial.Open(halSerial.DefaultPortPath, defaultMode)
	if err != nil {
		port = nil
		serialLogger.Error().
			Err(err).
			Str("path", halSerial.DefaultPortPath).
			Interface("mode", defaultMode).
			Msg("Error opening serial port")
		return fmt.Errorf("failed to open serial port: %w", err)
	}
	return nil
}

func handleSerialChannel(d *webrtc.DataChannel) {
	scopedLogger := serialLogger.With().
		Uint16("data_channel_id", *d.ID()).Logger()

	d.OnOpen(func() {
		// Capture port reference under lock for the read goroutine.
		serialMu.Lock()
		p := port
		serialMu.Unlock()

		go func() {
			if p == nil {
				return
			}

			buf := make([]byte, 1024)
			for {
				n, err := p.Read(buf)
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
		serialMu.Lock()
		p := port
		serialMu.Unlock()

		if p == nil {
			return
		}
		_, err := p.Write(msg.Data)
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
