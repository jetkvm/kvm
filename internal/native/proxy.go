package native

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/jetkvm/kvm/internal/utils"
	"github.com/rs/zerolog"
)

const (
	maxFrameSize = 1920 * 1080 / 2
)

type nativeProxyOptions struct {
	Disable               bool            `env:"JETKVM_NATIVE_DISABLE"`
	SystemVersion         *semver.Version `env:"JETKVM_NATIVE_SYSTEM_VERSION"`
	AppVersion            *semver.Version `env:"JETKVM_NATIVE_APP_VERSION"`
	DisplayRotation       uint16          `env:"JETKVM_NATIVE_DISPLAY_ROTATION"`
	DefaultQualityFactor  float64         `env:"JETKVM_NATIVE_DEFAULT_QUALITY_FACTOR"`
	CtrlUnixSocket        string          `env:"JETKVM_NATIVE_CTRL_UNIX_SOCKET"`
	VideoStreamUnixSocket string          `env:"JETKVM_NATIVE_VIDEO_STREAM_UNIX_SOCKET"`
	BinaryPath            string          `env:"JETKVM_NATIVE_BINARY_PATH"`
	LoggerLevel           zerolog.Level   `env:"JETKVM_NATIVE_LOGGER_LEVEL"`
	HandshakeMessage      string          `env:"JETKVM_NATIVE_HANDSHAKE_MESSAGE"`

	OnVideoFrameReceived func(frame []byte, duration time.Duration)
	OnIndevEvent         func(event string)
	OnRpcEvent           func(event string)
	OnVideoStateChange   func(state VideoState)
}

func (n *NativeOptions) toProxyOptions() *nativeProxyOptions {
	return &nativeProxyOptions{
		Disable:              n.Disable,
		SystemVersion:        n.SystemVersion,
		AppVersion:           n.AppVersion,
		DisplayRotation:      n.DisplayRotation,
		DefaultQualityFactor: n.DefaultQualityFactor,
		OnVideoFrameReceived: n.OnVideoFrameReceived,
		OnIndevEvent:         n.OnIndevEvent,
		OnRpcEvent:           n.OnRpcEvent,
		OnVideoStateChange:   n.OnVideoStateChange,
	}
}

func (p *nativeProxyOptions) toNativeOptions() *NativeOptions {
	return &NativeOptions{
		Disable:              p.Disable,
		SystemVersion:        p.SystemVersion,
		AppVersion:           p.AppVersion,
		DisplayRotation:      p.DisplayRotation,
		DefaultQualityFactor: p.DefaultQualityFactor,
	}
}

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
	nativeUnixSocket      string
	videoStreamUnixSocket string
	videoStreamListener   net.Listener
	binaryPath            string

	client      *GRPCClient
	cmd         *cmdWrapper
	logger      *zerolog.Logger
	ready       chan struct{}
	options     *nativeProxyOptions
	restartM    sync.Mutex
	stopped     bool
	processWait chan error
}

// NewNativeProxy creates a new NativeProxy that spawns a separate process
func NewNativeProxy(opts NativeOptions) (*NativeProxy, error) {
	proxyOptions := opts.toProxyOptions()
	proxyOptions.CtrlUnixSocket = "jetkvm-native-grpc"
	proxyOptions.VideoStreamUnixSocket = "@jetkvm-native-video-stream"

	// Get the current executable path to spawn itself
	exePath, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("failed to get executable path: %w", err)
	}

	proxy := &NativeProxy{
		nativeUnixSocket:      proxyOptions.CtrlUnixSocket,
		videoStreamUnixSocket: proxyOptions.VideoStreamUnixSocket,
		binaryPath:            exePath,
		logger:                nativeLogger,
		ready:                 make(chan struct{}),
		options:               proxyOptions,
		processWait:           make(chan error, 1),
	}
	proxy.cmd, err = proxy.spawnProcess()
	nativeLogger.Info().Msg("spawned process")
	if err != nil {
		return nil, fmt.Errorf("failed to spawn process: %w", err)
	}

	// create unix packet
	listener, err := net.Listen("unixpacket", proxyOptions.VideoStreamUnixSocket)
	if err != nil {
		nativeLogger.Warn().Err(err).Msg("failed to start server")
		return nil, fmt.Errorf("failed to start server: %w", err)
	}
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				nativeLogger.Warn().Err(err).Msg("failed to accept socket")
				continue
			}
			nativeLogger.Info().Str("socket", conn.RemoteAddr().String()).Msg("accepted socket")
			go proxy.handleVideoFrame(conn)
		}
	}()

	return proxy, nil
}

func (p *NativeProxy) spawnProcess() (*cmdWrapper, error) {
	envArgs, err := utils.MarshalEnv(p.options)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal environment variables: %w", err)
	}

	cmd := exec.Command(
		p.binaryPath,
		"-subcomponent=native",
	)
	// cmd.Stdout = os.Stdout // Forward stdout to parent
	cmd.Stderr = os.Stderr // Forward stderr to parent
	// Set environment variable to indicate native process mode
	cmd.Env = append(
		os.Environ(),
		envArgs...,
	)
	// Wrap cmd to implement processCmd interface
	wrappedCmd := &cmdWrapper{Cmd: cmd}

	return wrappedCmd, nil
}

func (p *NativeProxy) handleVideoFrame(conn net.Conn) {
	defer conn.Close()

	inboundPacket := make([]byte, maxFrameSize)
	lastFrame := time.Now()

	for {
		n, err := conn.Read(inboundPacket)
		if err != nil {
			nativeLogger.Warn().Err(err).Msg("failed to accept socket")
			break
		}
		now := time.Now()
		sinceLastFrame := now.Sub(lastFrame)
		lastFrame = now
		p.options.OnVideoFrameReceived(inboundPacket[:n], sinceLastFrame)
	}
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

	nativeLogger.Info().Msg("process ready")

	client, err := NewGRPCClient(p.nativeUnixSocket, nativeLogger)
	nativeLogger.Info().Str("socket_path", p.nativeUnixSocket).Msg("created client")
	if err != nil {
		return fmt.Errorf("failed to create IPC client: %w", err)
	}
	p.client = client

	// Wait for ready signal from the native process
	if err := p.client.WaitReady(); err != nil {
		// Clean up if ready failed
		if p.cmd.Process != nil {
			_ = p.cmd.Process.Kill()
			_ = p.cmd.Wait()
		}
		return fmt.Errorf("failed to wait for ready: %w", err)
	}

	// Set up event handlers
	p.setupEventHandlers(client)

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

	wrappedCmd, err := p.spawnProcess()
	if err != nil {
		return fmt.Errorf("failed to spawn process: %w", err)
	}

	// Close old client
	if p.client != nil {
		_ = p.client.Close()
	}

	// Create new client
	client, err := NewGRPCClient(p.nativeUnixSocket, p.logger)
	if err != nil {
		return fmt.Errorf("failed to create IPC client: %w", err)
	}

	// Set up event handlers again
	p.setupEventHandlers(client)

	// Start the process
	if err := wrappedCmd.Start(); err != nil {
		return fmt.Errorf("failed to start native process: %w", err)
	}

	// Wait for ready
	if err := client.WaitReady(); err != nil {
		if wrappedCmd.Process != nil {
			_ = wrappedCmd.Process.Kill()
			_ = wrappedCmd.Wait()
		}
		return fmt.Errorf("timeout waiting for ready: %w", err)
	}

	p.cmd = wrappedCmd
	p.client = client

	p.logger.Info().Msg("native process restarted successfully")
	return nil
}

func (p *NativeProxy) setupEventHandlers(client *GRPCClient) {
	// if p.opts.OnVideoStateChange != nil {
	// 	client.OnEvent("video_state_change", func(data interface{}) {
	// 		dataBytes, err := json.Marshal(data)
	// 		if err != nil {
	// 			p.logger.Warn().Err(err).Msg("failed to marshal video state event")
	// 			return
	// 		}
	// 		var state VideoState
	// 		if err := json.Unmarshal(dataBytes, &state); err != nil {
	// 			p.logger.Warn().Err(err).Msg("failed to unmarshal video state event")
	// 			return
	// 		}
	// 		p.opts.OnVideoStateChange(state)
	// 	})
	// }

	// if p.opts.OnIndevEvent != nil {
	// 	client.OnEvent("indev_event", func(data interface{}) {
	// 		if event, ok := data.(string); ok {
	// 			p.opts.OnIndevEvent(event)
	// 		}
	// 	})
	// }

	// if p.opts.OnRpcEvent != nil {
	// 	client.OnEvent("rpc_event", func(data interface{}) {
	// 		if event, ok := data.(string); ok {
	// 			p.opts.OnRpcEvent(event)
	// 		}
	// 	})
	// }

	// if p.opts.OnVideoFrameReceived != nil {
	// 	client.OnEvent("video_frame", func(data interface{}) {
	// 		dataMap, ok := data.(map[string]interface{})
	// 		if !ok {
	// 			p.logger.Warn().Msg("invalid video frame event data")
	// 			return
	// 		}

	// 		frameData, ok := dataMap["frame"].([]interface{})
	// 		if !ok {
	// 			p.logger.Warn().Msg("invalid frame data in event")
	// 			return
	// 		}

	// 		frame := make([]byte, len(frameData))
	// 		for i, v := range frameData {
	// 			if b, ok := v.(float64); ok {
	// 				frame[i] = byte(b)
	// 			}
	// 		}

	// 		durationNs, ok := dataMap["duration"].(float64)
	// 		if !ok {
	// 			p.logger.Warn().Msg("invalid duration in event")
	// 			return
	// 		}

	// 		p.opts.OnVideoFrameReceived(frame, time.Duration(durationNs))
	// 	})
	// }
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

// Implement all Native methods by forwarding to gRPC client
func (p *NativeProxy) VideoSetSleepMode(enabled bool) error {
	return p.client.VideoSetSleepMode(enabled)
}

func (p *NativeProxy) VideoGetSleepMode() (bool, error) {
	return p.client.VideoGetSleepMode()
}

func (p *NativeProxy) VideoSleepModeSupported() bool {
	return p.client.VideoSleepModeSupported()
}

func (p *NativeProxy) VideoSetQualityFactor(factor float64) error {
	return p.client.VideoSetQualityFactor(factor)
}

func (p *NativeProxy) VideoGetQualityFactor() (float64, error) {
	return p.client.VideoGetQualityFactor()
}

func (p *NativeProxy) VideoSetEDID(edid string) error {
	return p.client.VideoSetEDID(edid)
}

func (p *NativeProxy) VideoGetEDID() (string, error) {
	return p.client.VideoGetEDID()
}

func (p *NativeProxy) VideoLogStatus() (string, error) {
	return p.client.VideoLogStatus()
}

func (p *NativeProxy) VideoStop() error {
	return p.client.VideoStop()
}

func (p *NativeProxy) VideoStart() error {
	return p.client.VideoStart()
}

func (p *NativeProxy) GetLVGLVersion() (string, error) {
	return p.client.GetLVGLVersion()
}

func (p *NativeProxy) UIObjHide(objName string) (bool, error) {
	return p.client.UIObjHide(objName)
}

func (p *NativeProxy) UIObjShow(objName string) (bool, error) {
	return p.client.UIObjShow(objName)
}

func (p *NativeProxy) UISetVar(name string, value string) {
	p.client.UISetVar(name, value)
}

func (p *NativeProxy) UIGetVar(name string) string {
	return p.client.UIGetVar(name)
}

func (p *NativeProxy) UIObjAddState(objName string, state string) (bool, error) {
	return p.client.UIObjAddState(objName, state)
}

func (p *NativeProxy) UIObjClearState(objName string, state string) (bool, error) {
	return p.client.UIObjClearState(objName, state)
}

func (p *NativeProxy) UIObjAddFlag(objName string, flag string) (bool, error) {
	return p.client.UIObjAddFlag(objName, flag)
}

func (p *NativeProxy) UIObjClearFlag(objName string, flag string) (bool, error) {
	return p.client.UIObjClearFlag(objName, flag)
}

func (p *NativeProxy) UIObjSetOpacity(objName string, opacity int) (bool, error) {
	return p.client.UIObjSetOpacity(objName, opacity)
}

func (p *NativeProxy) UIObjFadeIn(objName string, duration uint32) (bool, error) {
	return p.client.UIObjFadeIn(objName, duration)
}

func (p *NativeProxy) UIObjFadeOut(objName string, duration uint32) (bool, error) {
	return p.client.UIObjFadeOut(objName, duration)
}

func (p *NativeProxy) UIObjSetLabelText(objName string, text string) (bool, error) {
	return p.client.UIObjSetLabelText(objName, text)
}

func (p *NativeProxy) UIObjSetImageSrc(objName string, image string) (bool, error) {
	return p.client.UIObjSetImageSrc(objName, image)
}

func (p *NativeProxy) DisplaySetRotation(rotation uint16) (bool, error) {
	return p.client.DisplaySetRotation(rotation)
}

func (p *NativeProxy) UpdateLabelIfChanged(objName string, newText string) {
	p.client.UpdateLabelIfChanged(objName, newText)
}

func (p *NativeProxy) UpdateLabelAndChangeVisibility(objName string, newText string) {
	p.client.UpdateLabelAndChangeVisibility(objName, newText)
}

func (p *NativeProxy) SwitchToScreenIf(screenName string, shouldSwitch []string) {
	p.client.SwitchToScreenIf(screenName, shouldSwitch)
}

func (p *NativeProxy) SwitchToScreenIfDifferent(screenName string) {
	p.client.SwitchToScreenIfDifferent(screenName)
}

func (p *NativeProxy) DoNotUseThisIsForCrashTestingOnly() {
	p.client.DoNotUseThisIsForCrashTestingOnly()
}
