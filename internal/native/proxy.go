package native

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/jetkvm/kvm/internal/supervisor"
	"github.com/rs/zerolog"
)

// cmdWrapper wraps exec.Cmd to implement processCmd interface
type cmdWrapper struct {
	*exec.Cmd
}

func (c *cmdWrapper) GetProcess() interface {
	Kill() error
	Signal(sig interface{}) error
} {
	return &processWrapper{Process: c.Cmd.Process}
}

type processWrapper struct {
	*os.Process
}

func (p *processWrapper) Signal(sig interface{}) error {
	if sig == nil {
		// Check if process is alive by sending signal 0
		return p.Process.Signal(os.Signal(syscall.Signal(0)))
	}
	if s, ok := sig.(os.Signal); ok {
		return p.Process.Signal(s)
	}
	return fmt.Errorf("invalid signal type")
}

// NativeProxy is a proxy that communicates with a separate native process
type NativeProxy struct {
	client      *IPCClient
	cmd         *exec.Cmd
	wrapped     *cmdWrapper
	logger      *zerolog.Logger
	ready       chan struct{}
	restartM    sync.Mutex
	stopped     bool
	opts        NativeProxyOptions
	binaryPath  string
	configJSON  []byte
	processWait chan error
}

// NativeProxyOptions are options for creating a NativeProxy
type NativeProxyOptions struct {
	Disable              bool
	SystemVersion        *semver.Version
	AppVersion           *semver.Version
	DisplayRotation      uint16
	DefaultQualityFactor float64
	OnVideoStateChange   func(state VideoState)
	OnVideoFrameReceived func(frame []byte, duration time.Duration)
	OnIndevEvent         func(event string)
	OnRpcEvent           func(event string)
	Logger               *zerolog.Logger
}

// NewNativeProxy creates a new NativeProxy that spawns a separate process
func NewNativeProxy(opts NativeProxyOptions) (*NativeProxy, error) {
	if opts.Logger == nil {
		opts.Logger = nativeLogger
	}

	// Get the current executable path to spawn itself
	exePath, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("failed to get executable path: %w", err)
	}
	binaryPath := exePath

	config := ProcessConfig{
		Disable:              opts.Disable,
		SystemVersion:        "",
		AppVersion:           "",
		DisplayRotation:      opts.DisplayRotation,
		DefaultQualityFactor: opts.DefaultQualityFactor,
	}
	if opts.SystemVersion != nil {
		config.SystemVersion = opts.SystemVersion.String()
	}
	if opts.AppVersion != nil {
		config.AppVersion = opts.AppVersion.String()
	}

	configJSON, err := json.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal config: %w", err)
	}

	cmd := exec.Command(binaryPath, string(configJSON))
	cmd.Stderr = os.Stderr // Forward stderr to parent
	// Set environment variable to indicate native process mode
	cmd.Env = append(os.Environ(), fmt.Sprintf("%s=native", supervisor.EnvSubcomponent))

	// Wrap cmd to implement processCmd interface
	wrappedCmd := &cmdWrapper{Cmd: cmd}

	client, err := NewIPCClient(wrappedCmd, opts.Logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create IPC client: %w", err)
	}

	proxy := &NativeProxy{
		client:      client,
		cmd:         cmd,
		wrapped:     wrappedCmd,
		logger:      opts.Logger,
		ready:       make(chan struct{}),
		opts:        opts,
		binaryPath:  binaryPath,
		configJSON:  configJSON,
		processWait: make(chan error, 1),
	}

	// Set up event handlers
	proxy.setupEventHandlers(client)

	return proxy, nil
}

// Start starts the native process
func (p *NativeProxy) Start() error {
	p.restartM.Lock()
	defer p.restartM.Unlock()

	if p.stopped {
		return fmt.Errorf("proxy is stopped")
	}

	if err := p.cmd.Start(); err != nil {
		return fmt.Errorf("failed to start native process: %w", err)
	}

	// Wait for ready signal from the native process
	if err := p.client.WaitReady(); err != nil {
		// Clean up if ready failed
		if p.cmd.Process != nil {
			_ = p.cmd.Process.Kill()
			_ = p.cmd.Wait()
		}
		return err
	}

	// Start monitoring process for crashes
	go p.monitorProcess()

	close(p.ready)
	return nil
}

// monitorProcess monitors the native process and restarts it if it crashes
func (p *NativeProxy) monitorProcess() {
	for {
		p.restartM.Lock()
		cmd := p.cmd
		stopped := p.stopped
		p.restartM.Unlock()

		if stopped {
			return
		}

		if cmd == nil {
			return
		}

		err := cmd.Wait()
		select {
		case p.processWait <- err:
		default:
		}

		p.restartM.Lock()
		if p.stopped {
			p.restartM.Unlock()
			return
		}
		p.restartM.Unlock()

		p.logger.Warn().Err(err).Msg("native process exited, restarting...")

		// Wait a bit before restarting
		time.Sleep(1 * time.Second)

		// Restart the process
		if err := p.restartProcess(); err != nil {
			p.logger.Error().Err(err).Msg("failed to restart native process")
			// Wait longer before retrying
			time.Sleep(5 * time.Second)
			continue
		}
	}
}

// restartProcess restarts the native process
func (p *NativeProxy) restartProcess() error {
	p.restartM.Lock()
	defer p.restartM.Unlock()

	if p.stopped {
		return fmt.Errorf("proxy is stopped")
	}

	// Create new command
	cmd := exec.Command(p.binaryPath, string(p.configJSON))
	cmd.Stderr = os.Stderr
	// Set environment variable to indicate native process mode
	cmd.Env = append(os.Environ(), "JETKVM_NATIVE_PROCESS=1")

	// Wrap cmd to implement processCmd interface
	wrappedCmd := &cmdWrapper{Cmd: cmd}

	// Close old client
	if p.client != nil {
		_ = p.client.Close()
	}

	// Create new client
	client, err := NewIPCClient(wrappedCmd, p.logger)
	if err != nil {
		return fmt.Errorf("failed to create IPC client: %w", err)
	}

	// Set up event handlers again
	p.setupEventHandlers(client)

	// Start the process
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start native process: %w", err)
	}

	// Wait for ready
	if err := client.WaitReady(); err != nil {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
		return fmt.Errorf("timeout waiting for ready: %w", err)
	}

	p.cmd = cmd
	p.wrapped = wrappedCmd
	p.client = client

	p.logger.Info().Msg("native process restarted successfully")
	return nil
}

func (p *NativeProxy) setupEventHandlers(client *IPCClient) {
	if p.opts.OnVideoStateChange != nil {
		client.OnEvent("video_state_change", func(data interface{}) {
			dataBytes, err := json.Marshal(data)
			if err != nil {
				p.logger.Warn().Err(err).Msg("failed to marshal video state event")
				return
			}
			var state VideoState
			if err := json.Unmarshal(dataBytes, &state); err != nil {
				p.logger.Warn().Err(err).Msg("failed to unmarshal video state event")
				return
			}
			p.opts.OnVideoStateChange(state)
		})
	}

	if p.opts.OnIndevEvent != nil {
		client.OnEvent("indev_event", func(data interface{}) {
			if event, ok := data.(string); ok {
				p.opts.OnIndevEvent(event)
			}
		})
	}

	if p.opts.OnRpcEvent != nil {
		client.OnEvent("rpc_event", func(data interface{}) {
			if event, ok := data.(string); ok {
				p.opts.OnRpcEvent(event)
			}
		})
	}

	if p.opts.OnVideoFrameReceived != nil {
		client.OnEvent("video_frame", func(data interface{}) {
			dataMap, ok := data.(map[string]interface{})
			if !ok {
				p.logger.Warn().Msg("invalid video frame event data")
				return
			}

			frameData, ok := dataMap["frame"].([]interface{})
			if !ok {
				p.logger.Warn().Msg("invalid frame data in event")
				return
			}

			frame := make([]byte, len(frameData))
			for i, v := range frameData {
				if b, ok := v.(float64); ok {
					frame[i] = byte(b)
				}
			}

			durationNs, ok := dataMap["duration"].(float64)
			if !ok {
				p.logger.Warn().Msg("invalid duration in event")
				return
			}

			p.opts.OnVideoFrameReceived(frame, time.Duration(durationNs))
		})
	}
}

// Stop stops the native process
func (p *NativeProxy) Stop() error {
	p.restartM.Lock()
	defer p.restartM.Unlock()

	p.stopped = true

	if err := p.client.Close(); err != nil {
		p.logger.Warn().Err(err).Msg("failed to close IPC client")
	}

	if p.cmd.Process != nil {
		if err := p.cmd.Process.Kill(); err != nil {
			return fmt.Errorf("failed to kill native process: %w", err)
		}
		_ = p.cmd.Wait()
	}

	return nil
}

// Implement all Native methods by forwarding to IPC

func (p *NativeProxy) VideoSetSleepMode(enabled bool) error {
	_, err := p.client.Call("VideoSetSleepMode", map[string]interface{}{
		"enabled": enabled,
	})
	return err
}

func (p *NativeProxy) VideoGetSleepMode() (bool, error) {
	resp, err := p.client.Call("VideoGetSleepMode", nil)
	if err != nil {
		return false, err
	}
	result, ok := resp.Result.(bool)
	if !ok {
		return false, fmt.Errorf("invalid response type")
	}
	return result, nil
}

func (p *NativeProxy) VideoSleepModeSupported() bool {
	resp, err := p.client.Call("VideoSleepModeSupported", nil)
	if err != nil {
		return false
	}
	result, ok := resp.Result.(bool)
	if !ok {
		return false
	}
	return result
}

func (p *NativeProxy) VideoSetQualityFactor(factor float64) error {
	_, err := p.client.Call("VideoSetQualityFactor", map[string]interface{}{
		"factor": factor,
	})
	return err
}

func (p *NativeProxy) VideoGetQualityFactor() (float64, error) {
	resp, err := p.client.Call("VideoGetQualityFactor", nil)
	if err != nil {
		return 0, err
	}
	result, ok := resp.Result.(float64)
	if !ok {
		return 0, fmt.Errorf("invalid response type")
	}
	return result, nil
}

func (p *NativeProxy) VideoSetEDID(edid string) error {
	_, err := p.client.Call("VideoSetEDID", map[string]interface{}{
		"edid": edid,
	})
	return err
}

func (p *NativeProxy) VideoGetEDID() (string, error) {
	resp, err := p.client.Call("VideoGetEDID", nil)
	if err != nil {
		return "", err
	}
	result, ok := resp.Result.(string)
	if !ok {
		return "", fmt.Errorf("invalid response type")
	}
	return result, nil
}

func (p *NativeProxy) VideoLogStatus() (string, error) {
	resp, err := p.client.Call("VideoLogStatus", nil)
	if err != nil {
		return "", err
	}
	result, ok := resp.Result.(string)
	if !ok {
		return "", fmt.Errorf("invalid response type")
	}
	return result, nil
}

func (p *NativeProxy) VideoStop() error {
	_, err := p.client.Call("VideoStop", nil)
	return err
}

func (p *NativeProxy) VideoStart() error {
	_, err := p.client.Call("VideoStart", nil)
	return err
}

func (p *NativeProxy) GetLVGLVersion() (string, error) {
	resp, err := p.client.Call("GetLVGLVersion", nil)
	if err != nil {
		return "", err
	}
	result, ok := resp.Result.(string)
	if !ok {
		return "", fmt.Errorf("invalid response type")
	}
	return result, nil
}

func (p *NativeProxy) UIObjHide(objName string) (bool, error) {
	resp, err := p.client.Call("UIObjHide", map[string]interface{}{
		"obj_name": objName,
	})
	if err != nil {
		return false, err
	}
	result, ok := resp.Result.(bool)
	if !ok {
		return false, fmt.Errorf("invalid response type")
	}
	return result, nil
}

func (p *NativeProxy) UIObjShow(objName string) (bool, error) {
	resp, err := p.client.Call("UIObjShow", map[string]interface{}{
		"obj_name": objName,
	})
	if err != nil {
		return false, err
	}
	result, ok := resp.Result.(bool)
	if !ok {
		return false, fmt.Errorf("invalid response type")
	}
	return result, nil
}

func (p *NativeProxy) UISetVar(name string, value string) {
	_, _ = p.client.Call("UISetVar", map[string]interface{}{
		"name":  name,
		"value": value,
	})
}

func (p *NativeProxy) UIGetVar(name string) string {
	resp, err := p.client.Call("UIGetVar", map[string]interface{}{
		"name": name,
	})
	if err != nil {
		return ""
	}
	result, ok := resp.Result.(string)
	if !ok {
		return ""
	}
	return result
}

func (p *NativeProxy) UIObjAddState(objName string, state string) (bool, error) {
	resp, err := p.client.Call("UIObjAddState", map[string]interface{}{
		"obj_name": objName,
		"state":    state,
	})
	if err != nil {
		return false, err
	}
	result, ok := resp.Result.(bool)
	if !ok {
		return false, fmt.Errorf("invalid response type")
	}
	return result, nil
}

func (p *NativeProxy) UIObjClearState(objName string, state string) (bool, error) {
	resp, err := p.client.Call("UIObjClearState", map[string]interface{}{
		"obj_name": objName,
		"state":    state,
	})
	if err != nil {
		return false, err
	}
	result, ok := resp.Result.(bool)
	if !ok {
		return false, fmt.Errorf("invalid response type")
	}
	return result, nil
}

func (p *NativeProxy) UIObjAddFlag(objName string, flag string) (bool, error) {
	resp, err := p.client.Call("UIObjAddFlag", map[string]interface{}{
		"obj_name": objName,
		"flag":     flag,
	})
	if err != nil {
		return false, err
	}
	result, ok := resp.Result.(bool)
	if !ok {
		return false, fmt.Errorf("invalid response type")
	}
	return result, nil
}

func (p *NativeProxy) UIObjClearFlag(objName string, flag string) (bool, error) {
	resp, err := p.client.Call("UIObjClearFlag", map[string]interface{}{
		"obj_name": objName,
		"flag":     flag,
	})
	if err != nil {
		return false, err
	}
	result, ok := resp.Result.(bool)
	if !ok {
		return false, fmt.Errorf("invalid response type")
	}
	return result, nil
}

func (p *NativeProxy) UIObjSetOpacity(objName string, opacity int) (bool, error) {
	resp, err := p.client.Call("UIObjSetOpacity", map[string]interface{}{
		"obj_name": objName,
		"opacity":  opacity,
	})
	if err != nil {
		return false, err
	}
	result, ok := resp.Result.(bool)
	if !ok {
		return false, fmt.Errorf("invalid response type")
	}
	return result, nil
}

func (p *NativeProxy) UIObjFadeIn(objName string, duration uint32) (bool, error) {
	resp, err := p.client.Call("UIObjFadeIn", map[string]interface{}{
		"obj_name": objName,
		"duration": duration,
	})
	if err != nil {
		return false, err
	}
	result, ok := resp.Result.(bool)
	if !ok {
		return false, fmt.Errorf("invalid response type")
	}
	return result, nil
}

func (p *NativeProxy) UIObjFadeOut(objName string, duration uint32) (bool, error) {
	resp, err := p.client.Call("UIObjFadeOut", map[string]interface{}{
		"obj_name": objName,
		"duration": duration,
	})
	if err != nil {
		return false, err
	}
	result, ok := resp.Result.(bool)
	if !ok {
		return false, fmt.Errorf("invalid response type")
	}
	return result, nil
}

func (p *NativeProxy) UIObjSetLabelText(objName string, text string) (bool, error) {
	resp, err := p.client.Call("UIObjSetLabelText", map[string]interface{}{
		"obj_name": objName,
		"text":     text,
	})
	if err != nil {
		return false, err
	}
	result, ok := resp.Result.(bool)
	if !ok {
		return false, fmt.Errorf("invalid response type")
	}
	return result, nil
}

func (p *NativeProxy) UIObjSetImageSrc(objName string, image string) (bool, error) {
	resp, err := p.client.Call("UIObjSetImageSrc", map[string]interface{}{
		"obj_name": objName,
		"image":    image,
	})
	if err != nil {
		return false, err
	}
	result, ok := resp.Result.(bool)
	if !ok {
		return false, fmt.Errorf("invalid response type")
	}
	return result, nil
}

func (p *NativeProxy) DisplaySetRotation(rotation uint16) (bool, error) {
	resp, err := p.client.Call("DisplaySetRotation", map[string]interface{}{
		"rotation": rotation,
	})
	if err != nil {
		return false, err
	}
	result, ok := resp.Result.(bool)
	if !ok {
		return false, fmt.Errorf("invalid response type")
	}
	return result, nil
}

func (p *NativeProxy) UpdateLabelIfChanged(objName string, newText string) {
	_, _ = p.client.Call("UpdateLabelIfChanged", map[string]interface{}{
		"obj_name": objName,
		"new_text": newText,
	})
}

func (p *NativeProxy) UpdateLabelAndChangeVisibility(objName string, newText string) {
	_, _ = p.client.Call("UpdateLabelAndChangeVisibility", map[string]interface{}{
		"obj_name": objName,
		"new_text": newText,
	})
}

func (p *NativeProxy) SwitchToScreenIf(screenName string, shouldSwitch []string) {
	_, _ = p.client.Call("SwitchToScreenIf", map[string]interface{}{
		"screen_name":   screenName,
		"should_switch": shouldSwitch,
	})
}

func (p *NativeProxy) SwitchToScreenIfDifferent(screenName string) {
	_, _ = p.client.Call("SwitchToScreenIfDifferent", map[string]interface{}{
		"screen_name": screenName,
	})
}

func (p *NativeProxy) DoNotUseThisIsForCrashTestingOnly() {
	_, _ = p.client.Call("DoNotUseThisIsForCrashTestingOnly", nil)
}
