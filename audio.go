package kvm

import (
	"fmt"
	"net"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

func startFFmpeg() (cmd *exec.Cmd, err error) {
	binaryPath := "/userdata/jetkvm/bin/ffmpeg"
	// Run the binary in the background
	cmd = exec.Command(binaryPath,
		"-f", "alsa",
		"-channels", "2",
		"-sample_rate", "48000",
		"-i", "hw:1,0",
		"-c:a", "libopus",
		"-b:a", "64k", // ought to be enough for anybody
		"-vbr", "off",
		"-frame_duration", "20",
		"-compression_level", "2",
		"-f", "rtp",
		"rtp://127.0.0.1:3333")

	nativeOutputLock := sync.Mutex{}
	nativeStdout := &nativeOutput{
		mu:     &nativeOutputLock,
		logger: nativeLogger.Info().Str("pipe", "stdout"),
	}
	nativeStderr := &nativeOutput{
		mu:     &nativeOutputLock,
		logger: nativeLogger.Info().Str("pipe", "stderr"),
	}

	// Redirect stdout and stderr to the current process
	cmd.Stdout = nativeStdout
	cmd.Stderr = nativeStderr

	// Set the process group ID so we can kill the process and its children when this process exits
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid:   true,
		Pdeathsig: syscall.SIGKILL,
	}

	// Start the command
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start binary: %w", err)
	}

	return
}

func StartRtpAudioServer(handleClient func(net.Conn)) {
	scopedLogger := nativeLogger.With().
		Logger()

	listener, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 3333})
	if err != nil {
		scopedLogger.Warn().Err(err).Msg("failed to start server")
		return
	}

	scopedLogger.Info().Msg("server listening")

	go func() {
		for {
			cmd, err := startFFmpeg()
			if err != nil {
				scopedLogger.Error().Err(err).Msg("failed to start ffmpeg")
			}
			err = cmd.Wait()
			scopedLogger.Error().Err(err).Msg("ffmpeg exited, restarting")
			time.Sleep(2 * time.Second)
		}
	}()

	go handleClient(listener)
}
