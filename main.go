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
	var cancel context.CancelFunc
	appCtx, cancel = context.WithCancel(context.Background())
	defer cancel()
	logger.Info("Starting JetKvm")
	go runWatchdog()
	go confirmCurrentSystem()

	http.DefaultClient.Timeout = 1 * time.Minute
	LoadConfig()
	logger.Debug("config loaded")

	err := rootcerts.UpdateDefaultTransport()
	if err != nil {
		logger.Errorf("failed to load CA certs: %v", err)
	}

	go TimeSyncLoop()

	StartNativeCtrlSocketServer()
	StartNativeVideoSocketServer()

	initPrometheus()

	go func() {
		err = ExtractAndRunNativeBin()
		if err != nil {
			logger.Errorf("failed to extract and run native bin: %v", err)
			//TODO: prepare an error message screen buffer to show on kvm screen
		}
	}()

	initUsbGadget()

	go func() {
		time.Sleep(15 * time.Minute)
		for {
			logger.Debugf("UPDATING - Auto update enabled: %v", config.AutoUpdateEnabled)
			if !config.AutoUpdateEnabled {
				return
			}
			if currentSession != nil {
				logger.Debugf("skipping update since a session is active")
				time.Sleep(1 * time.Minute)
				continue
			}
			includePreRelease := config.IncludePreRelease
			err = TryUpdate(context.Background(), GetDeviceID(), includePreRelease)
			if err != nil {
				logger.Errorf("failed to auto update: %v", err)
			}
			time.Sleep(1 * time.Hour)
		}
	}()
	//go RunFuseServer()
	go RunWebServer()
	if config.TLSMode != "" {
		go RunWebSecureServer()
	}
	// If the cloud token isn't set, the client won't be started by default.
	// However, if the user adopts the device via the web interface, handleCloudRegister will start the client.
	if config.CloudToken != "" {
		go RunWebsocketClient()
	}
	initSerialPort()
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	<-sigs
	logger.Info("JetKVM Shutting Down")
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
