package kvm

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/google/uuid"
	"github.com/jetkvm/kvm/internal/ota"
)

var builtAppVersion = "0.1.0+dev"

var otaState *ota.State

func initOta() {
	otaState = ota.NewState(ota.Options{
		Logger:             otaLogger,
		ReleaseAPIEndpoint: config.GetUpdateAPIURL(),
		GetHTTPClient: func() ota.HttpClient {
			transport := http.DefaultTransport.(*http.Transport).Clone()
			transport.Proxy = config.NetworkConfig.GetTransportProxyFunc()

			client := &http.Client{
				Transport: transport,
			}
			return client
		},
		GetLocalVersion: GetLocalVersion,
		HwReboot:        hwReboot,
		ResetConfig:     rpcResetConfig,
		SetAutoUpdate:   rpcSetAutoUpdateState,
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

func getUpdateStatus(includePreRelease bool) (*ota.UpdateStatus, error) {
	updateStatus, err := otaState.GetUpdateStatus(context.Background(), ota.UpdateParams{
		DeviceID:          GetDeviceID(),
		IncludePreRelease: includePreRelease,
		RequestID:         uuid.New().String(),
	})

	// to ensure backwards compatibility,
	// if there's an error, we won't return an error, but we will set the error field
	if err != nil {
		if updateStatus == nil {
			return nil, fmt.Errorf("error checking for updates: %w", err)
		}
		updateStatus.Error = err.Error()
	}

	// otaState doesn't have the current auto-update state, so we need to get it from the config
	if updateStatus.WillDisableAutoUpdate {
		updateStatus.WillDisableAutoUpdate = config.AutoUpdateEnabled
	}

	otaLogger.Info().Interface("updateStatus", updateStatus).Msg("Update status")

	return updateStatus, nil
}

func rpcGetDevChannelState() (bool, error) {
	return config.IncludePreRelease, nil
}

func rpcSetDevChannelState(enabled bool) error {
	config.IncludePreRelease = enabled
	if err := SaveConfig(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}
	return nil
}

func rpcGetUpdateStatus() (*ota.UpdateStatus, error) {
	return getUpdateStatus(config.IncludePreRelease)
}

func rpcGetUpdateStatusChannel(channel string) (*ota.UpdateStatus, error) {
	switch channel {
	case "stable":
		return getUpdateStatus(false)
	case "dev":
		return getUpdateStatus(true)
	default:
		return nil, fmt.Errorf("invalid channel: %s", channel)
	}
}

func rpcGetLocalVersion() (*ota.LocalMetadata, error) {
	systemVersion, appVersion, err := GetLocalVersion()
	if err != nil {
		return nil, fmt.Errorf("error getting local version: %w", err)
	}
	return &ota.LocalMetadata{
		AppVersion:    appVersion.String(),
		SystemVersion: systemVersion.String(),
	}, nil
}

type updateParams struct {
	Components map[string]string `json:"components,omitempty"`
}

func rpcTryUpdate() error {
	return rpcTryUpdateComponents(updateParams{
		Components: make(map[string]string),
	}, config.IncludePreRelease, false)
}

// rpcCheckUpdateComponents checks the update status for the given components
func rpcCheckUpdateComponents(params updateParams, includePreRelease bool) (*ota.UpdateStatus, error) {
	updateParams := ota.UpdateParams{
		DeviceID:          GetDeviceID(),
		IncludePreRelease: includePreRelease,
		Components:        params.Components,
	}
	info, err := otaState.GetUpdateStatus(context.Background(), updateParams)
	if err != nil {
		return nil, fmt.Errorf("failed to check update: %w", err)
	}
	return info, nil
}

func rpcTryUpdateComponents(params updateParams, includePreRelease bool, resetConfig bool) error {
	updateParams := ota.UpdateParams{
		DeviceID:          GetDeviceID(),
		IncludePreRelease: includePreRelease,
		ResetConfig:       resetConfig,
		Components:        params.Components,
	}

	go func() {
		err := otaState.TryUpdate(context.Background(), updateParams)
		if err != nil {
			otaLogger.Warn().Err(err).Msg("failed to try update")
		}
	}()
	return nil
}
