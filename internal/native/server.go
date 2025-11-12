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

// RunNativeProcess runs the native process mode
func RunNativeProcess(binaryName string) {
	logger := *nativeLogger
	// Initialize logger

	gspt.SetProcTitle(binaryName + " [native]")

	var proxyOptions nativeProxyOptions
	if err := env.Parse(&proxyOptions); err != nil {
		logger.Fatal().Err(err).Msg("failed to parse native options")
	}

	// connect to video stream socket
	conn, err := net.Dial("unixpacket", proxyOptions.VideoStreamUnixSocket)
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to connect to video stream socket")
	}
	logger.Info().Str("video_stream_socket_path", proxyOptions.VideoStreamUnixSocket).Msg("connected to video stream socket")

	nativeOptions := proxyOptions.toNativeOptions()
	nativeOptions.OnVideoFrameReceived = func(frame []byte, duration time.Duration) {
		_, err := conn.Write(frame)
		if err != nil {
			logger.Fatal().Err(err).Msg("failed to write frame to video stream socket")
		}
	}

	// Create native instance
	nativeInstance := NewNative(*nativeOptions)

	// Start native instance
	if err := nativeInstance.Start(); err != nil {
		logger.Fatal().Err(err).Msg("failed to start native instance")
	}

	// Create gRPC server
	grpcServer := NewGRPCServer(nativeInstance, &logger)

	logger.Info().Msg("starting gRPC server")
	// Start gRPC server
	server, lis, err := StartGRPCServer(grpcServer, fmt.Sprintf("@%v", proxyOptions.CtrlUnixSocket), &logger)
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to start gRPC server")
	}
	gspt.SetProcTitle(binaryName + " [native] ready")

	// Signal that we're ready by writing socket path to stdout (for parent to read)
	fmt.Fprintf(os.Stdout, "%s\n", proxyOptions.CtrlUnixSocket)
	defer os.Stdout.Close()

	// Set up signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)

	// Wait for signal
	<-sigChan
	logger.Info().Msg("received termination signal")

	// Graceful shutdown
	server.GracefulStop()
	lis.Close()

	logger.Info().Msg("native process exiting")
}
