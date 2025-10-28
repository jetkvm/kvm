package kvm

import (
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/jetkvm/kvm/internal/ota"
)

var builtAppVersion = "0.1.0+dev"

var otaState *ota.State

func initOta() {
	otaState = ota.NewState(ota.Options{
		Logger:             otaLogger,
		ReleaseAPIEndpoint: config.GetUpdateAPIURL(),
		GetHTTPClient: func() *http.Client {
			transport := http.DefaultTransport.(*http.Transport).Clone()
			transport.Proxy = config.NetworkConfig.GetTransportProxyFunc()

			client := &http.Client{
				Transport: transport,
			}
			return client
		},
		GetLocalVersion: GetLocalVersion,
		HwReboot:        hwReboot,
		OnStateUpdate: func(state *ota.RPCState) {
			triggerOTAStateUpdate(state)
		},
		OnProgressUpdate: func(progress float32) {
			writeJSONRPCEvent("otaProgress", progress, currentSession)
		},
	})
}

func triggerOTAStateUpdate(state *ota.RPCState) {
	go func() {
		if currentSession == nil || (otaState == nil && state == nil) {
			return
		}
		if state == nil {
			state = otaState.ToRPCState()
		}
		writeJSONRPCEvent("otaState", state, currentSession)
	}()
}

// GetBuiltAppVersion returns the built-in app version
func GetBuiltAppVersion() string {
	return builtAppVersion
}

// GetLocalVersion returns the local version of the system and app
func GetLocalVersion() (systemVersion *semver.Version, appVersion *semver.Version, err error) {
	appVersion, err = semver.NewVersion(builtAppVersion)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid built-in app version: %w", err)
	}

	systemVersionBytes, err := os.ReadFile("/version")
	if err != nil {
		return nil, appVersion, fmt.Errorf("error reading system version: %w", err)
	}

	systemVersion, err = semver.NewVersion(strings.TrimSpace(string(systemVersionBytes)))
	if err != nil {
		return nil, appVersion, fmt.Errorf("invalid system version: %w", err)
	}

	return systemVersion, appVersion, nil
}
