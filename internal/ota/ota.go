package ota

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/Masterminds/semver/v3"
)

// UpdateReleaseAPIEndpoint updates the release API endpoint
func (s *State) UpdateReleaseAPIEndpoint(endpoint string) {
	s.releaseAPIEndpoint = endpoint
}

// GetReleaseAPIEndpoint returns the release API endpoint
func (s *State) GetReleaseAPIEndpoint() string {
	return s.releaseAPIEndpoint
}

// getUpdateURL returns the update URL for the given parameters
func (s *State) getUpdateURL(params UpdateParams) (string, error) {
	updateURL, err := url.Parse(s.releaseAPIEndpoint)
	if err != nil {
		return "", fmt.Errorf("error parsing update metadata URL: %w", err)
	}

	appTargetVersion := s.GetTargetVersion("app")
	if appTargetVersion != "" && params.AppTargetVersion == "" {
		params.AppTargetVersion = appTargetVersion
	}
	systemTargetVersion := s.GetTargetVersion("system")
	if systemTargetVersion != "" && params.SystemTargetVersion == "" {
		params.SystemTargetVersion = systemTargetVersion
	}

	query := updateURL.Query()
	query.Set("deviceId", params.DeviceID)
	query.Set("prerelease", fmt.Sprintf("%v", params.IncludePreRelease))
	if params.AppTargetVersion != "" {
		query.Set("appVersion", params.AppTargetVersion)
	}
	if params.SystemTargetVersion != "" {
		query.Set("systemVersion", params.SystemTargetVersion)
	}
	updateURL.RawQuery = query.Encode()

	return updateURL.String(), nil
}

func (s *State) fetchUpdateMetadata(ctx context.Context, params UpdateParams) (*UpdateMetadata, error) {
	metadata := &UpdateMetadata{}

	url, err := s.getUpdateURL(params)
	if err != nil {
		return nil, fmt.Errorf("error getting update URL: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("error creating request: %w", err)
	}

	client := s.client()

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error sending request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	err = json.NewDecoder(resp.Body).Decode(metadata)
	if err != nil {
		return nil, fmt.Errorf("error decoding response: %w", err)
	}

	return metadata, nil
}

func (s *State) TryUpdate(ctx context.Context, params UpdateParams) error {
	return s.doUpdate(ctx, params)
}

func (s *State) triggerStateUpdate() {
	s.onStateUpdate(s.ToRPCState())
}

func (s *State) doUpdate(ctx context.Context, params UpdateParams) error {
	scopedLogger := s.l.With().
		Interface("params", params).
		Logger()

	scopedLogger.Info().Msg("checking for updates")
	if s.updating {
		return fmt.Errorf("update already in progress")
	}

	s.updating = true
	s.triggerStateUpdate()

	defer func() {
		s.updating = false
		s.triggerStateUpdate()
	}()

	appUpdate, systemUpdate, err := s.getUpdateStatus(ctx, params)
	if err != nil {
		return s.componentUpdateError("Error checking for updates", err, &scopedLogger)
	}

	if params.CheckOnly {
		return nil
	}

	s.metadataFetchedAt = time.Now()
	s.triggerStateUpdate()

	if appUpdate.available || appUpdate.downgradeAvailable {
		appUpdate.pending = true
	}

	if systemUpdate.available || systemUpdate.downgradeAvailable {
		systemUpdate.pending = true
	}

	if appUpdate.pending {
		scopedLogger.Info().
			Str("url", appUpdate.url).
			Str("hash", appUpdate.hash).
			Msg("App update available")

		if err := s.updateApp(ctx, appUpdate); err != nil {
			return s.componentUpdateError("Error updating app", err, &scopedLogger)
		}
	} else {
		scopedLogger.Info().Msg("App is up to date")
	}

	if systemUpdate.pending {
		if err := s.updateSystem(ctx, systemUpdate); err != nil {
			return s.componentUpdateError("Error updating system", err, &scopedLogger)
		}
	} else {
		scopedLogger.Info().Msg("System is up to date")
	}

	if s.rebootNeeded {
		scopedLogger.Info().Msg("System Rebooting due to OTA update")

		postRebootAction := &PostRebootAction{
			HealthCheck: "/device/status",
			RedirectUrl: fmt.Sprintf("/settings/general/update?version=%s", systemUpdate.version),
		}

		if err := s.reboot(true, postRebootAction, 10*time.Second); err != nil {
			return s.componentUpdateError("Error requesting reboot", err, &scopedLogger)
		}
	}

	return nil
}

// UpdateParams represents the parameters for the update
type UpdateParams struct {
	DeviceID            string `json:"deviceID"`
	AppTargetVersion    string `json:"appTargetVersion"`
	SystemTargetVersion string `json:"systemTargetVersion"`
	IncludePreRelease   bool   `json:"includePreRelease"`
	CheckOnly           bool   `json:"checkOnly"`
}

func (s *State) getUpdateStatus(
	ctx context.Context,
	params UpdateParams,
) (
	appUpdate *componentUpdateStatus,
	systemUpdate *componentUpdateStatus,
	err error,
) {
	appUpdate = &componentUpdateStatus{}
	systemUpdate = &componentUpdateStatus{}

	if currentAppUpdate, ok := s.componentUpdateStatuses["app"]; ok {
		appUpdate = &currentAppUpdate
	}

	if currentSystemUpdate, ok := s.componentUpdateStatuses["system"]; ok {
		systemUpdate = &currentSystemUpdate
	}

	// Get local versions
	systemVersionLocal, appVersionLocal, err := s.getLocalVersion()
	if err != nil {
		return nil, nil, fmt.Errorf("error getting local version: %w", err)
	}
	appUpdate.localVersion = appVersionLocal.String()
	systemUpdate.localVersion = systemVersionLocal.String()

	// Get remote metadata
	remoteMetadata, err := s.fetchUpdateMetadata(ctx, params)
	if err != nil {
		err = fmt.Errorf("error checking for updates: %w", err)
		return
	}
	appUpdate.url = remoteMetadata.AppURL
	appUpdate.hash = remoteMetadata.AppHash
	appUpdate.version = remoteMetadata.AppVersion

	systemUpdate.url = remoteMetadata.SystemURL
	systemUpdate.hash = remoteMetadata.SystemHash
	systemUpdate.version = remoteMetadata.SystemVersion

	// Get remote versions
	systemVersionRemote, err := semver.NewVersion(remoteMetadata.SystemVersion)
	if err != nil {
		err = fmt.Errorf("error parsing remote system version: %w", err)
		return
	}
	systemUpdate.available = systemVersionRemote.GreaterThan(systemVersionLocal)
	systemUpdate.downgradeAvailable = systemVersionRemote.LessThan(systemVersionLocal)

	appVersionRemote, err := semver.NewVersion(remoteMetadata.AppVersion)
	if err != nil {
		err = fmt.Errorf("error parsing remote app version: %w, %s", err, remoteMetadata.AppVersion)
		return
	}
	appUpdate.available = appVersionRemote.GreaterThan(appVersionLocal)
	appUpdate.downgradeAvailable = appVersionRemote.LessThan(appVersionLocal)

	// Handle pre-release updates
	isRemoteSystemPreRelease := systemVersionRemote.Prerelease() != ""
	isRemoteAppPreRelease := appVersionRemote.Prerelease() != ""

	if isRemoteSystemPreRelease && !params.IncludePreRelease {
		systemUpdate.available = false
	}
	if isRemoteAppPreRelease && !params.IncludePreRelease {
		appUpdate.available = false
	}

	s.componentUpdateStatuses["app"] = *appUpdate
	s.componentUpdateStatuses["system"] = *systemUpdate

	return
}

// GetUpdateStatus returns the current update status (for backwards compatibility)
func (s *State) GetUpdateStatus(ctx context.Context, params UpdateParams) (*UpdateStatus, error) {
	_, _, err := s.getUpdateStatus(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("error getting update status: %w", err)
	}

	return s.ToUpdateStatus(), nil
}
