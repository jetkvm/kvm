package native

import (
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/erikdubbelboer/gspt"
)

// Native Process
// stdout - exchange messages with the parent process
// stderr - logging and error messages

var (
	procPrefix    string = "jetkvm: [native]"
	lastProcTitle string
)

func setProcTitle(status string) {
	lastProcTitle = status
	if status != "" {
		status = " " + status
	}
	title := fmt.Sprintf("%s%s", procPrefix, status)
	gspt.SetProcTitle(title)
}

// RunNativeProcess runs the native process mode
func RunNativeProcess(binaryName string) {
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

	// Signal that we're ready by writing handshake message to stdout (for parent to read)
	// Stdout.Write is used to avoid buffering the message
	_, err = os.Stdout.Write([]byte(proxyOptions.HandshakeMessage + "\n"))
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to write handshake message to stdout")
	}

	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGHUP)

		// non-blocking receive
		select {
		case <-sigChan:
			logger.Info().Msg("received SIGHUP signal, emulating crash")
			nativeInstance.DoNotUseThisIsForCrashTestingOnly()
		default:
		}
	}()

	// Set up signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)

	// Wait for signal
	sig := <-sigChan
	logger.Info().
		Str("signal", sig.String()).
		Msg("received termination signal")

	// Graceful shutdown
	server.GracefulStop()
	lis.Close()

	logger.Info().Msg("native process exiting")
}
