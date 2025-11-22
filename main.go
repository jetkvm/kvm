package kvm

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/erikdubbelboer/gspt"
	"github.com/gwatts/rootcerts"
	"github.com/jetkvm/kvm/internal/logging"
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
	setProcTitle("starting")
	mainLogger := logging.GetSubsystemLogger("jetkvm-main")
	mainLogger.Log().Msg("JetKVM Starting Up")

	checkFailsafeReason()
	if failsafeModeActive {
		procPrefix = "jetkvm: [app+failsafe]"
		logging.GetSubsystemLogger("failsafe").Warn().Str("reason", failsafeModeReason).Msg("failsafe mode activated")
	}

	LoadConfig()

	var cancel context.CancelFunc
	appCtx, cancel = context.WithCancel(context.Background())
	defer cancel()

	systemVersionLocal, appVersionLocal, err := GetLocalVersion()
	if err != nil {
		mainLogger.Warn().Err(err).Msg("failed to get local version")
	}

	mainLogger.Info().
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
		mainLogger.Warn().Err(err).Msg("failed to load Root CA certificates")
	}
	mainLogger.Info().
		Int("ca_certs_loaded", len(rootcerts.Certs())).
		Msg("loaded Root CA certificates")

	initOta()

	http.DefaultClient.Timeout = 1 * time.Minute

	// Initialize network
	setProcTitle("initNetwork")
	if err := initNetwork(); err != nil {
		mainLogger.Error().Err(err).Msg("failed to initialize network")
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
		mainLogger.Error().Err(err).Msg("failed to initialize mDNS")
	}

	setProcTitle("initPrometheus")
	initPrometheus()
	if err := setInitialVirtualMediaState(); err != nil {
		mainLogger.Warn().Err(err).Msg("failed to set initial virtual media state")
	}

	if err := initImagesFolder(); err != nil {
		mainLogger.Warn().Err(err).Msg("failed to init images folder")
	}

	initJiggler()

	// start video sleep mode timer
	startVideoSleepModeTicker()

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
	mainLogger.Log().Msg("JetKVM ready")

	go RunAutoUpdateCheck()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	<-sigs

	if err := rpcUnmountImage(); err != nil {
		mainLogger.Warn().Err(err).Msg("failed to eject virtual media on shutdown")
	}

	gadget.Close()

	mainLogger.Log().Msg("JetKVM Shutting Down")
	// os.Exit(0)
}
