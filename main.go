package kvm

import (
	"context"
	"encoding/pem"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jetkvm/kvm/internal/ota"

	"github.com/erikdubbelboer/gspt"
	"github.com/gwatts/rootcerts"
	"github.com/rs/zerolog"
)

// caCertBundlePath is the path where the embedded CA certificate bundle is
// written at startup so that child processes (e.g. tailscale) can validate TLS
// certificates even though the device rootfs ships no system CA store.
const caCertBundlePath = "/tmp/jetkvm-cacerts.pem"

var appCtx context.Context
var procPrefix string = "jetkvm: [app]"

// writeCABundleFile converts the embedded rootcerts DER certificates to PEM
// and writes them to caCertBundlePath. This allows child processes to use the
// bundle via the SSL_CERT_FILE environment variable.
func writeCABundleFile() error {
	var bundle []byte
	for _, c := range rootcerts.CertsByTrust(rootcerts.ServerTrustedDelegator) {
		block := &pem.Block{
			Type:  "CERTIFICATE",
			Bytes: c.DER,
		}
		bundle = append(bundle, pem.EncodeToMemory(block)...)
	}
	return os.WriteFile(caCertBundlePath, bundle, 0644) //nolint:gosec
}

func setProcTitle(status string) {
	if status != "" {
		status = " " + status
	}
	title := fmt.Sprintf("%s%s", procPrefix, status)
	gspt.SetProcTitle(title)
}

func Main() {
	setProcTitle("starting")

	logger.Log().Msg("JetKVM Starting Up")

	defer func() {
		if r := recover(); r != nil {
			logger.WithLevel(zerolog.PanicLevel).Interface("error", r).Msg("Received panic")
			panic(r) // Re-panic to crash as usual
		}
	}()

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

	http.DefaultClient.Timeout = 1 * time.Minute

	err = rootcerts.UpdateDefaultTransport()
	if err != nil {
		logger.Warn().Err(err).Msg("failed to load Root CA certificates")
	}
	logger.Info().
		Int("ca_certs_loaded", len(rootcerts.Certs())).
		Msg("loaded Root CA certificates")

	// Write the embedded CA bundle to disk so child processes (tailscale, etc.)
	// can validate TLS certificates via SSL_CERT_FILE.
	if werr := writeCABundleFile(); werr != nil {
		logger.Warn().Err(werr).Msg("failed to write CA certificate bundle to disk")
	}

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
