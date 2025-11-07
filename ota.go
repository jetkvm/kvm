package kvm

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

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
		ResetConfig:     rpcResetConfig,
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
	})
	// to ensure backwards compatibility,
	// if there's an error, we won't return an error, but we will set the error field
	if err != nil {
		if updateStatus == nil {
			return nil, fmt.Errorf("error checking for updates: %w", err)
		}
		updateStatus.Error = err.Error()
	}

	logger.Info().Interface("updateStatus", updateStatus).Msg("Update status")

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

// ComponentName represents the name of a component
type tryUpdateComponents struct {
	AppTargetVersion    string `json:"app"`
	SystemTargetVersion string `json:"system"`
	Components          string `json:"components,omitempty"` // components is a comma-separated list of components to update
}

func rpcTryUpdate() error {
	return rpcTryUpdateComponents(tryUpdateComponents{
		AppTargetVersion:    "",
		SystemTargetVersion: "",
	}, config.IncludePreRelease, false, false)
}

func rpcTryUpdateComponents(components tryUpdateComponents, includePreRelease bool, checkOnly bool, resetConfig bool) error {
	updateParams := ota.UpdateParams{
		DeviceID:          GetDeviceID(),
		IncludePreRelease: includePreRelease,
		CheckOnly:         checkOnly,
		ResetConfig:       resetConfig,
	}

	logger.Info().Interface("components", components).Msg("components")

	currentAppTargetVersion := otaState.GetTargetVersion("app")
	appTargetVersionChanged := currentAppTargetVersion != components.AppTargetVersion
	updateParams.AppTargetVersion = components.AppTargetVersion
	if err := otaState.SetTargetVersion("app", components.AppTargetVersion); err != nil {
		return fmt.Errorf("failed to set app target version: %w", err)
	}

	currentSystemTargetVersion := otaState.GetTargetVersion("system")
	systemTargetVersionChanged := currentSystemTargetVersion != components.SystemTargetVersion
	updateParams.SystemTargetVersion = components.SystemTargetVersion
	if err := otaState.SetTargetVersion("system", components.SystemTargetVersion); err != nil {
		return fmt.Errorf("failed to set system target version: %w", err)
	}

	if components.Components != "" {
		updateParams.Components = strings.Split(components.Components, ",")
	}

	// if it's a check only update, we don't need to try to update, we just need to check if the version is available
	// and return the error immediately then revert the previous target versions
	if checkOnly {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		_, err := otaState.GetUpdateStatus(ctx, updateParams)
		if err == nil {
			return nil
		}

		// revert the previous target versions
		if appTargetVersionChanged {
			if err := otaState.SetTargetVersion("app", currentAppTargetVersion); err != nil {
				return fmt.Errorf("failed to revert app target version: %w", err)
			}
		}
		if systemTargetVersionChanged {
			if err := otaState.SetTargetVersion("system", currentSystemTargetVersion); err != nil {
				return fmt.Errorf("failed to revert system target version: %w", err)
			}
		}

		return err
	}

	go func() {
		err := otaState.TryUpdate(context.Background(), updateParams)
		if err != nil {
			logger.Warn().Err(err).Msg("failed to try update")
		}
	}()
	return nil
}

func rpcCancelDowngrade() error {
	if err := otaState.SetTargetVersion("app", ""); err != nil {
		return fmt.Errorf("failed to set app target version: %w", err)
	}
	if err := otaState.SetTargetVersion("system", ""); err != nil {
		return fmt.Errorf("failed to set system target version: %w", err)
	}
	return nil
}
