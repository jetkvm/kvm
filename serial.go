package kvm

import (
	"bufio"
	"io"
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
	reader := bufio.NewReader(port)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			logger.Errorf("Error reading from serial port: %v", err)
			return
		}

		// Each line should be 4 binary digits + newline
		if len(line) != 5 {
			logger.Warnf("Invalid line length: %d", len(line))
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

			logger.Debugf("Status changed: HDD LED: %v, PWR LED: %v, RST BTN: %v, PWR BTN: %v",
				newLedHDDState, newLedPWRState, newBtnRSTState, newBtnPWRState)

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
	go runDCControl()
	return nil
}

func unmountDCControl() error {
	_ = reopenSerialPort()
	return nil
}

var dcState DCPowerState

func runDCControl() {
	reader := bufio.NewReader(port)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			logger.Errorf("Error reading from serial port: %v", err)
			return
		}

		// Split the line by semicolon
		parts := strings.Split(strings.TrimSpace(line), ";")
		if len(parts) != 4 {
			logger.Warnf("Invalid line: %s", line)
			continue
		}

		// Parse new states
		powerState, err := strconv.Atoi(parts[0])
		if err != nil {
			logger.Warnf("Invalid power state: %v", err)
			continue
		}
		dcState.IsOn = powerState == 1
		milliVolts, err := strconv.ParseFloat(parts[1], 64)
		if err != nil {
			logger.Warnf("Invalid voltage: %v", err)
			continue
		}
		volts := milliVolts / 1000 // Convert mV to V

		milliAmps, err := strconv.ParseFloat(parts[2], 64)
		if err != nil {
			logger.Warnf("Invalid current: %v", err)
			continue
		}
		amps := milliAmps / 1000 // Convert mA to A

		milliWatts, err := strconv.ParseFloat(parts[3], 64)
		if err != nil {
			logger.Warnf("Invalid power: %v", err)
			continue
		}
		watts := milliWatts / 1000 // Convert mW to W

		dcState.Voltage = volts
		dcState.Current = amps
		dcState.Power = watts

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

var defaultMode = &serial.Mode{
	BaudRate: 115200,
	DataBits: 8,
	Parity:   serial.NoParity,
	StopBits: serial.OneStopBit,
}

func initSerialPort() {
	_ = reopenSerialPort()
	if config.ActiveExtension == "atx-power" {
		_ = mountATXControl()
	} else if config.ActiveExtension == "dc-power" {
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
		logger.Errorf("Error opening serial port: %v", err)
	}
	return nil
}

func handleSerialChannel(d *webrtc.DataChannel) {
	d.OnOpen(func() {
		go func() {
			buf := make([]byte, 1024)
			for {
				n, err := port.Read(buf)
				if err != nil {
					if err != io.EOF {
						logger.Errorf("Failed to read from serial port: %v", err)
					}
					break
				}
				err = d.Send(buf[:n])
				if err != nil {
					logger.Errorf("Failed to send serial output: %v", err)
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
			logger.Errorf("Failed to write to serial: %v", err)
		}
	})

	d.OnClose(func() {

	})
}
