package kvm

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/erikdubbelboer/gspt"
	"github.com/gwatts/rootcerts"
	cryptotls "github.com/jetkvm/kvm/internal/crypto/tls"
	"github.com/jetkvm/kvm/internal/ota"
)

var appCtx context.Context
var procPrefix string = "jetkvm: [app]"

func setProcTitle(status string) {
	if status != "" {
		status = " " + status
	}
	title := fmt.Sprintf("%s%s", procPrefix, status)
	gspt.SetProcTitle(title)
}

func Main() {
	// Set GOMAXPROCS higher than CPU count to improve concurrency when goroutines
	// are blocked in I/O (TLS, sockets, CGO). This helps RDP/VNC/Web run in parallel
	// on the single-core RV1106 by allowing more OS threads for blocking operations.
	runtime.GOMAXPROCS(4)

	// Enable contention profiling for mutex and blocking events.
	// MutexProfileFraction(5) samples 1 in 5 mutex contentions.
	// BlockProfileRate(1_000_000) reports blocking events >= 1ms.
	runtime.SetMutexProfileFraction(5)
	runtime.SetBlockProfileRate(1_000_000)

	setProcTitle("starting")

	logger.Log().Msg("JetKVM Starting Up")

	checkFailsafeReason()
	if failsafeModeActive {
		procPrefix = "jetkvm: [app+failsafe]"
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

	// initialize usb gadget
	setProcTitle("initUsbGadget")
	initUsbGadget()

	setProcTitle("initNative")
	initNative(systemVersionLocal, appVersionLocal)
	initDisplay()

	initAudio()
	defer stopAudio()

	http.DefaultClient.Timeout = 1 * time.Minute

	err = rootcerts.UpdateDefaultTransport()
	if err != nil {
		logger.Warn().Err(err).Msg("failed to load Root CA certificates")
	}
	logger.Info().
		Int("ca_certs_loaded", len(rootcerts.Certs())).
		Msg("loaded Root CA certificates")

	initOta()

	http.DefaultClient.Timeout = 1 * time.Minute

	// Initialize network
	setProcTitle("initNetwork")
	if err := initNetwork(); err != nil {
		logger.Error().Err(err).Msg("failed to initialize network")
		// TODO: reset config to default
		os.Exit(1)
	}

	// Initialize time sync
	setProcTitle("initTimeSync")
	initTimeSync()
	timeSync.Start()

	// Initialize mesh VPN
	setProcTitle("initMeshVPN")
	initMeshVPN()

	// Initialize mDNS
	setProcTitle("initMdns")
	if err := initMdns(); err != nil {
		logger.Error().Err(err).Msg("failed to initialize mDNS")
	}

	setProcTitle("initPrometheus")
	initPrometheus()
	if err := setInitialVirtualMediaState(); err != nil {
		logger.Warn().Err(err).Msg("failed to set initial virtual media state")
	}

	if err := initImagesFolder(); err != nil {
		logger.Warn().Err(err).Msg("failed to init images folder")
	}

	initJiggler()

	// Initialize TLS subsystem early to check hardware crypto availability.
	// This ensures OpenSSL is fully initialized before any TLS connections
	// (HTTPS web server, VNC, RDP) which prevents first-connection delays.
	setProcTitle("initTLS")
	cryptotls.Init()
	logger.Info().
		Bool("hardwareAccelerated", cryptotls.IsHardwareAvailable()).
		Str("engine", cryptotls.HardwareEngine()).
		Msg("TLS subsystem initialized")

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

			if currentSession.Load() != nil {
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
			err = otaState.TryUpdate(context.Background(), ota.UpdateParams{
				DeviceID:          GetDeviceID(),
				IncludePreRelease: includePreRelease,
			})
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

	// Initialize VNC server if enabled
	if config.VNCEnabled {
		if err := initVNCServer(); err != nil {
			logger.Error().Err(err).Msg("failed to initialize VNC server")
		}
	}

	// Initialize RDP server if enabled
	if config.RDPEnabled {
		if err := initRDPServer(); err != nil {
			logger.Error().Err(err).Msg("failed to initialize RDP server")
		}
	}

	setProcTitle("ready")

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
