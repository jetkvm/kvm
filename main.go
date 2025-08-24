package kvm

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gwatts/rootcerts"
	"github.com/jetkvm/kvm/internal/audio"
)

var (
	appCtx           context.Context
	isAudioServer    bool
	audioProcessDone chan struct{}
	audioSupervisor  *audio.AudioServerSupervisor
)

// runAudioServer is now handled by audio.RunAudioOutputServer
// This function is kept for backward compatibility but delegates to the audio package
func runAudioServer() {
	err := audio.RunAudioOutputServer()
	if err != nil {
		logger.Error().Err(err).Msg("audio output server failed")
		os.Exit(1)
	}
}

func startAudioSubprocess() error {
	// Start adaptive buffer management for optimal performance
	audio.StartAdaptiveBuffering()

	// Create audio server supervisor
	audioSupervisor = audio.NewAudioServerSupervisor()

	// Set the global supervisor for access from audio package
	audio.SetAudioOutputSupervisor(audioSupervisor)

	// Set up callbacks for process lifecycle events
	audioSupervisor.SetCallbacks(
		// onProcessStart
		func(pid int) {
			logger.Info().Int("pid", pid).Msg("audio server process started")

			// Start audio relay system for main process without a track initially
			// The track will be updated when a WebRTC session is created
			if err := audio.StartAudioRelay(nil); err != nil {
				logger.Error().Err(err).Msg("failed to start audio relay")
			}
		},
		// onProcessExit
		func(pid int, exitCode int, crashed bool) {
			if crashed {
				logger.Error().Int("pid", pid).Int("exit_code", exitCode).Msg("audio server process crashed")
			} else {
				logger.Info().Int("pid", pid).Msg("audio server process exited gracefully")
			}

			// Stop audio relay when process exits
			audio.StopAudioRelay()
			// Stop adaptive buffering
			audio.StopAdaptiveBuffering()
		},
		// onRestart
		func(attempt int, delay time.Duration) {
			logger.Warn().Int("attempt", attempt).Dur("delay", delay).Msg("restarting audio server process")
		},
	)

	// Start the supervisor
	if err := audioSupervisor.Start(); err != nil {
		return fmt.Errorf("failed to start audio supervisor: %w", err)
	}

	// Monitor supervisor and handle cleanup
	go func() {
		defer close(audioProcessDone)

		// Wait for supervisor to stop
		for audioSupervisor.IsRunning() {
			time.Sleep(100 * time.Millisecond)
		}

		logger.Info().Msg("audio supervisor stopped")
	}()

	return nil
}

func Main(audioServer bool, audioInputServer bool) {
	// Initialize channel and set audio server flag
	isAudioServer = audioServer
	audioProcessDone = make(chan struct{})

	// If running as audio server, only initialize audio processing
	if isAudioServer {
		runAudioServer()
		return
	}

	// If running as audio input server, only initialize audio input processing
	if audioInputServer {
		err := audio.RunAudioInputServer()
		if err != nil {
			logger.Error().Err(err).Msg("audio input server failed")
			os.Exit(1)
		}
		return
	}
	LoadConfig()

	var cancel context.CancelFunc
	appCtx, cancel = context.WithCancel(context.Background())
	defer cancel()

	systemVersionLocal, appVersionLocal, err := GetLocalVersion()
	if err != nil {
		logger.Warn().Err(err).Msg("failed to get local version")
	}

	logger.Info().
		Interface("system_version", systemVersionLocal).
		Interface("app_version", appVersionLocal).
		Msg("starting JetKVM")

	go runWatchdog()
	go confirmCurrentSystem()

	http.DefaultClient.Timeout = 1 * time.Minute

	err = rootcerts.UpdateDefaultTransport()
	if err != nil {
		logger.Warn().Err(err).Msg("failed to load Root CA certificates")
	}
	logger.Info().
		Int("ca_certs_loaded", len(rootcerts.Certs())).
		Msg("loaded Root CA certificates")

	// Initialize network
	if err := initNetwork(); err != nil {
		logger.Error().Err(err).Msg("failed to initialize network")
		os.Exit(1)
	}

	// Initialize time sync
	initTimeSync()
	timeSync.Start()

	// Initialize mDNS
	if err := initMdns(); err != nil {
		logger.Error().Err(err).Msg("failed to initialize mDNS")
		os.Exit(1)
	}

	// Initialize native ctrl socket server
	StartNativeCtrlSocketServer()

	// Initialize native video socket server
	StartNativeVideoSocketServer()

	initPrometheus()

	go func() {
		err = ExtractAndRunNativeBin()
		if err != nil {
			logger.Warn().Err(err).Msg("failed to extract and run native bin")
			// (future) prepare an error message screen buffer to show on kvm screen
		}
	}()

	// initialize usb gadget
	initUsbGadget()

	// Start audio subprocess
	err = startAudioSubprocess()
	if err != nil {
		logger.Warn().Err(err).Msg("failed to start audio subprocess")
	}

	// Initialize session provider for audio events
	initializeAudioSessionProvider()

	// Initialize audio event broadcaster for WebSocket-based real-time updates
	audio.InitializeAudioEventBroadcaster()
	logger.Info().Msg("audio event broadcaster initialized")

	if err := setInitialVirtualMediaState(); err != nil {
		logger.Warn().Err(err).Msg("failed to set initial virtual media state")
	}

	if err := initImagesFolder(); err != nil {
		logger.Warn().Err(err).Msg("failed to init images folder")
	}
	initJiggler()

	// initialize display
	initDisplay()

	go func() {
		time.Sleep(15 * time.Minute)
		for {
			logger.Debug().Bool("auto_update_enabled", config.AutoUpdateEnabled).Msg("UPDATING")
			if !config.AutoUpdateEnabled {
				return
			}
			if currentSession != nil {
				logger.Debug().Msg("skipping update since a session is active")
				time.Sleep(1 * time.Minute)
				continue
			}
			includePreRelease := config.IncludePreRelease
			err = TryUpdate(context.Background(), GetDeviceID(), includePreRelease)
			if err != nil {
				logger.Warn().Err(err).Msg("failed to auto update")
			}
			time.Sleep(1 * time.Hour)
		}
	}()
	//go RunFuseServer()
	go RunWebServer()

	go RunWebSecureServer()
	// Web secure server is started only if TLS mode is enabled
	if config.TLSMode != "" {
		startWebSecureServer()
	}

	// As websocket client already checks if the cloud token is set, we can start it here.
	go RunWebsocketClient()

	initSerialPort()
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	<-sigs
	logger.Info().Msg("JetKVM Shutting Down")

	// Stop audio subprocess and wait for cleanup
	if !isAudioServer {
		if audioSupervisor != nil {
			logger.Info().Msg("stopping audio supervisor")
			if err := audioSupervisor.Stop(); err != nil {
				logger.Error().Err(err).Msg("failed to stop audio supervisor")
			}
		}
		<-audioProcessDone
	} else {
		audio.StopNonBlockingAudioStreaming()
	}
	//if fuseServer != nil {
	//	err := setMassStorageImage(" ")
	//	if err != nil {
	//		logger.Infof("Failed to unmount mass storage image: %v", err)
	//	}
	//	err = fuseServer.Unmount()
	//	if err != nil {
	//		logger.Infof("Failed to unmount fuse: %v", err)
	//	}

	// os.Exit(0)
}
