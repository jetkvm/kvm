package kvm

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gwatts/rootcerts"
)

var appCtx context.Context

func Main() {
	logger.Log().Msg("JetKVM Starting Up")

	checkFailsafeReason()
	if failsafeModeActive {
		logger.Warn().Str("reason", failsafeModeReason).Msg("failsafe mode activated")
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

	initDisplay()
	initNative(systemVersionLocal, appVersionLocal)

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
		// TODO: reset config to default
		os.Exit(1)
	}

	// Initialize time sync
	initTimeSync()
	timeSync.Start()

	// Initialize mDNS
	if err := initMdns(); err != nil {
		logger.Error().Err(err).Msg("failed to initialize mDNS")
	}

	initPrometheus()

	// initialize usb gadget
	initUsbGadget()
	if err := setInitialVirtualMediaState(); err != nil {
		logger.Warn().Err(err).Msg("failed to set initial virtual media state")
	}

	if err := initImagesFolder(); err != nil {
		logger.Warn().Err(err).Msg("failed to init images folder")
	}
	initJiggler()

	// start video sleep mode timer
	startVideoSleepModeTicker()

	go func() {
		// wait for 15 minutes before starting auto-update checks
		// this is to avoid interfering with initial setup processes
		// and to ensure the system is stable before checking for updates
		time.Sleep(15 * time.Minute)

		for {
			logger.Info().Bool("auto_update_enabled", config.AutoUpdateEnabled).Msg("auto-update check")
			if !config.AutoUpdateEnabled {
				logger.Debug().Msg("auto-update disabled")
				time.Sleep(5 * time.Minute) // we'll check if auto-updates are enabled in five minutes
				continue
			}

			if currentSession != nil {
				logger.Debug().Msg("skipping update since a session is active")
				time.Sleep(1 * time.Minute)
				continue
			}

			if isTimeSyncNeeded() || !timeSync.IsSyncSuccess() {
				logger.Debug().Msg("system time is not synced, will retry in 30 seconds")
				time.Sleep(30 * time.Second)
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
	initPublicIPState()

	initSerialPort()
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	<-sigs

	logger.Log().Msg("JetKVM Shutting Down")
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
