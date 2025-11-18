package native

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/erikdubbelboer/gspt"
	"github.com/rs/zerolog"
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

func monitorCrashSignal(ctx context.Context, logger *zerolog.Logger, nativeInstance NativeInterface) {
	logger.Info().Msg("DEBUG mode: will crash the process on SIGHUP signal")

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGHUP)

	for {
		select {
		case sig := <-sigChan:
			logger.Info().Str("signal", sig.String()).Msg("received termination signal")
			nativeInstance.DoNotUseThisIsForCrashTestingOnly()
		case <-ctx.Done():
			logger.Info().Msg("context done, stopping monitor process")
			return
		}
	}
}

// RunNativeProcess runs the native process mode
func RunNativeProcess(binaryName string) {
	appCtx, appCtxCancel := context.WithCancel(context.Background())
	defer appCtxCancel()

	logger := nativeLogger.With().Int("pid", os.Getpid()).Logger()
	setProcTitle("starting")

	// Parse native options
	var proxyOptions nativeProxyOptions
	if err := env.Parse(&proxyOptions); err != nil {
		logger.Fatal().Err(err).Msg("failed to parse native proxy options")
	}

	// Connect to video stream socket
	conn, err := net.Dial("unixpacket", proxyOptions.VideoStreamUnixSocket)
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to connect to video stream socket")
	}
	logger.Info().Str("videoStreamSocketPath", proxyOptions.VideoStreamUnixSocket).Msg("connected to video stream socket")

	nativeOptions := proxyOptions.toNativeOptions()
	nativeOptions.OnVideoFrameReceived = func(frame []byte, duration time.Duration) {
		_, err := conn.Write(frame)
		if err != nil {
			logger.Fatal().Err(err).Msg("failed to write frame to video stream socket")
		}
	}

	// Create native instance
	nativeInstance := NewNative(*nativeOptions)
	gspt.SetProcTitle("jetkvm: [native] initializing")

	// Start native instance
	if err := nativeInstance.Start(); err != nil {
		logger.Fatal().Err(err).Msg("failed to start native instance")
	}

	grpcLogger := logger.With().Str("socketPath", fmt.Sprintf("@%v", proxyOptions.CtrlUnixSocket)).Logger()
	setProcTitle("starting gRPC server")
	// Create gRPC server
	grpcServer := NewGRPCServer(nativeInstance, &grpcLogger)

	logger.Info().Msg("starting gRPC server")
	// Start gRPC server
	server, lis, err := StartGRPCServer(grpcServer, fmt.Sprintf("@%v", proxyOptions.CtrlUnixSocket), &logger)
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to start gRPC server")
	}
	setProcTitle("ready")

	if _, err := os.Stat(DebugModeFile); err == nil {
		logger.Info().Msg("DEBUG mode: enabled")
		go monitorCrashSignal(appCtx, &logger, nativeInstance)
	}

	// Signal that we're ready by writing handshake message to stdout (for parent to read)
	// Stdout.Write is used to avoid buffering the message
	_, err = os.Stdout.Write([]byte(proxyOptions.HandshakeMessage + "\n"))
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to write handshake message to stdout")
	}

	// Set up signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)

	// Wait for signal
	sig := <-sigChan
	logger.Info().
		Str("signal", sig.String()).
		Msg("received termination signal")

	// Graceful shutdown might stuck forever,
	// we will use Stop() instead to force quit the gRPC server,
	// we can implement a graceful shutdown with a timeout in the future if needed
	server.Stop()
	lis.Close()

	logger.Info().Msg("native process exiting")
}
