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
	"github.com/jetkvm/kvm/internal/kvm"
	"github.com/jetkvm/kvm/internal/logging"

	"github.com/gwatts/rootcerts"
)

var ctx context.Context

func main() {
	var cancel context.CancelFunc
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()

	logging.Logger.Info("Starting JetKvm")
	go kvm.RunWatchdog(ctx)
	go kvm.ConfirmCurrentSystem()

	http.DefaultClient.Timeout = 1 * time.Minute
	cfg := config.LoadConfig()
	logging.Logger.Debug("config loaded")

	err := rootcerts.UpdateDefaultTransport()
	if err != nil {
		logging.Logger.Errorf("failed to load CA certs: %v", err)
	}

	go kvm.TimeSyncLoop()

	kvm.StartNativeCtrlSocketServer()
	kvm.StartNativeVideoSocketServer()

	go func() {
		err = kvm.ExtractAndRunNativeBin(ctx)
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
			if kvm.CurrentSession != nil {
				logging.Logger.Debugf("skipping update since a session is active")
				time.Sleep(1 * time.Minute)
				continue
			}
			includePreRelease := cfg.IncludePreRelease
			err = kvm.TryUpdate(context.Background(), kvm.GetDeviceID(), includePreRelease)
			if err != nil {
				logging.Logger.Errorf("failed to auto update: %v", err)
			}
			time.Sleep(1 * time.Hour)
		}
	}()
	//go RunFuseServer()
	go kvm.RunWebServer()
	go kvm.RunWebsocketClient()
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
