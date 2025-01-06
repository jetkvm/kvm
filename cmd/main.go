package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jetkvm/kvm/internal/config"
	"github.com/jetkvm/kvm/internal/hardware"
	"github.com/jetkvm/kvm/internal/logging"
	"github.com/jetkvm/kvm/internal/network"
	"github.com/jetkvm/kvm/internal/server"

	"github.com/gwatts/rootcerts"
)

var appCtx context.Context

func main() {
	var cancel context.CancelFunc
	appCtx, cancel = context.WithCancel(context.Background())
	defer cancel()

	logging.Logger.Info("Starting JetKvm")
	go hardware.RunWatchdog()
	go network.ConfirmCurrentSystem()

	http.DefaultClient.Timeout = 1 * time.Minute
	cfg := config.LoadConfig()
	logging.Logger.Debug("config loaded")

	err := rootcerts.UpdateDefaultTransport()
	if err != nil {
		logging.Logger.Errorf("failed to load CA certs: %v", err)
	}

	go network.TimeSyncLoop()

	hardware.StartNativeCtrlSocketServer()
	hardware.StartNativeVideoSocketServer()

	go func() {
		err = hardware.ExtractAndRunNativeBin()
		if err != nil {
			logging.Logger.Errorf("failed to extract and run native bin: %v", err)
			//TODO: prepare an error message screen buffer to show on kvm screen
		}
	}()

	go func() {
		time.Sleep(15 * time.Minute)
		for {
			logging.Logger.Debugf("UPDATING - Auto update enabled: %v", cfg.AutoUpdateEnabled)
			if cfg.AutoUpdateEnabled == false {
				return
			}
			if server.CurrentSession != nil {
				logging.Logger.Debugf("skipping update since a session is active")
				time.Sleep(1 * time.Minute)
				continue
			}
			includePreRelease := cfg.IncludePreRelease
			err = network.TryUpdate(context.Background(), hardware.GetDeviceID(), includePreRelease)
			if err != nil {
				logging.Logger.Errorf("failed to auto update: %v", err)
			}
			time.Sleep(1 * time.Hour)
		}
	}()
	//go RunFuseServer()
	go server.RunWebServer()
	go server.RunWebsocketClient()
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	<-sigs
	log.Println("JetKVM Shutting Down")
	//if fuseServer != nil {
	//	err := setMassStorageImage(" ")
	//	if err != nil {
	//		log.Printf("Failed to unmount mass storage image: %v", err)
	//	}
	//	err = fuseServer.Unmount()
	//	if err != nil {
	//		log.Printf("Failed to unmount fuse: %v", err)
	//	}

	// os.Exit(0)
}
