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

func (s *State) fetchUpdateMetadata(ctx context.Context, deviceID string, includePreRelease bool) (*UpdateMetadata, error) {
	metadata := &UpdateMetadata{}

	updateURL, err := url.Parse(s.releaseAPIEndpoint)
	if err != nil {
		return nil, fmt.Errorf("error parsing update metadata URL: %w", err)
	}

	query := updateURL.Query()
	query.Set("deviceId", deviceID)
	query.Set("prerelease", fmt.Sprintf("%v", includePreRelease))
	updateURL.RawQuery = query.Encode()

	logger.Info().Str("url", updateURL.String()).Msg("Checking for updates")

	req, err := http.NewRequestWithContext(ctx, "GET", updateURL.String(), nil)
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

func (s *State) TryUpdate(ctx context.Context, deviceID string, includePreRelease bool) error {
	scopedLogger := s.l.With().
		Str("deviceID", deviceID).
		Str("includePreRelease", fmt.Sprintf("%v", includePreRelease)).
		Logger()

	scopedLogger.Info().Msg("Trying to update...")
	if s.updating {
		return fmt.Errorf("update already in progress")
	}

	s.updating = true
	s.onProgressUpdate()

	defer func() {
		s.updating = false
		s.onProgressUpdate()
	}()

	appUpdate, systemUpdate, err := s.getUpdateStatus(ctx, deviceID, includePreRelease)
	if err != nil {
		return s.componentUpdateError("Error checking for updates", err, &scopedLogger)
	}

	s.metadataFetchedAt = time.Now()
	s.onProgressUpdate()

	if appUpdate.available {
		appUpdate.pending = true
	}

	if systemUpdate.available {
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

func (s *State) getUpdateStatus(
	ctx context.Context,
	deviceID string,
	includePreRelease bool,
) (
	appUpdate *componentUpdateStatus,
	systemUpdate *componentUpdateStatus,
	err error,
) {
	appUpdate = &componentUpdateStatus{}
	systemUpdate = &componentUpdateStatus{}
	err = nil

	// Get local versions
	systemVersionLocal, appVersionLocal, err := s.getLocalVersion()
	if err != nil {
		return nil, nil, fmt.Errorf("error getting local version: %w", err)
	}
	appUpdate.localVersion = appVersionLocal.String()
	systemUpdate.localVersion = systemVersionLocal.String()

	// Get remote metadata
	remoteMetadata, err := s.fetchUpdateMetadata(ctx, deviceID, includePreRelease)
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

	appVersionRemote, err := semver.NewVersion(remoteMetadata.AppVersion)
	if err != nil {
		err = fmt.Errorf("error parsing remote app version: %w, %s", err, remoteMetadata.AppVersion)
		return
	}
	appUpdate.available = appVersionRemote.GreaterThan(appVersionLocal)

	// Handle pre-release updates
	isRemoteSystemPreRelease := systemVersionRemote.Prerelease() != ""
	isRemoteAppPreRelease := appVersionRemote.Prerelease() != ""

	if isRemoteSystemPreRelease && !includePreRelease {
		systemUpdate.available = false
	}
	if isRemoteAppPreRelease && !includePreRelease {
		appUpdate.available = false
	}

	s.componentUpdateStatuses["app"] = *appUpdate
	s.componentUpdateStatuses["system"] = *systemUpdate

	return
}

// GetUpdateStatus returns the current update status (for backwards compatibility)
func (s *State) GetUpdateStatus(ctx context.Context, deviceID string, includePreRelease bool) (*UpdateStatus, error) {
	_, _, err := s.getUpdateStatus(ctx, deviceID, includePreRelease)
	if err != nil {
		return nil, fmt.Errorf("error getting update status: %w", err)
	}

	return s.ToUpdateStatus(), nil
}
