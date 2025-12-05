package native

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/erikdubbelboer/gspt"
	"github.com/jetkvm/kvm/internal/logging"
)

// Native Process
// stdout - exchange messages with the parent process
// stderr - logging and error messages

var (
	procPrefix    string = "jetkvm: [native]"
	lastProcTitle string
)

const (
	DebugModeFile = "/userdata/jetkvm/.native-debug-mode"
)

func setProcTitle(status string) {
	lastProcTitle = status
	if status != "" {
		status = " " + status
	}
	title := fmt.Sprintf("%s%s", procPrefix, status)
	gspt.SetProcTitle(title)
}

func getClientLogger() *logging.Context {
	return GetNativeLogger().With().Str("component", "grpc-client").Int("pid", pid)
}

func getServerLogger() *logging.Context {
	return GetNativeLogger().With().Str("component", "grpc-server").Int("pid", pid)
}

func monitorCrashSignal(ctx context.Context, nativeInstance NativeInterface) {
	getServerLogger().Info().Msg("DEBUG mode: will crash the process on SIGHUP signal")

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGHUP)

	for {
		select {
		case sig := <-sigChan:
			getServerLogger().Warn().Str("signal", sig.String()).Msg("received termination signal, crashing the process for testing purposes")
			nativeInstance.DoNotUseThisIsForCrashTestingOnly()
		case <-ctx.Done():
			getServerLogger().Info().Msg("context done, stopping monitor process")
			return
		}
	}
}

func updateProcessTitle(state *VideoState) {
	if state == nil {
		procPrefix = "jetkvm: [native]"
	} else {
		var status string
		if state.Streaming == VideoStreamingStatusInactive {
			status = "inactive"
		} else if !state.Ready {
			status = "not ready"
		} else if state.Error != "" {
			status = state.Error
		} else {
			status = fmt.Sprintf("%s,%dx%d,%.1ffps", state.Streaming.String(), state.Width, state.Height, state.FramePerSecond)
		}
		procPrefix = fmt.Sprintf("jetkvm: [native+video{%s}]", status)
	}
	setProcTitle(lastProcTitle)
}

// RunNativeProcess runs the native process mode
func RunNativeProcess(binaryName string) {
	appCtx, appCtxCancel := context.WithCancel(context.Background())
	defer appCtxCancel()

	setProcTitle("starting")
	logger := getServerLogger()

	// for defer clean-up scoping... this is NOT a goroutine
	func() {
		// Parse native options
		var proxyOptions nativeProxyOptions
		if err := env.Parse(&proxyOptions); err != nil {
			logger.Fatal().Err(err).Msg("failed to parse native proxy options")
			return
		}
		socketPath := fmt.Sprintf("@%v", proxyOptions.CtrlUnixSocket)
		logger = logger.With().Interface("proxyOptions", proxyOptions).Str("socketPath", socketPath)

		// Connect to video stream socket
		conn, err := net.Dial("unix", proxyOptions.VideoStreamUnixSocket)
		if err != nil {
			logger.Fatal().Err(err).Msg("failed to connect to video stream socket")
			return
		}
		defer conn.Close()
		logger = logger.With().Interface("local", conn.LocalAddr()).Interface("remote", conn.RemoteAddr())
		logger.Info().Msg("connected to video stream socket")

		nativeOptions := proxyOptions.toNativeOptions()
		nativeOptions.OnVideoFrameReceived = func(frame []byte, duration time.Duration) {
			// Write 4-byte frame length prefix, then frame data
			var frameSizeBuffer [4]byte
			binary.LittleEndian.PutUint32(frameSizeBuffer[:], uint32(len(frame)))

			if _, err := conn.Write(frameSizeBuffer[:]); err != nil {
				logger.Fatal().Err(err).Msg("failed to write frame size to video stream socket")
				return
			}
			if _, err := conn.Write(frame); err != nil {
				logger.Fatal().Err(err).Msg("failed to write frame to video stream socket")
				return
			}
		}
		nativeOptions.OnVideoStateChange = func(state VideoState) {
			updateProcessTitle(&state)
		}

		setProcTitle("initializing")
		logger.Info().Msg("starting native instance")

		// Create and start native instance
		nativeInstance := NewNative(*nativeOptions)
		if err := nativeInstance.Start(); err != nil {
			logger.Fatal().Err(err).Msg("failed to start native instance")
			return
		}

		setProcTitle("starting gRPC server")
		logger.Info().Msg("starting gRPC server")

		// Create and Start gRPC server
		grpcServer := NewGRPCServer(nativeInstance, socketPath)
		server, lis, err := grpcServer.Start()
		if err != nil {
			logger.Fatal().Err(err).Msg("failed to start gRPC server")
			return
		}

		defer lis.Close()   // close listener after
		defer server.Stop() // forceful server stop

		logger = logger.With().Interface("listener", lis)

		setProcTitle("ready")

		if _, err := os.Stat(DebugModeFile); err == nil {
			go monitorCrashSignal(appCtx, nativeInstance)
		}

		// Signal that we're ready by writing handshake message to stdout (for parent to read)
		// Stdout.Write is used to avoid buffering the message
		_, err = os.Stdout.Write([]byte(proxyOptions.HandshakeMessage + "\n"))
		if err != nil {
			logger.Fatal().Err(err).Msg("failed to write handshake message to stdout")
			return
		}
		logger.Debug().Msg("wrote handshake message for supervisor")

		// Set up signal handling
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)

		// Wait for signal
		sig := <-sigChan
		logger.Info().Str("signal", sig.String()).Msg("received termination signal")

		// Graceful shutdown might take a long time so use a 2 second timeout
		shutdownCtx, shutdownCancel := context.WithTimeout(appCtx, 2*time.Second)
		defer shutdownCancel()

		// Stop gRPC server
		setProcTitle("shutting down gRPC server")
		logger.Info().Msg("shutting down gRPC server")
		go func() {
			server.GracefulStop()
		}()

		// Wait for shutdown or timeout (and then the defer will force stop)
		<-shutdownCtx.Done()
	}()

	logger.Info().Msg("native process exiting")
}
