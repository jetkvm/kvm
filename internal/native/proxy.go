package native

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/jetkvm/kvm/internal/supervisor"
	"github.com/jetkvm/kvm/internal/utils"
	"github.com/rs/zerolog"
)

const (
	maxFrameSize                   = 1920 * 1080 / 2
	defaultMaxRestartAttempts uint = 5
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
	MaxRestartAttempts    uint

	OnVideoFrameReceived func(frame []byte, duration time.Duration)
	OnIndevEvent         func(event string)
	OnRpcEvent           func(event string)
	OnVideoStateChange   func(state VideoState)
	OnNativeRestart      func()
}

func randomId(binaryLength int) string {
	s := make([]byte, binaryLength)
	_, err := rand.Read(s)
	if err != nil {
		nativeLogger.Error().Err(err).Msg("failed to generate random ID")
		return strings.Repeat("0", binaryLength*2) // return all zeros if error
	}
	return hex.EncodeToString(s)
}

func (n *NativeOptions) toProxyOptions() *nativeProxyOptions {
	// random 16 bytes hex string
	handshakeMessage := randomId(16)
	maxRestartAttempts := defaultMaxRestartAttempts
	if n.MaxRestartAttempts > 0 {
		maxRestartAttempts = n.MaxRestartAttempts
	}
	return &nativeProxyOptions{
		SystemVersion:        n.SystemVersion,
		AppVersion:           n.AppVersion,
		DisplayRotation:      n.DisplayRotation,
		DefaultQualityFactor: n.DefaultQualityFactor,
		OnVideoFrameReceived: n.OnVideoFrameReceived,
		OnIndevEvent:         n.OnIndevEvent,
		OnRpcEvent:           n.OnRpcEvent,
		OnVideoStateChange:   n.OnVideoStateChange,
		OnNativeRestart:      n.OnNativeRestart,
		HandshakeMessage:     handshakeMessage,
		MaxRestartAttempts:   maxRestartAttempts,
	}
}

func (p *nativeProxyOptions) toNativeOptions() *NativeOptions {
	return &NativeOptions{
		SystemVersion:        p.SystemVersion,
		AppVersion:           p.AppVersion,
		DisplayRotation:      p.DisplayRotation,
		DefaultQualityFactor: p.DefaultQualityFactor,
	}
}

// cmdWrapper wraps exec.Cmd to implement processCmd interface
type cmdWrapper struct {
	*exec.Cmd
	stdoutHandler *nativeProxyStdoutHandler
}

func (c *cmdWrapper) GetProcess() interface {
	Kill() error
	Signal(sig interface{}) error
} {
	return &processWrapper{Process: c.Process}
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

	startMu sync.Mutex // mutex for the start process (context and isStopped)
	ctx     context.Context
	cancel  context.CancelFunc

	client   *GRPCClient
	clientMu sync.RWMutex // mutex for the client

	cmd   *cmdWrapper
	cmdMu sync.Mutex // mutex for the cmd

	logger   *zerolog.Logger
	options  *nativeProxyOptions
	restarts uint
	stopped  bool
}

// NewNativeProxy creates a new NativeProxy that spawns a separate process
func NewNativeProxy(opts NativeOptions) (*NativeProxy, error) {
	proxyOptions := opts.toProxyOptions()
	proxyOptions.VideoStreamUnixSocket = fmt.Sprintf("@jetkvm/native/video-stream/%s", randomId(4))

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
		options:               proxyOptions,
		restarts:              0,
	}

	return proxy, nil
}

func (p *NativeProxy) startVideoStreamListener() error {
	if p.videoStreamListener != nil {
		return nil
	}

	logger := p.logger.With().Str("socketPath", p.videoStreamUnixSocket).Logger()
	listener, err := net.Listen("unix", p.videoStreamUnixSocket)
	if err != nil {
		logger.Warn().Err(err).Msg("failed to start video stream listener")
		return fmt.Errorf("failed to start video stream listener: %w", err)
	}
	logger.Info().Msg("video stream listener started")
	p.videoStreamListener = listener

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				logger.Warn().Err(err).Msg("failed to accept socket")
				continue
			}

			logger.Info().Msg("video stream socket accepted")
			go p.handleVideoFrame(conn)
		}
	}()

	return nil
}

type nativeProxyStdoutHandler struct {
	mu               *sync.Mutex
	handshakeCh      chan bool
	handshakeMessage string
	handshakeDone    bool
}

func (w *nativeProxyStdoutHandler) Write(p []byte) (n int, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if !w.handshakeDone && strings.Contains(string(p), w.handshakeMessage) {
		w.handshakeDone = true
		w.handshakeCh <- true
		return len(p), nil
	}

	os.Stdout.Write(p)

	return len(p), nil
}

func (p *NativeProxy) toProcessCommand() (*cmdWrapper, error) {
	// generate a new random ID for the gRPC socket on each restart
	// sometimes the socket is not closed properly when the process exits
	// this is a workaround to avoid the issue
	p.nativeUnixSocket = fmt.Sprintf("jetkvm/native/grpc/%s", randomId(4))
	p.options.CtrlUnixSocket = p.nativeUnixSocket

	envArgs, err := utils.MarshalEnv(p.options)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal environment variables: %w", err)
	}

	cmd := &cmdWrapper{
		Cmd: exec.Command(
			p.binaryPath,
			"-subcomponent=native",
		),
		stdoutHandler: &nativeProxyStdoutHandler{
			mu:               &sync.Mutex{},
			handshakeCh:      make(chan bool),
			handshakeMessage: p.options.HandshakeMessage,
		},
	}
	cmd.Stdout = cmd.stdoutHandler
	cmd.Stderr = os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid:   true,
		Pdeathsig: syscall.SIGTERM,
	}
	// Set environment variable to indicate native process mode
	cmd.Env = append(
		os.Environ(),
		envArgs...,
	)

	return cmd, nil
}

func (p *NativeProxy) handleVideoFrame(conn net.Conn) {
	defer conn.Close()

	inboundPacket := make([]byte, maxFrameSize)
	var frameSizeBuffer [4]byte
	lastFrame := time.Now()

	for {
		// Read 4-byte frame length prefix
		_, err := io.ReadFull(conn, frameSizeBuffer[:])
		if err != nil {
			if err != io.EOF {
				p.logger.Warn().Err(err).Msg("failed to read frame size from socket")
			}
			break
		}

		frameSize := binary.LittleEndian.Uint32(frameSizeBuffer[:])
		if frameSize == 0 || frameSize > maxFrameSize {
			p.logger.Error().Uint32("frameSize", frameSize).Uint32("maxFrameSize", maxFrameSize).
				Msg("received invalid frame size")
			break
		}

		// Read the actual frame data
		_, err = io.ReadFull(conn, inboundPacket[:frameSize])
		if err != nil {
			p.logger.Warn().Err(err).Msg("failed to read video frame from socket")
			break
		}

		now := time.Now()
		sinceLastFrame := now.Sub(lastFrame)
		lastFrame = now
		p.options.OnVideoFrameReceived(inboundPacket[:frameSize], sinceLastFrame)
	}
}

// it should be only called by start() method, as it isn't thread-safe
func (p *NativeProxy) setUpGRPCClient() error {
	// wait until handshake completed
	select {
	case <-p.cmd.stdoutHandler.handshakeCh:
		p.logger.Info().Msg("handshake completed")
	case <-time.After(10 * time.Second):
		return fmt.Errorf("handshake not completed within 10 seconds")
	}

	logger := p.logger.With().Str("socketPath", "@"+p.nativeUnixSocket).Logger()
	client, err := NewGRPCClient(grpcClientOptions{
		SocketPath:         p.nativeUnixSocket,
		Logger:             &logger,
		OnIndevEvent:       p.options.OnIndevEvent,
		OnRpcEvent:         p.options.OnRpcEvent,
		OnVideoStateChange: p.options.OnVideoStateChange,
	})

	logger.Info().Msg("created gRPC client")
	if err != nil {
		return fmt.Errorf("failed to create gRPC client: %w", err)
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

	// Call on native restart callback if it exists and restarts are greater than 0
	if p.options.OnNativeRestart != nil && p.restarts > 0 {
		go p.options.OnNativeRestart()
	}

	return nil
}

func (p *NativeProxy) doStart() error {
	p.cmdMu.Lock()
	defer p.cmdMu.Unlock()

	// lock OS thread to prevent the process from being moved to a different thread
	// see also https://go.dev/issue/27505
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	cmd, err := p.toProcessCommand()
	if err != nil {
		return fmt.Errorf("failed to create process: %w", err)
	}

	p.cmd = cmd

	if err := p.cmd.Start(); err != nil {
		return fmt.Errorf("failed to start native process: %w", err)
	}

	// here we'll replace the logger with a new one that includes the process ID
	// there's no need to lock the mutex here as the side effect is acceptable
	newLogger := p.logger.With().Int("pid", p.cmd.Process.Pid).Logger()
	p.logger = &newLogger

	p.logger.Info().Msg("native process started")

	if err := p.setUpGRPCClient(); err != nil {
		return fmt.Errorf("failed to set up gRPC client: %w", err)
	}

	return nil
}

// Start starts the native process
func (p *NativeProxy) Start() error {
	p.startMu.Lock()
	defer p.startMu.Unlock()

	p.ctx, p.cancel = context.WithCancel(context.Background())

	if p.stopped {
		return fmt.Errorf("proxy is stopped")
	}

	if err := p.startVideoStreamListener(); err != nil {
		return fmt.Errorf("failed to start video stream listener: %w", err)
	}

	if err := p.doStart(); err != nil {
		return fmt.Errorf("failed to start native process: %w", err)
	}

	go p.monitorProcess()

	return nil
}

// monitorProcess monitors the native process and restarts it if it crashes
func (p *NativeProxy) monitorProcess() {
	for {
		if p.stopped {
			return
		}

		select {
		case <-p.ctx.Done():
			p.logger.Trace().Msg("context done, stopping monitor process [before wait]")
			return
		default:
		}

		p.cmdMu.Lock()
		err := fmt.Errorf("native process not started")
		if p.cmd != nil {
			err = p.cmd.Wait()
		}
		p.cmdMu.Unlock()

		if p.stopped {
			return
		}

		select {
		case <-p.ctx.Done():
			p.logger.Trace().Msg("context done, stopping monitor process [after wait]")
			return
		default:
		}

		p.logger.Warn().Err(err).Msg("native process exited, restarting ...")

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
	p.restarts++
	logger := p.logger.With().Uint("attempt", p.restarts).Uint("maxAttempts", p.options.MaxRestartAttempts).Logger()

	if p.restarts >= p.options.MaxRestartAttempts {
		logger.Fatal().Msgf("max restart attempts reached, exiting: %s", supervisor.FailsafeReasonVideoMaxRestartAttemptsReached)
		return fmt.Errorf("max restart attempts reached")
	}

	if p.stopped {
		return fmt.Errorf("proxy is stopped")
	}

	// Close old client
	p.clientMu.Lock()
	if p.client != nil {
		if err := p.client.Close(); err != nil {
			logger.Warn().Err(err).Msg("failed to close gRPC client")
		}
		p.client = nil // set to nil to avoid closing it again
	}
	p.clientMu.Unlock()
	logger.Info().Msg("gRPC client closed")

	logger.Info().Msg("attempting to restart native process")
	if err := p.doStart(); err != nil {
		logger.Error().Err(err).Msg("failed to start native process")
		return fmt.Errorf("failed to start native process: %w", err)
	}

	logger.Info().Msg("native process restarted successfully")
	return nil
}

// Stop stops the native process
func (p *NativeProxy) Stop() error {
	p.startMu.Lock()
	defer p.startMu.Unlock()

	p.stopped = true

	if p.cmd.Process != nil {
		if err := p.cmd.Process.Kill(); err != nil {
			return fmt.Errorf("failed to kill native process: %w", err)
		}
		_ = p.cmd.Wait()
	}

	return nil
}

func zeroValue[V string | bool | float64]() V {
	var v V
	return v
}

func nativeProxyClientExec[K comparable, V string | bool | float64](p *NativeProxy, fn func(*GRPCClient) (V, error)) (V, error) {
	p.clientMu.RLock()
	defer p.clientMu.RUnlock()

	if p.client == nil {
		return zeroValue[V](), fmt.Errorf("gRPC client not initialized")
	}

	return fn(p.client)
}

func nativeProxyClientExecWithoutArgument(p *NativeProxy, fn func(*GRPCClient) error) error {
	p.clientMu.RLock()
	defer p.clientMu.RUnlock()

	if p.client == nil {
		return fmt.Errorf("gRPC client not initialized")
	}

	return fn(p.client)
}

// Implement all Native methods by forwarding to gRPC client
func (p *NativeProxy) VideoSetSleepMode(enabled bool) error {
	return nativeProxyClientExecWithoutArgument(p, func(client *GRPCClient) error {
		return client.VideoSetSleepMode(enabled)
	})
}

func (p *NativeProxy) VideoGetSleepMode() (bool, error) {
	return nativeProxyClientExec[bool](p, func(client *GRPCClient) (bool, error) {
		return client.VideoGetSleepMode()
	})
}

func (p *NativeProxy) VideoSleepModeSupported() bool {
	result, _ := nativeProxyClientExec[bool](p, func(client *GRPCClient) (bool, error) {
		return client.VideoSleepModeSupported(), nil
	})
	return result
}

func (p *NativeProxy) VideoSetQualityFactor(factor float64) error {
	return nativeProxyClientExecWithoutArgument(p, func(client *GRPCClient) error {
		return client.VideoSetQualityFactor(factor)
	})
}

func (p *NativeProxy) VideoGetQualityFactor() (float64, error) {
	return nativeProxyClientExec[float64](p, func(client *GRPCClient) (float64, error) {
		return client.VideoGetQualityFactor()
	})
}

func (p *NativeProxy) VideoSetEDID(edid string) error {
	return nativeProxyClientExecWithoutArgument(p, func(client *GRPCClient) error {
		return client.VideoSetEDID(edid)
	})
}

func (p *NativeProxy) VideoGetEDID() (string, error) {
	return nativeProxyClientExec[string](p, func(client *GRPCClient) (string, error) {
		return client.VideoGetEDID()
	})
}

func (p *NativeProxy) VideoLogStatus() (string, error) {
	return nativeProxyClientExec[string](p, func(client *GRPCClient) (string, error) {
		return client.VideoLogStatus()
	})
}

func (p *NativeProxy) VideoStop() error {
	return nativeProxyClientExecWithoutArgument(p, func(client *GRPCClient) error {
		return client.VideoStop()
	})
}

func (p *NativeProxy) VideoStart() error {
	return nativeProxyClientExecWithoutArgument(p, func(client *GRPCClient) error {
		return client.VideoStart()
	})
}

func (p *NativeProxy) GetLVGLVersion() (string, error) {
	return nativeProxyClientExec[string](p, func(client *GRPCClient) (string, error) {
		return client.GetLVGLVersion()
	})
}

func (p *NativeProxy) UIObjHide(objName string) (bool, error) {
	result, err := nativeProxyClientExec[bool](p, func(client *GRPCClient) (bool, error) {
		return client.UIObjHide(objName)
	})
	return result, err
}

func (p *NativeProxy) UIObjShow(objName string) (bool, error) {
	result, err := nativeProxyClientExec[bool](p, func(client *GRPCClient) (bool, error) {
		return client.UIObjShow(objName)
	})
	return result, err
}

func (p *NativeProxy) UISetVar(name string, value string) {
	_ = nativeProxyClientExecWithoutArgument(p, func(client *GRPCClient) error {
		client.UISetVar(name, value)
		return nil
	})
}

func (p *NativeProxy) UIGetVar(name string) string {
	result, _ := nativeProxyClientExec[string](p, func(client *GRPCClient) (string, error) {
		return client.UIGetVar(name), nil
	})
	return result
}

func (p *NativeProxy) UIObjAddState(objName string, state string) (bool, error) {
	return nativeProxyClientExec[bool](p, func(client *GRPCClient) (bool, error) {
		return client.UIObjAddState(objName, state)
	})
}

func (p *NativeProxy) UIObjClearState(objName string, state string) (bool, error) {
	return nativeProxyClientExec[bool](p, func(client *GRPCClient) (bool, error) {
		return client.UIObjClearState(objName, state)
	})
}

func (p *NativeProxy) UIObjAddFlag(objName string, flag string) (bool, error) {
	return nativeProxyClientExec[bool](p, func(client *GRPCClient) (bool, error) {
		return client.UIObjAddFlag(objName, flag)
	})
}

func (p *NativeProxy) UIObjClearFlag(objName string, flag string) (bool, error) {
	return nativeProxyClientExec[bool](p, func(client *GRPCClient) (bool, error) {
		return client.UIObjClearFlag(objName, flag)
	})
}

func (p *NativeProxy) UIObjFadeIn(objName string, duration uint32) (bool, error) {
	return nativeProxyClientExec[bool](p, func(client *GRPCClient) (bool, error) {
		return client.UIObjFadeIn(objName, duration)
	})
}

func (p *NativeProxy) UIObjFadeOut(objName string, duration uint32) (bool, error) {
	return nativeProxyClientExec[bool](p, func(client *GRPCClient) (bool, error) {
		return client.UIObjFadeOut(objName, duration)
	})
}

func (p *NativeProxy) UIObjSetLabelText(objName string, text string) (bool, error) {
	return nativeProxyClientExec[bool](p, func(client *GRPCClient) (bool, error) {
		return client.UIObjSetLabelText(objName, text)
	})
}

func (p *NativeProxy) UIObjSetImageSrc(objName string, image string) (bool, error) {
	return nativeProxyClientExec[bool](p, func(client *GRPCClient) (bool, error) {
		return client.UIObjSetImageSrc(objName, image)
	})
}

func (p *NativeProxy) UIObjSetOpacity(objName string, opacity int) (bool, error) {
	return nativeProxyClientExec[bool](p, func(client *GRPCClient) (bool, error) {
		return client.UIObjSetOpacity(objName, opacity)
	})
}

func (p *NativeProxy) DisplaySetRotation(rotation uint16) (bool, error) {
	return nativeProxyClientExec[bool](p, func(client *GRPCClient) (bool, error) {
		return client.DisplaySetRotation(rotation)
	})
}

func (p *NativeProxy) UpdateLabelIfChanged(objName string, newText string) {
	_ = nativeProxyClientExecWithoutArgument(p, func(client *GRPCClient) error {
		client.UpdateLabelIfChanged(objName, newText)
		return nil
	})
}

func (p *NativeProxy) UpdateLabelAndChangeVisibility(objName string, newText string) {
	_ = nativeProxyClientExecWithoutArgument(p, func(client *GRPCClient) error {
		client.UpdateLabelAndChangeVisibility(objName, newText)
		return nil
	})
}

func (p *NativeProxy) SwitchToScreenIf(screenName string, shouldSwitch []string) {
	_ = nativeProxyClientExecWithoutArgument(p, func(client *GRPCClient) error {
		client.SwitchToScreenIf(screenName, shouldSwitch)
		return nil
	})
}

func (p *NativeProxy) SwitchToScreenIfDifferent(screenName string) {
	_ = nativeProxyClientExecWithoutArgument(p, func(client *GRPCClient) error {
		client.SwitchToScreenIfDifferent(screenName)
		return nil
	})
}

func (p *NativeProxy) DoNotUseThisIsForCrashTestingOnly() {
	_ = nativeProxyClientExecWithoutArgument(p, func(client *GRPCClient) error {
		client.DoNotUseThisIsForCrashTestingOnly()
		return nil
	})
}
