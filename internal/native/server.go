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
	"github.com/rs/zerolog"
)

// Native Process
// stdout - exchange messages with the parent process
// stderr - logging and error messages

// RunNativeProcess runs the native process mode
func RunNativeProcess(binaryName string) {
	// Initialize logger
	logger := zerolog.New(os.Stderr).With().Timestamp().Logger()

	gspt.SetProcTitle(binaryName + " [native]")

	// Determine socket path
	socketPath := os.Getenv("JETKVM_NATIVE_SOCKET")
	videoStreamSocketPath := os.Getenv("JETKVM_VIDEO_STREAM_SOCKET")

	if socketPath == "" || videoStreamSocketPath == "" {
		logger.Fatal().Str("socket_path", socketPath).Str("video_stream_socket_path", videoStreamSocketPath).Msg("socket path or video stream socket path is not set")
	}

	// connect to video stream socket
	conn, err := net.Dial("unixpacket", videoStreamSocketPath)
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to connect to video stream socket")
	}
	logger.Info().Str("video_stream_socket_path", videoStreamSocketPath).Msg("connected to video stream socket")

	var nativeOptions NativeOptions
	if err := env.Parse(&nativeOptions); err != nil {
		logger.Fatal().Err(err).Msg("failed to parse native options")
	}

	nativeOptions.OnVideoFrameReceived = func(frame []byte, duration time.Duration) {
		_, err := conn.Write(frame)
		if err != nil {
			logger.Fatal().Err(err).Msg("failed to write frame to video stream socket")
		}
	}

	// Create native instance
	nativeInstance := NewNative(nativeOptions)

	// Start native instance
	if err := nativeInstance.Start(); err != nil {
		logger.Fatal().Err(err).Msg("failed to start native instance")
	}

	// Create gRPC server
	grpcServer := NewGRPCServer(nativeInstance, &logger)

	logger.Info().Msg("starting gRPC server")
	// Start gRPC server
	server, lis, err := StartGRPCServer(grpcServer, fmt.Sprintf("@%v", socketPath), &logger)
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to start gRPC server")
	}
	gspt.SetProcTitle(binaryName + " [native] ready")

	// Signal that we're ready by writing socket path to stdout (for parent to read)
	fmt.Fprintf(os.Stdout, "%s\n", socketPath)
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
