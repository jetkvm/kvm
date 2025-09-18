package kvm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"sync"
	"time"

	"github.com/pion/webrtc/v4"
	"github.com/rs/zerolog"
	"go.bug.st/serial"

	"github.com/jetkvm/kvm/internal/hidrpc"
	"github.com/jetkvm/kvm/internal/usbgadget"
	"github.com/jetkvm/kvm/internal/utils"
)

type JSONRPCRequest struct {
	JSONRPC string         `json:"jsonrpc"`
	Method  string         `json:"method"`
	Params  map[string]any `json:"params,omitempty"`
	ID      any            `json:"id,omitempty"`
}

type JSONRPCResponse struct {
	JSONRPC string `json:"jsonrpc"`
	Result  any    `json:"result,omitempty"`
	Error   any    `json:"error,omitempty"`
	ID      any    `json:"id"`
}

type JSONRPCEvent struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type DisplayRotationSettings struct {
	Rotation string `json:"rotation"`
}

type BacklightSettings struct {
	MaxBrightness int `json:"max_brightness"`
	DimAfter      int `json:"dim_after"`
	OffAfter      int `json:"off_after"`
}

func writeJSONRPCResponse(response JSONRPCResponse, session *Session) {
	responseBytes, err := json.Marshal(response)
	if err != nil {
		jsonRpcLogger.Warn().Err(err).Msg("Error marshalling JSONRPC response")
		return
	}
	err = session.RPCChannel.SendText(string(responseBytes))
	if err != nil {
		jsonRpcLogger.Warn().Err(err).Msg("Error sending JSONRPC response")
		return
	}
}

func writeJSONRPCEvent(event string, params any, session *Session) {
	request := JSONRPCEvent{
		JSONRPC: "2.0",
		Method:  event,
		Params:  params,
	}
	requestBytes, err := json.Marshal(request)
	if err != nil {
		jsonRpcLogger.Warn().Err(err).Msg("Error marshalling JSONRPC event")
		return
	}
	if session == nil || session.RPCChannel == nil {
		jsonRpcLogger.Info().Msg("RPC channel not available")
		return
	}

	requestString := string(requestBytes)
	scopedLogger := jsonRpcLogger.With().
		Str("data", requestString).
		Logger()

	scopedLogger.Trace().Msg("sending JSONRPC event")

	err = session.RPCChannel.SendText(requestString)
	if err != nil {
		scopedLogger.Warn().Err(err).Msg("error sending JSONRPC event")
		return
	}
}

func onRPCMessage(message webrtc.DataChannelMessage, session *Session) {
	var request JSONRPCRequest
	err := json.Unmarshal(message.Data, &request)
	if err != nil {
		jsonRpcLogger.Warn().
			Str("data", string(message.Data)).
			Err(err).
			Msg("Error unmarshalling JSONRPC request")

		errorResponse := JSONRPCResponse{
			JSONRPC: "2.0",
			Error: map[string]any{
				"code":    -32700,
				"message": "Parse error",
			},
			ID: 0,
		}
		writeJSONRPCResponse(errorResponse, session)
		return
	}

	scopedLogger := jsonRpcLogger.With().
		Str("method", request.Method).
		Interface("params", request.Params).
		Interface("id", request.ID).Logger()

	scopedLogger.Trace().Msg("Received RPC request")

	handler, ok := rpcHandlers[request.Method]
	if !ok {
		errorResponse := JSONRPCResponse{
			JSONRPC: "2.0",
			Error: map[string]any{
				"code":    -32601,
				"message": "Method not found",
			},
			ID: request.ID,
		}
		writeJSONRPCResponse(errorResponse, session)
		return
	}

	result, err := callRPCHandler(scopedLogger, handler, request.Params)
	if err != nil {
		scopedLogger.Error().Err(err).Msg("Error calling RPC handler")
		errorResponse := JSONRPCResponse{
			JSONRPC: "2.0",
			Error: map[string]any{
				"code":    -32603,
				"message": "Internal error",
				"data":    err.Error(),
			},
			ID: request.ID,
		}
		writeJSONRPCResponse(errorResponse, session)
		return
	}

	scopedLogger.Trace().Interface("result", result).Msg("RPC handler returned")

	response := JSONRPCResponse{
		JSONRPC: "2.0",
		Result:  result,
		ID:      request.ID,
	}
	writeJSONRPCResponse(response, session)
}

func rpcPing() (string, error) {
	return "pong", nil
}

func rpcGetDeviceID() (string, error) {
	return GetDeviceID(), nil
}

func rpcReboot(force bool) error {
	logger.Info().Msg("Got reboot request from JSONRPC, rebooting...")

	args := []string{}
	if force {
		args = append(args, "-f")
	}

	cmd := exec.Command("reboot", args...)
	err := cmd.Start()
	if err != nil {
		logger.Error().Err(err).Msg("failed to reboot")
		return fmt.Errorf("failed to reboot: %w", err)
	}

	// If the reboot command is successful, exit the program after 5 seconds
	go func() {
		time.Sleep(5 * time.Second)
		os.Exit(0)
	}()

	return nil
}

var streamFactor = 1.0

func rpcGetStreamQualityFactor() (float64, error) {
	return streamFactor, nil
}

func rpcSetStreamQualityFactor(factor float64) error {
	logger.Info().Float64("factor", factor).Msg("Setting stream quality factor")
	var _, err = CallCtrlAction("set_video_quality_factor", map[string]any{"quality_factor": factor})
	if err != nil {
		return err
	}

	streamFactor = factor
	return nil
}

func rpcGetAutoUpdateState() (bool, error) {
	return config.AutoUpdateEnabled, nil
}

func rpcSetAutoUpdateState(enabled bool) (bool, error) {
	config.AutoUpdateEnabled = enabled
	if err := SaveConfig(); err != nil {
		return config.AutoUpdateEnabled, fmt.Errorf("failed to save config: %w", err)
	}
	return enabled, nil
}

func rpcGetEDID() (string, error) {
	resp, err := CallCtrlAction("get_edid", nil)
	if err != nil {
		return "", err
	}
	edid, ok := resp.Result["edid"]
	if ok {
		return edid.(string), nil
	}
	return "", errors.New("EDID not found in response")
}

func rpcSetEDID(edid string) error {
	if edid == "" {
		logger.Info().Msg("Restoring EDID to default")
		edid = "00ffffffffffff0052620188008888881c150103800000780a0dc9a05747982712484c00000001010101010101010101010101010101023a801871382d40582c4500c48e2100001e011d007251d01e206e285500c48e2100001e000000fc00543734392d6648443732300a20000000fd00147801ff1d000a202020202020017b"
	} else {
		logger.Info().Str("edid", edid).Msg("Setting EDID")
	}
	_, err := CallCtrlAction("set_edid", map[string]any{"edid": edid})
	if err != nil {
		return err
	}

	// Save EDID to config, allowing it to be restored on reboot.
	config.EdidString = edid
	_ = SaveConfig()
	return nil
}

func rpcGetDevChannelState() (bool, error) {
	return config.IncludePreRelease, nil
}

func rpcSetDevChannelState(enabled bool) error {
	config.IncludePreRelease = enabled
	if err := SaveConfig(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}
	return nil
}

func rpcGetUpdateStatus() (*UpdateStatus, error) {
	includePreRelease := config.IncludePreRelease
	updateStatus, err := GetUpdateStatus(context.Background(), GetDeviceID(), includePreRelease)
	// to ensure backwards compatibility,
	// if there's an error, we won't return an error, but we will set the error field
	if err != nil {
		if updateStatus == nil {
			return nil, fmt.Errorf("error checking for updates: %w", err)
		}
		updateStatus.Error = err.Error()
	}

	return updateStatus, nil
}

func rpcTryUpdate() error {
	includePreRelease := config.IncludePreRelease
	go func() {
		err := TryUpdate(context.Background(), GetDeviceID(), includePreRelease)
		if err != nil {
			logger.Warn().Err(err).Msg("failed to try update")
		}
	}()
	return nil
}

func rpcSetDisplayRotation(params DisplayRotationSettings) error {
	var err error
	_, err = lvDispSetRotation(params.Rotation)
	if err == nil {
		config.DisplayRotation = params.Rotation
		if err := SaveConfig(); err != nil {
			return fmt.Errorf("failed to save config: %w", err)
		}
	}
	return err
}

func rpcGetDisplayRotation() (*DisplayRotationSettings, error) {
	return &DisplayRotationSettings{
		Rotation: config.DisplayRotation,
	}, nil
}

func rpcSetBacklightSettings(params BacklightSettings) error {
	blConfig := params

	// NOTE: by default, the frontend limits the brightness to 64, as that's what the device originally shipped with.
	if blConfig.MaxBrightness > 255 || blConfig.MaxBrightness < 0 {
		return fmt.Errorf("maxBrightness must be between 0 and 255")
	}

	if blConfig.DimAfter < 0 {
		return fmt.Errorf("dimAfter must be a positive integer")
	}

	if blConfig.OffAfter < 0 {
		return fmt.Errorf("offAfter must be a positive integer")
	}

	config.DisplayMaxBrightness = blConfig.MaxBrightness
	config.DisplayDimAfterSec = blConfig.DimAfter
	config.DisplayOffAfterSec = blConfig.OffAfter

	if err := SaveConfig(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	logger.Info().Int("max_brightness", config.DisplayMaxBrightness).Int("dim_after", config.DisplayDimAfterSec).Int("off_after", config.DisplayOffAfterSec).Msg("rpc: display: settings applied")

	// If the device started up with auto-dim and/or auto-off set to zero, the display init
	// method will not have started the tickers. So in case that has changed, attempt to start the tickers now.
	startBacklightTickers()

	// Wake the display after the settings are altered, this ensures the tickers
	// are reset to the new settings, and will bring the display up to maxBrightness.
	// Calling with force set to true, to ignore the current state of the display, and force
	// it to reset the tickers.
	wakeDisplay(true)
	return nil
}

func rpcGetBacklightSettings() (*BacklightSettings, error) {
	return &BacklightSettings{
		MaxBrightness: config.DisplayMaxBrightness,
		DimAfter:      int(config.DisplayDimAfterSec),
		OffAfter:      int(config.DisplayOffAfterSec),
	}, nil
}

const (
	devModeFile = "/userdata/jetkvm/devmode.enable"
	sshKeyDir   = "/userdata/dropbear/.ssh"
	sshKeyFile  = "/userdata/dropbear/.ssh/authorized_keys"
)

type DevModeState struct {
	Enabled bool `json:"enabled"`
}

type SSHKeyState struct {
	SSHKey string `json:"sshKey"`
}

func rpcGetDevModeState() (DevModeState, error) {
	devModeEnabled := false
	if _, err := os.Stat(devModeFile); err != nil {
		if !os.IsNotExist(err) {
			return DevModeState{}, fmt.Errorf("error checking dev mode file: %w", err)
		}
	} else {
		devModeEnabled = true
	}

	return DevModeState{
		Enabled: devModeEnabled,
	}, nil
}

func rpcSetDevModeState(enabled bool) error {
	if enabled {
		if _, err := os.Stat(devModeFile); os.IsNotExist(err) {
			if err := os.MkdirAll(filepath.Dir(devModeFile), 0755); err != nil {
				return fmt.Errorf("failed to create directory for devmode file: %w", err)
			}
			if err := os.WriteFile(devModeFile, []byte{}, 0644); err != nil {
				return fmt.Errorf("failed to create devmode file: %w", err)
			}
		} else {
			logger.Debug().Msg("dev mode already enabled")
			return nil
		}
	} else {
		if _, err := os.Stat(devModeFile); err == nil {
			if err := os.Remove(devModeFile); err != nil {
				return fmt.Errorf("failed to remove devmode file: %w", err)
			}
		} else if os.IsNotExist(err) {
			logger.Debug().Msg("dev mode already disabled")
			return nil
		} else {
			return fmt.Errorf("error checking dev mode file: %w", err)
		}
	}

	cmd := exec.Command("dropbear.sh")
	output, err := cmd.CombinedOutput()
	if err != nil {
		logger.Warn().Err(err).Bytes("output", output).Msg("Failed to start/stop SSH")
		return fmt.Errorf("failed to start/stop SSH, you may need to reboot for changes to take effect")
	}

	return nil
}

func rpcGetSSHKeyState() (string, error) {
	keyData, err := os.ReadFile(sshKeyFile)
	if err != nil {
		if !os.IsNotExist(err) {
			return "", fmt.Errorf("error reading SSH key file: %w", err)
		}
	}
	return string(keyData), nil
}

func rpcSetSSHKeyState(sshKey string) error {
	if sshKey == "" {
		// Remove SSH key file if empty string is provided
		if err := os.Remove(sshKeyFile); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove SSH key file: %w", err)
		}
		return nil
	}

	// Validate SSH key
	if err := utils.ValidateSSHKey(sshKey); err != nil {
		return err
	}

	// Create directory if it doesn't exist
	if err := os.MkdirAll(sshKeyDir, 0700); err != nil {
		return fmt.Errorf("failed to create SSH key directory: %w", err)
	}

	// Write SSH key to file
	if err := os.WriteFile(sshKeyFile, []byte(sshKey), 0600); err != nil {
		return fmt.Errorf("failed to write SSH key: %w", err)
	}

	return nil
}

func rpcGetTLSState() TLSState {
	return getTLSState()
}

func rpcSetTLSState(state TLSState) error {
	err := setTLSState(state)
	if err != nil {
		return fmt.Errorf("failed to set TLS state: %w", err)
	}

	if err := SaveConfig(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	return nil
}

type RPCHandler struct {
	Func   any
	Params []string
}

// call the handler but recover from a panic to ensure our RPC thread doesn't collapse on malformed calls
func callRPCHandler(logger zerolog.Logger, handler RPCHandler, params map[string]any) (result any, err error) {
	// Use defer to recover from a panic
	defer func() {
		if r := recover(); r != nil {
			// Convert the panic to an error
			if e, ok := r.(error); ok {
				err = e
			} else {
				err = fmt.Errorf("panic occurred: %v", r)
			}
		}
	}()

	// Call the handler
	result, err = riskyCallRPCHandler(logger, handler, params)
	return result, err // do not combine these two lines into one, as it breaks the above defer function's setting of err
}

func riskyCallRPCHandler(logger zerolog.Logger, handler RPCHandler, params map[string]any) (any, error) {
	handlerValue := reflect.ValueOf(handler.Func)
	handlerType := handlerValue.Type()

	if handlerType.Kind() != reflect.Func {
		return nil, errors.New("handler is not a function")
	}

	numParams := handlerType.NumIn()
	paramNames := handler.Params // Get the parameter names from the RPCHandler

	if len(paramNames) != numParams {
		err := fmt.Errorf("mismatch between handler parameters (%d) and defined parameter names (%d)", numParams, len(paramNames))
		logger.Error().Strs("paramNames", paramNames).Err(err).Msg("Cannot call RPC handler")
		return nil, err
	}

	args := make([]reflect.Value, numParams)

	for i := range numParams {
		paramType := handlerType.In(i)
		paramName := paramNames[i]
		paramValue, ok := params[paramName]
		if !ok {
			err := fmt.Errorf("missing parameter: %s", paramName)
			logger.Error().Err(err).Msg("Cannot marshal arguments for RPC handler")
			return nil, err
		}

		convertedValue := reflect.ValueOf(paramValue)
		if !convertedValue.Type().ConvertibleTo(paramType) {
			if paramType.Kind() == reflect.Slice && (convertedValue.Kind() == reflect.Slice || convertedValue.Kind() == reflect.Array) {
				newSlice := reflect.MakeSlice(paramType, convertedValue.Len(), convertedValue.Len())
				for j := 0; j < convertedValue.Len(); j++ {
					elemValue := convertedValue.Index(j)
					if elemValue.Kind() == reflect.Interface {
						elemValue = elemValue.Elem()
					}
					if !elemValue.Type().ConvertibleTo(paramType.Elem()) {
						// Handle float64 to uint8 conversion
						if elemValue.Kind() == reflect.Float64 && paramType.Elem().Kind() == reflect.Uint8 {
							intValue := int(elemValue.Float())
							if intValue < 0 || intValue > 255 {
								return nil, fmt.Errorf("value out of range for uint8: %v for parameter %s", intValue, paramName)
							}
							newSlice.Index(j).SetUint(uint64(intValue))
						} else {
							fromType := elemValue.Type()
							toType := paramType.Elem()
							return nil, fmt.Errorf("invalid element type in slice for parameter %s: from %v to %v", paramName, fromType, toType)
						}
					} else {
						newSlice.Index(j).Set(elemValue.Convert(paramType.Elem()))
					}
				}
				args[i] = newSlice
			} else if paramType.Kind() == reflect.Struct && convertedValue.Kind() == reflect.Map {
				jsonData, err := json.Marshal(convertedValue.Interface())
				if err != nil {
					return nil, fmt.Errorf("failed to marshal map to JSON: %v for parameter %s", err, paramName)
				}

				newStruct := reflect.New(paramType).Interface()
				if err := json.Unmarshal(jsonData, newStruct); err != nil {
					return nil, fmt.Errorf("failed to unmarshal JSON into struct: %v for parameter %s", err, paramName)
				}
				args[i] = reflect.ValueOf(newStruct).Elem()
			} else {
				return nil, fmt.Errorf("invalid parameter type for: %s, type: %s", paramName, paramType.Kind())
			}
		} else {
			args[i] = convertedValue.Convert(paramType)
		}
	}

	logger.Trace().Msg("Calling RPC handler")
	results := handlerValue.Call(args)

	if len(results) == 0 {
		return nil, nil
	}

	if len(results) == 1 {
		if ok, err := asError(results[0]); ok {
			return nil, err
		}
		return results[0].Interface(), nil
	}

	if len(results) == 2 {
		if ok, err := asError(results[1]); ok {
			if err != nil {
				return nil, err
			}
		}
		return results[0].Interface(), nil
	}

	return nil, fmt.Errorf("too many return values from handler: %d", len(results))
}

func asError(value reflect.Value) (bool, error) {
	if value.Type().Implements(reflect.TypeOf((*error)(nil)).Elem()) {
		if value.IsNil() {
			return true, nil
		}
		return true, value.Interface().(error)
	}
	return false, nil
}

func rpcSetMassStorageMode(mode string) (string, error) {
	logger.Info().Str("mode", mode).Msg("Setting mass storage mode")
	var cdrom bool
	switch mode {
	case "cdrom":
		cdrom = true
	case "file":
		cdrom = false
	default:
		logger.Info().Str("mode", mode).Msg("Invalid mode provided")
		return "", fmt.Errorf("invalid mode: %s", mode)
	}

	logger.Info().Str("mode", mode).Msg("Setting mass storage mode")

	err := setMassStorageMode(cdrom)
	if err != nil {
		return "", fmt.Errorf("failed to set mass storage mode: %w", err)
	}

	logger.Info().Str("mode", mode).Msg("Mass storage mode set")

	// Get the updated mode after setting
	return rpcGetMassStorageMode()
}

func rpcGetMassStorageMode() (string, error) {
	cdrom, err := getMassStorageCDROMEnabled()
	if err != nil {
		return "", fmt.Errorf("failed to get mass storage mode: %w", err)
	}

	mode := "file"
	if cdrom {
		mode = "cdrom"
	}
	return mode, nil
}

func rpcIsUpdatePending() (bool, error) {
	return IsUpdatePending(), nil
}

func rpcGetUsbEmulationState() (bool, error) {
	return gadget.IsUDCBound()
}

func rpcSetUsbEmulationState(enabled bool) error {
	if enabled {
		return gadget.BindUDC()
	} else {
		return gadget.UnbindUDC()
	}
}

func rpcGetUsbConfig() (usbgadget.Config, error) {
	LoadConfig()
	return *config.UsbConfig, nil
}

func rpcSetUsbConfig(usbConfig usbgadget.Config) error {
	LoadConfig()
	config.UsbConfig = &usbConfig
	gadget.SetGadgetConfig(config.UsbConfig)
	return updateUsbRelatedConfig()
}

func rpcGetWakeOnLanDevices() ([]WakeOnLanDevice, error) {
	if config.WakeOnLanDevices == nil {
		return []WakeOnLanDevice{}, nil
	}
	return config.WakeOnLanDevices, nil
}

type SetWakeOnLanDevicesParams struct {
	Devices []WakeOnLanDevice `json:"devices"`
}

func rpcSetWakeOnLanDevices(params SetWakeOnLanDevicesParams) error {
	config.WakeOnLanDevices = params.Devices
	return SaveConfig()
}

func rpcResetConfig() error {
	config = defaultConfig
	if err := SaveConfig(); err != nil {
		return fmt.Errorf("failed to reset config: %w", err)
	}

	logger.Info().Msg("Configuration reset to default")
	return nil
}

type DCPowerState struct {
	IsOn         bool    `json:"isOn"`
	Voltage      float64 `json:"voltage"`
	Current      float64 `json:"current"`
	Power        float64 `json:"power"`
	RestoreState int     `json:"restoreState"`
}

func rpcGetDCPowerState() (DCPowerState, error) {
	return dcState, nil
}

func rpcSetDCPowerState(enabled bool) error {
	logger.Info().Bool("enabled", enabled).Msg("Setting DC power state")
	err := setDCPowerState(enabled)
	if err != nil {
		return fmt.Errorf("failed to set DC power state: %w", err)
	}
	return nil
}

func rpcSetDCRestoreState(state int) error {
	logger.Info().Int("state", state).Msg("Setting DC restore state")
	err := setDCRestoreState(state)
	if err != nil {
		return fmt.Errorf("failed to set DC restore state: %w", err)
	}
	return nil
}

func rpcGetActiveExtension() (string, error) {
	return config.ActiveExtension, nil
}

func rpcSetActiveExtension(extensionId string) error {
	if config.ActiveExtension == extensionId {
		return nil
	}
	switch config.ActiveExtension {
	case "atx-power":
		_ = unmountATXControl()
	case "dc-power":
		_ = unmountDCControl()
	}
	config.ActiveExtension = extensionId
	if err := SaveConfig(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}
	switch extensionId {
	case "atx-power":
		_ = mountATXControl()
	case "dc-power":
		_ = mountDCControl()
	}
	return nil
}

func rpcSetATXPowerAction(action string) error {
	logger.Debug().Str("action", action).Msg("Executing ATX power action")
	switch action {
	case "power-short":
		logger.Debug().Msg("Simulating short power button press")
		return pressATXPowerButton(200 * time.Millisecond)
	case "power-long":
		logger.Debug().Msg("Simulating long power button press")
		return pressATXPowerButton(5 * time.Second)
	case "reset":
		logger.Debug().Msg("Simulating reset button press")
		return pressATXResetButton(200 * time.Millisecond)
	default:
		return fmt.Errorf("invalid action: %s", action)
	}
}

type ATXState struct {
	Power bool `json:"power"`
	HDD   bool `json:"hdd"`
}

func rpcGetATXState() (ATXState, error) {
	state := ATXState{
		Power: ledPWRState,
		HDD:   ledHDDState,
	}
	return state, nil
}

type SerialSettings struct {
	BaudRate string `json:"baudRate"`
	DataBits string `json:"dataBits"`
	StopBits string `json:"stopBits"`
	Parity   string `json:"parity"`
}

func rpcGetSerialSettings() (SerialSettings, error) {
	settings := SerialSettings{
		BaudRate: strconv.Itoa(serialPortMode.BaudRate),
		DataBits: strconv.Itoa(serialPortMode.DataBits),
		StopBits: "1",
		Parity:   "none",
	}

	switch serialPortMode.StopBits {
	case serial.OneStopBit:
		settings.StopBits = "1"
	case serial.OnePointFiveStopBits:
		settings.StopBits = "1.5"
	case serial.TwoStopBits:
		settings.StopBits = "2"
	}

	switch serialPortMode.Parity {
	case serial.NoParity:
		settings.Parity = "none"
	case serial.OddParity:
		settings.Parity = "odd"
	case serial.EvenParity:
		settings.Parity = "even"
	case serial.MarkParity:
		settings.Parity = "mark"
	case serial.SpaceParity:
		settings.Parity = "space"
	}

	return settings, nil
}

var serialPortMode = defaultMode

func rpcSetSerialSettings(settings SerialSettings) error {
	baudRate, err := strconv.Atoi(settings.BaudRate)
	if err != nil {
		return fmt.Errorf("invalid baud rate: %v", err)
	}
	dataBits, err := strconv.Atoi(settings.DataBits)
	if err != nil {
		return fmt.Errorf("invalid data bits: %v", err)
	}

	var stopBits serial.StopBits
	switch settings.StopBits {
	case "1":
		stopBits = serial.OneStopBit
	case "1.5":
		stopBits = serial.OnePointFiveStopBits
	case "2":
		stopBits = serial.TwoStopBits
	default:
		return fmt.Errorf("invalid stop bits: %s", settings.StopBits)
	}

	var parity serial.Parity
	switch settings.Parity {
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
		return fmt.Errorf("invalid parity: %s", settings.Parity)
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

func rpcGetUsbDevices() (usbgadget.Devices, error) {
	return *config.UsbDevices, nil
}

func updateUsbRelatedConfig() error {
	if err := gadget.UpdateGadgetConfig(); err != nil {
		return fmt.Errorf("failed to write gadget config: %w", err)
	}
	if err := SaveConfig(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}
	return nil
}

func rpcSetUsbDevices(usbDevices usbgadget.Devices) error {
	config.UsbDevices = &usbDevices
	gadget.SetGadgetDevices(config.UsbDevices)
	return updateUsbRelatedConfig()
}

func rpcSetUsbDeviceState(device string, enabled bool) error {
	switch device {
	case "absoluteMouse":
		config.UsbDevices.AbsoluteMouse = enabled
	case "relativeMouse":
		config.UsbDevices.RelativeMouse = enabled
	case "keyboard":
		config.UsbDevices.Keyboard = enabled
	case "massStorage":
		config.UsbDevices.MassStorage = enabled
	default:
		return fmt.Errorf("invalid device: %s", device)
	}
	gadget.SetGadgetDevices(config.UsbDevices)
	return updateUsbRelatedConfig()
}

func rpcSetCloudUrl(apiUrl string, appUrl string) error {
	currentCloudURL := config.CloudURL
	config.CloudURL = apiUrl
	config.CloudAppURL = appUrl

	if currentCloudURL != apiUrl {
		disconnectCloud(fmt.Errorf("cloud url changed from %s to %s", currentCloudURL, apiUrl))
	}

	if err := SaveConfig(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	return nil
}

func rpcGetKeyboardLayout() (string, error) {
	return config.KeyboardLayout, nil
}

func rpcSetKeyboardLayout(layout string) error {
	config.KeyboardLayout = layout
	if err := SaveConfig(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}
	return nil
}

func getKeyboardMacros() (any, error) {
	macros := make([]KeyboardMacro, len(config.KeyboardMacros))
	copy(macros, config.KeyboardMacros)

	return macros, nil
}

type KeyboardMacrosParams struct {
	Macros []any `json:"macros"`
}

func setKeyboardMacros(params KeyboardMacrosParams) (any, error) {
	if params.Macros == nil {
		return nil, fmt.Errorf("missing or invalid macros parameter")
	}

	newMacros := make([]KeyboardMacro, 0, len(params.Macros))

	for i, item := range params.Macros {
		macroMap, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("invalid macro at index %d", i)
		}

		id, _ := macroMap["id"].(string)
		if id == "" {
			id = fmt.Sprintf("macro-%d", time.Now().UnixNano())
		}

		name, _ := macroMap["name"].(string)

		sortOrder := i + 1
		if sortOrderFloat, ok := macroMap["sortOrder"].(float64); ok {
			sortOrder = int(sortOrderFloat)
		}

		steps := []KeyboardMacroStep{}
		if stepsArray, ok := macroMap["steps"].([]any); ok {
			for _, stepItem := range stepsArray {
				stepMap, ok := stepItem.(map[string]any)
				if !ok {
					continue
				}

				step := KeyboardMacroStep{}

				if keysArray, ok := stepMap["keys"].([]any); ok {
					for _, k := range keysArray {
						if keyStr, ok := k.(string); ok {
							step.Keys = append(step.Keys, keyStr)
						}
					}
				}

				if modsArray, ok := stepMap["modifiers"].([]any); ok {
					for _, m := range modsArray {
						if modStr, ok := m.(string); ok {
							step.Modifiers = append(step.Modifiers, modStr)
						}
					}
				}

				if delay, ok := stepMap["delay"].(float64); ok {
					step.Delay = int(delay)
				}

				steps = append(steps, step)
			}
		}

		macro := KeyboardMacro{
			ID:        id,
			Name:      name,
			Steps:     steps,
			SortOrder: sortOrder,
		}

		if err := macro.Validate(); err != nil {
			return nil, fmt.Errorf("invalid macro at index %d: %w", i, err)
		}

		newMacros = append(newMacros, macro)
	}

	config.KeyboardMacros = newMacros

	if err := SaveConfig(); err != nil {
		return nil, err
	}

	return nil, nil
}

func rpcGetLocalLoopbackOnly() (bool, error) {
	return config.LocalLoopbackOnly, nil
}

func rpcSetLocalLoopbackOnly(enabled bool) error {
	// Check if the setting is actually changing
	if config.LocalLoopbackOnly == enabled {
		return nil
	}

	// Update the setting
	config.LocalLoopbackOnly = enabled
	if err := SaveConfig(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	return nil
}

var (
	keyboardMacroCancel context.CancelFunc
	keyboardMacroLock   sync.Mutex
)

// cancelKeyboardMacro cancels any ongoing keyboard macro execution
func cancelKeyboardMacro() {
	keyboardMacroLock.Lock()
	defer keyboardMacroLock.Unlock()

	if keyboardMacroCancel != nil {
		keyboardMacroCancel()
		logger.Info().Msg("canceled keyboard macro")
		keyboardMacroCancel = nil
	}
}

func setKeyboardMacroCancel(cancel context.CancelFunc) {
	keyboardMacroLock.Lock()
	defer keyboardMacroLock.Unlock()

	keyboardMacroCancel = cancel
}

func rpcExecuteKeyboardMacro(macro []hidrpc.KeyboardMacroStep) error {
	cancelKeyboardMacro()

	ctx, cancel := context.WithCancel(context.Background())
	setKeyboardMacroCancel(cancel)

	s := hidrpc.KeyboardMacroState{
		State:   true,
		IsPaste: true,
	}

	if currentSession != nil {
		currentSession.reportHidRPCKeyboardMacroState(s)
	}

	err := rpcDoExecuteKeyboardMacro(ctx, macro)

	setKeyboardMacroCancel(nil)

	s.State = false
	if currentSession != nil {
		currentSession.reportHidRPCKeyboardMacroState(s)
	}

	return err
}

func rpcCancelKeyboardMacro() {
	cancelKeyboardMacro()
}

var keyboardClearStateKeys = make([]byte, hidrpc.HidKeyBufferSize)

func isClearKeyStep(step hidrpc.KeyboardMacroStep) bool {
	return step.Modifier == 0 && bytes.Equal(step.Keys, keyboardClearStateKeys)
}

func rpcDoExecuteKeyboardMacro(ctx context.Context, macro []hidrpc.KeyboardMacroStep) error {
	logger.Debug().Interface("macro", macro).Msg("Executing keyboard macro")

	for i, step := range macro {
		delay := time.Duration(step.Delay) * time.Millisecond

		err := rpcKeyboardReport(step.Modifier, step.Keys)
		if err != nil {
			logger.Warn().Err(err).Msg("failed to execute keyboard macro")
			return err
		}

		// notify the device that the keyboard state is being cleared
		if isClearKeyStep(step) {
			gadget.UpdateKeysDown(0, keyboardClearStateKeys)
		}

		// Use context-aware sleep that can be cancelled
		select {
		case <-time.After(delay):
			// Sleep completed normally
		case <-ctx.Done():
			// make sure keyboard state is reset
			err := rpcKeyboardReport(0, keyboardClearStateKeys)
			if err != nil {
				logger.Warn().Err(err).Msg("failed to reset keyboard state")
			}

			logger.Debug().Int("step", i).Msg("Keyboard macro cancelled during sleep")
			return ctx.Err()
		}
	}

	return nil
}

var rpcHandlers = map[string]RPCHandler{
	"ping":                   {Func: rpcPing},
	"reboot":                 {Func: rpcReboot, Params: []string{"force"}},
	"getDeviceID":            {Func: rpcGetDeviceID},
	"deregisterDevice":       {Func: rpcDeregisterDevice},
	"getCloudState":          {Func: rpcGetCloudState},
	"getNetworkState":        {Func: rpcGetNetworkState},
	"getNetworkSettings":     {Func: rpcGetNetworkSettings},
	"setNetworkSettings":     {Func: rpcSetNetworkSettings, Params: []string{"settings"}},
	"renewDHCPLease":         {Func: rpcRenewDHCPLease},
	"getKeyboardLedState":    {Func: rpcGetKeyboardLedState},
	"getKeyDownState":        {Func: rpcGetKeysDownState},
	"absMouseReport":         {Func: rpcAbsMouseReport, Params: []string{"x", "y", "buttons"}},
	"relMouseReport":         {Func: rpcRelMouseReport, Params: []string{"dx", "dy", "buttons"}},
	"wheelReport":            {Func: rpcWheelReport, Params: []string{"wheelY"}},
	"getVideoState":          {Func: rpcGetVideoState},
	"getUSBState":            {Func: rpcGetUSBState},
	"unmountImage":           {Func: rpcUnmountImage},
	"rpcMountBuiltInImage":   {Func: rpcMountBuiltInImage, Params: []string{"filename"}},
	"setJigglerState":        {Func: rpcSetJigglerState, Params: []string{"enabled"}},
	"getJigglerState":        {Func: rpcGetJigglerState},
	"setJigglerConfig":       {Func: rpcSetJigglerConfig, Params: []string{"jigglerConfig"}},
	"getJigglerConfig":       {Func: rpcGetJigglerConfig},
	"getTimezones":           {Func: rpcGetTimezones},
	"sendWOLMagicPacket":     {Func: rpcSendWOLMagicPacket, Params: []string{"macAddress"}},
	"getStreamQualityFactor": {Func: rpcGetStreamQualityFactor},
	"setStreamQualityFactor": {Func: rpcSetStreamQualityFactor, Params: []string{"factor"}},
	"getAutoUpdateState":     {Func: rpcGetAutoUpdateState},
	"setAutoUpdateState":     {Func: rpcSetAutoUpdateState, Params: []string{"enabled"}},
	"getEDID":                {Func: rpcGetEDID},
	"setEDID":                {Func: rpcSetEDID, Params: []string{"edid"}},
	"getDevChannelState":     {Func: rpcGetDevChannelState},
	"setDevChannelState":     {Func: rpcSetDevChannelState, Params: []string{"enabled"}},
	"getUpdateStatus":        {Func: rpcGetUpdateStatus},
	"tryUpdate":              {Func: rpcTryUpdate},
	"getDevModeState":        {Func: rpcGetDevModeState},
	"setDevModeState":        {Func: rpcSetDevModeState, Params: []string{"enabled"}},
	"getSSHKeyState":         {Func: rpcGetSSHKeyState},
	"setSSHKeyState":         {Func: rpcSetSSHKeyState, Params: []string{"sshKey"}},
	"getTLSState":            {Func: rpcGetTLSState},
	"setTLSState":            {Func: rpcSetTLSState, Params: []string{"state"}},
	"setMassStorageMode":     {Func: rpcSetMassStorageMode, Params: []string{"mode"}},
	"getMassStorageMode":     {Func: rpcGetMassStorageMode},
	"isUpdatePending":        {Func: rpcIsUpdatePending},
	"getUsbEmulationState":   {Func: rpcGetUsbEmulationState},
	"setUsbEmulationState":   {Func: rpcSetUsbEmulationState, Params: []string{"enabled"}},
	"getUsbConfig":           {Func: rpcGetUsbConfig},
	"setUsbConfig":           {Func: rpcSetUsbConfig, Params: []string{"usbConfig"}},
	"checkMountUrl":          {Func: rpcCheckMountUrl, Params: []string{"url"}},
	"getVirtualMediaState":   {Func: rpcGetVirtualMediaState},
	"getStorageSpace":        {Func: rpcGetStorageSpace},
	"mountWithHTTP":          {Func: rpcMountWithHTTP, Params: []string{"url", "mode"}},
	"mountWithStorage":       {Func: rpcMountWithStorage, Params: []string{"filename", "mode"}},
	"listStorageFiles":       {Func: rpcListStorageFiles},
	"deleteStorageFile":      {Func: rpcDeleteStorageFile, Params: []string{"filename"}},
	"startStorageFileUpload": {Func: rpcStartStorageFileUpload, Params: []string{"filename", "size"}},
	"getWakeOnLanDevices":    {Func: rpcGetWakeOnLanDevices},
	"setWakeOnLanDevices":    {Func: rpcSetWakeOnLanDevices, Params: []string{"params"}},
	"resetConfig":            {Func: rpcResetConfig},
	"setDisplayRotation":     {Func: rpcSetDisplayRotation, Params: []string{"params"}},
	"getDisplayRotation":     {Func: rpcGetDisplayRotation},
	"setBacklightSettings":   {Func: rpcSetBacklightSettings, Params: []string{"params"}},
	"getBacklightSettings":   {Func: rpcGetBacklightSettings},
	"getDCPowerState":        {Func: rpcGetDCPowerState},
	"setDCPowerState":        {Func: rpcSetDCPowerState, Params: []string{"enabled"}},
	"setDCRestoreState":      {Func: rpcSetDCRestoreState, Params: []string{"state"}},
	"getActiveExtension":     {Func: rpcGetActiveExtension},
	"setActiveExtension":     {Func: rpcSetActiveExtension, Params: []string{"extensionId"}},
	"getATXState":            {Func: rpcGetATXState},
	"setATXPowerAction":      {Func: rpcSetATXPowerAction, Params: []string{"action"}},
	"getSerialSettings":      {Func: rpcGetSerialSettings},
	"setSerialSettings":      {Func: rpcSetSerialSettings, Params: []string{"settings"}},
	"getUsbDevices":          {Func: rpcGetUsbDevices},
	"setUsbDevices":          {Func: rpcSetUsbDevices, Params: []string{"devices"}},
	"setUsbDeviceState":      {Func: rpcSetUsbDeviceState, Params: []string{"device", "enabled"}},
	"setCloudUrl":            {Func: rpcSetCloudUrl, Params: []string{"apiUrl", "appUrl"}},
	"getKeyboardLayout":      {Func: rpcGetKeyboardLayout},
	"setKeyboardLayout":      {Func: rpcSetKeyboardLayout, Params: []string{"layout"}},
	"getKeyboardMacros":      {Func: getKeyboardMacros},
	"setKeyboardMacros":      {Func: setKeyboardMacros, Params: []string{"params"}},
	"getLocalLoopbackOnly":   {Func: rpcGetLocalLoopbackOnly},
	"setLocalLoopbackOnly":   {Func: rpcSetLocalLoopbackOnly, Params: []string{"enabled"}},
}
