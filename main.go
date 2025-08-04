package kvm

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gwatts/rootcerts"
	"github.com/jetkvm/kvm/internal/audio"
	"github.com/pion/webrtc/v4/pkg/media"
)

var appCtx context.Context

func Main() {
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

	// Start non-blocking audio streaming and deliver Opus frames to WebRTC
	err = audio.StartNonBlockingAudioStreaming(func(frame []byte) {
		// Deliver Opus frame to WebRTC audio track if session is active
		if currentSession != nil {
			config := audio.GetAudioConfig()
			var sampleData []byte
			if audio.IsAudioMuted() {
				sampleData = make([]byte, len(frame)) // silence
			} else {
				sampleData = frame
			}
			if err := currentSession.AudioTrack.WriteSample(media.Sample{
				Data:     sampleData,
				Duration: config.FrameSize,
			}); err != nil {
				logger.Warn().Err(err).Msg("error writing audio sample")
				audio.RecordFrameDropped()
			}
		} else {
			audio.RecordFrameDropped()
		}
	})
	if err != nil {
		logger.Warn().Err(err).Msg("failed to start non-blocking audio streaming")
	}

	// Initialize audio event broadcaster for WebSocket-based real-time updates
	InitializeAudioEventBroadcaster()
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

	// Stop non-blocking audio manager
	audio.StopNonBlockingAudioStreaming()
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
