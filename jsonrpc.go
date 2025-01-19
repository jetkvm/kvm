package kvm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"kvm/internal/jsonrpc"
	"kvm/internal/plugin"
	"log"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/pion/webrtc/v4"
)

type DataChannelWriter struct {
	dataChannel *webrtc.DataChannel
}

func NewDataChannelWriter(dataChannel *webrtc.DataChannel) *DataChannelWriter {
	return &DataChannelWriter{
		dataChannel: dataChannel,
	}
}

func (w *DataChannelWriter) Write(data []byte) (int, error) {
	err := w.dataChannel.SendText(string(data))
	if err != nil {
		log.Println("Error sending JSONRPC response:", err)
		return 0, err
	}
	return len(data), nil
}

func NewDataChannelJsonRpcRouter(dataChannel *webrtc.DataChannel) *jsonrpc.JSONRPCRouter {
	return jsonrpc.NewJSONRPCRouter(
		NewDataChannelWriter(dataChannel),
		rpcHandlers,
	)
}

// TODO: embed this into the session's rpc server
func writeJSONRPCEvent(event string, params interface{}, session *Session) {
	request := jsonrpc.JSONRPCEvent{
		JSONRPC: "2.0",
		Method:  event,
		Params:  params,
	}
	requestBytes, err := json.Marshal(request)
	if err != nil {
		log.Println("Error marshalling JSONRPC event:", err)
		return
	}
	if session == nil || session.RPCChannel == nil {
		log.Println("RPC channel not available")
		return
	}
	err = session.RPCChannel.SendText(string(requestBytes))
	if err != nil {
		log.Println("Error sending JSONRPC event:", err)
		return
	}
}

func rpcPing() (string, error) {
	return "pong", nil
}

func rpcGetDeviceID() (string, error) {
	return GetDeviceID(), nil
}

var streamFactor = 1.0

func rpcGetStreamQualityFactor() (float64, error) {
	return streamFactor, nil
}

func rpcSetStreamQualityFactor(factor float64) error {
	log.Printf("Setting stream quality factor to: %f", factor)
	var _, err = CallCtrlAction("set_video_quality_factor", map[string]interface{}{"quality_factor": factor})
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
		log.Println("Restoring EDID to default")
		edid = "00ffffffffffff0052620188008888881c150103800000780a0dc9a05747982712484c00000001010101010101010101010101010101023a801871382d40582c4500c48e2100001e011d007251d01e206e285500c48e2100001e000000fc00543734392d6648443732300a20000000fd00147801ff1d000a202020202020017b"
	} else {
		log.Printf("Setting EDID to: %s", edid)
	}
	_, err := CallCtrlAction("set_edid", map[string]interface{}{"edid": edid})
	if err != nil {
		return err
	}
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
	if err != nil {
		return nil, fmt.Errorf("error checking for updates: %w", err)
	}

	return updateStatus, nil
}

func rpcTryUpdate() error {
	includePreRelease := config.IncludePreRelease
	go func() {
		err := TryUpdate(context.Background(), GetDeviceID(), includePreRelease)
		if err != nil {
			logger.Warnf("failed to try update: %v", err)
		}
	}()
	return nil
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
			logger.Debug("dev mode already enabled")
			return nil
		}
	} else {
		if _, err := os.Stat(devModeFile); err == nil {
			if err := os.Remove(devModeFile); err != nil {
				return fmt.Errorf("failed to remove devmode file: %w", err)
			}
		} else if os.IsNotExist(err) {
			logger.Debug("dev mode already disabled")
			return nil
		} else {
			return fmt.Errorf("error checking dev mode file: %w", err)
		}
	}

	cmd := exec.Command("dropbear.sh")
	output, err := cmd.CombinedOutput()
	if err != nil {
		logger.Warnf("Failed to start/stop SSH: %v, %v", err, output)
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
	if sshKey != "" {
		// Create directory if it doesn't exist
		if err := os.MkdirAll(sshKeyDir, 0700); err != nil {
			return fmt.Errorf("failed to create SSH key directory: %w", err)
		}

		// Write SSH key to file
		if err := os.WriteFile(sshKeyFile, []byte(sshKey), 0600); err != nil {
			return fmt.Errorf("failed to write SSH key: %w", err)
		}
	} else {
		// Remove SSH key file if empty string is provided
		if err := os.Remove(sshKeyFile); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove SSH key file: %w", err)
		}
	}

	return nil
}

func rpcSetMassStorageMode(mode string) (string, error) {
	log.Printf("[jsonrpc.go:rpcSetMassStorageMode] Setting mass storage mode to: %s", mode)
	var cdrom bool
	if mode == "cdrom" {
		cdrom = true
	} else if mode != "file" {
		log.Printf("[jsonrpc.go:rpcSetMassStorageMode] Invalid mode provided: %s", mode)
		return "", fmt.Errorf("invalid mode: %s", mode)
	}

	log.Printf("[jsonrpc.go:rpcSetMassStorageMode] Setting mass storage mode to: %s", mode)

	err := setMassStorageMode(cdrom)
	if err != nil {
		return "", fmt.Errorf("failed to set mass storage mode: %w", err)
	}

	log.Printf("[jsonrpc.go:rpcSetMassStorageMode] Mass storage mode set to %s", mode)

	// Get the updated mode after setting
	return rpcGetMassStorageMode()
}

func rpcGetMassStorageMode() (string, error) {
	cdrom, err := getMassStorageMode()
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

var udcFilePath = filepath.Join("/sys/bus/platform/drivers/dwc3", udc)

func rpcGetUsbEmulationState() (bool, error) {
	_, err := os.Stat(udcFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("error checking USB emulation state: %w", err)
	}
	return true, nil
}

func rpcSetUsbEmulationState(enabled bool) error {
	if enabled {
		return os.WriteFile("/sys/bus/platform/drivers/dwc3/bind", []byte(udc), 0644)
	} else {
		return os.WriteFile("/sys/bus/platform/drivers/dwc3/unbind", []byte(udc), 0644)
	}
}

func rpcGetWakeOnLanDevices() ([]WakeOnLanDevice, error) {
	LoadConfig()
	if config.WakeOnLanDevices == nil {
		return []WakeOnLanDevice{}, nil
	}
	return config.WakeOnLanDevices, nil
}

type SetWakeOnLanDevicesParams struct {
	Devices []WakeOnLanDevice `json:"devices"`
}

func rpcSetWakeOnLanDevices(params SetWakeOnLanDevicesParams) error {
	LoadConfig()
	config.WakeOnLanDevices = params.Devices
	return SaveConfig()
}

func rpcResetConfig() error {
	LoadConfig()
	config = defaultConfig
	if err := SaveConfig(); err != nil {
		return fmt.Errorf("failed to reset config: %w", err)
	}

	log.Println("Configuration reset to default")
	return nil
}

// TODO: replace this crap with code generator
var rpcHandlers = map[string]*jsonrpc.RPCHandler{
	"ping":                   {Func: rpcPing},
	"getDeviceID":            {Func: rpcGetDeviceID},
	"deregisterDevice":       {Func: rpcDeregisterDevice},
	"getCloudState":          {Func: rpcGetCloudState},
	"keyboardReport":         {Func: rpcKeyboardReport, Params: []string{"modifier", "keys"}},
	"absMouseReport":         {Func: rpcAbsMouseReport, Params: []string{"x", "y", "buttons"}},
	"wheelReport":            {Func: rpcWheelReport, Params: []string{"wheelY"}},
	"getVideoState":          {Func: rpcGetVideoState},
	"getUSBState":            {Func: rpcGetUSBState},
	"unmountImage":           {Func: rpcUnmountImage},
	"rpcMountBuiltInImage":   {Func: rpcMountBuiltInImage, Params: []string{"filename"}},
	"setJigglerState":        {Func: rpcSetJigglerState, Params: []string{"enabled"}},
	"getJigglerState":        {Func: rpcGetJigglerState},
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
	"setMassStorageMode":     {Func: rpcSetMassStorageMode, Params: []string{"mode"}},
	"getMassStorageMode":     {Func: rpcGetMassStorageMode},
	"isUpdatePending":        {Func: rpcIsUpdatePending},
	"getUsbEmulationState":   {Func: rpcGetUsbEmulationState},
	"setUsbEmulationState":   {Func: rpcSetUsbEmulationState, Params: []string{"enabled"}},
	"checkMountUrl":          {Func: rpcCheckMountUrl, Params: []string{"url"}},
	"getVirtualMediaState":   {Func: rpcGetVirtualMediaState},
	"getStorageSpace":        {Func: rpcGetStorageSpace},
	"mountWithHTTP":          {Func: rpcMountWithHTTP, Params: []string{"url", "mode"}},
	"mountWithWebRTC":        {Func: rpcMountWithWebRTC, Params: []string{"filename", "size", "mode"}},
	"mountWithStorage":       {Func: rpcMountWithStorage, Params: []string{"filename", "mode"}},
	"listStorageFiles":       {Func: rpcListStorageFiles},
	"deleteStorageFile":      {Func: rpcDeleteStorageFile, Params: []string{"filename"}},
	"startStorageFileUpload": {Func: rpcStartStorageFileUpload, Params: []string{"filename", "size"}},
	"getWakeOnLanDevices":    {Func: rpcGetWakeOnLanDevices},
	"setWakeOnLanDevices":    {Func: rpcSetWakeOnLanDevices, Params: []string{"params"}},
	"resetConfig":            {Func: rpcResetConfig},
	"pluginStartUpload":      {Func: plugin.RpcPluginStartUpload, Params: []string{"filename", "size"}},
	"pluginExtract":          {Func: plugin.RpcPluginExtract, Params: []string{"filename"}},
	"pluginInstall":          {Func: plugin.RpcPluginInstall, Params: []string{"name", "version"}},
	"pluginList":             {Func: plugin.RpcPluginList},
	"pluginUpdateConfig":     {Func: plugin.RpcPluginUpdateConfig, Params: []string{"name", "enabled"}},
	"pluginUninstall":        {Func: plugin.RpcPluginUninstall, Params: []string{"name"}},
}
