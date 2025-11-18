package ota

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"time"

	"github.com/rs/zerolog"
)

// HttpClient is the interface for the HTTP client
type HttpClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// UpdateReleaseAPIEndpoint updates the release API endpoint
func (s *State) UpdateReleaseAPIEndpoint(endpoint string) {
	s.releaseAPIEndpoint = endpoint
}

// GetReleaseAPIEndpoint returns the release API endpoint
func (s *State) GetReleaseAPIEndpoint() string {
	return s.releaseAPIEndpoint
}

// getUpdateURL returns the update URL for the given parameters
func (s *State) getUpdateURL(params UpdateParams) (string, error, bool) {
	updateURL, err := url.Parse(s.releaseAPIEndpoint)
	if err != nil {
		return "", fmt.Errorf("error parsing update metadata URL: %w", err), false
	}

	isCustomVersion := false

	query := updateURL.Query()
	query.Set("deviceId", params.DeviceID)
	query.Set("prerelease", fmt.Sprintf("%v", params.IncludePreRelease))

	// set the custom versions if they are specified
	for component, constraint := range params.Components {
		if constraint == "" {
			continue
		}

		query.Set(component+"Version", constraint)
		isCustomVersion = true
	}

	updateURL.RawQuery = query.Encode()

	return updateURL.String(), nil, isCustomVersion
}

func (s *State) fetchUpdateMetadata(ctx context.Context, params UpdateParams) (*UpdateMetadata, error) {
	metadata := &UpdateMetadata{}

	logger := s.l.With().Logger()
	if params.RequestID != "" {
		logger = logger.With().Str("requestID", params.RequestID).Logger()
	}
	t := time.Now()
	traceLogger := func() *zerolog.Event {
		return logger.Trace().Dur("duration", time.Since(t))
	}

	url, err, isCustomVersion := s.getUpdateURL(params)
	traceLogger().Err(err).
		Msg("fetchUpdateMetadata: getUpdateURL")
	if err != nil {
		return nil, fmt.Errorf("error getting update URL: %w", err)
	}

	traceLogger().
		Str("url", url).
		Msg("fetching update metadata")

	localCtx := ctx
	if s.l.GetLevel() <= zerolog.TraceLevel {
		clientTrace := &httptrace.ClientTrace{
			GetConn: func(hostPort string) { traceLogger().Str("hostPort", hostPort).Msg("starting to create conn") },
			DNSStart: func(info httptrace.DNSStartInfo) {
				traceLogger().Interface("info", info).Msg("starting to look up dns")
			},
			DNSDone: func(info httptrace.DNSDoneInfo) { traceLogger().Interface("info", info).Msg("done looking up dns") },
			ConnectStart: func(network, addr string) {
				traceLogger().Str("network", network).Str("addr", addr).Msg("starting tcp connection")
			},
			ConnectDone: func(network, addr string, err error) {
				traceLogger().Str("network", network).Str("addr", addr).Err(err).Msg("tcp connection created")
			},
			GotConn: func(info httptrace.GotConnInfo) { traceLogger().Interface("info", info).Msg("connection established") },
		}
		localCtx = httptrace.WithClientTrace(localCtx, clientTrace)
	}

	req, err := http.NewRequestWithContext(localCtx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("error creating request: %w", err)
	}

	client := s.client()

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error sending request: %w", err)
	}
	defer resp.Body.Close()

	traceLogger().
		Int("status", resp.StatusCode).
		Msg("fetchUpdateMetadata: response")

	if isCustomVersion && resp.StatusCode == http.StatusNotFound {
		return nil, ErrVersionNotFound
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	err = json.NewDecoder(resp.Body).Decode(metadata)
	if err != nil {
		return nil, fmt.Errorf("error decoding response: %w", err)
	}

	traceLogger().
		Msg("fetchUpdateMetadata: completed")

	return metadata, nil
}

func (s *State) triggerStateUpdate() {
	s.onStateUpdate(s.ToRPCState())
}

func (s *State) triggerComponentUpdateState(component string, update *componentUpdateStatus) {
	s.componentUpdateStatuses[component] = *update
	s.triggerStateUpdate()
}

// TryUpdate tries to update the given components
// if the update is already in progress, it returns an error
func (s *State) TryUpdate(ctx context.Context, params UpdateParams) error {
	locked := s.mu.TryLock()
	if !locked {
		return fmt.Errorf("update already in progress")
	}

	return s.doUpdate(ctx, params)
}

// before calling doUpdate, the caller must have locked the mutex
// otherwise a runtime error will occur
func (s *State) doUpdate(ctx context.Context, params UpdateParams) error {
	defer s.mu.Unlock()

	scopedLogger := s.l.With().
		Interface("params", params).
		Logger()

	scopedLogger.Info().Msg("checking for updates")
	if s.updating {
		return fmt.Errorf("update already in progress")
	}

	if len(params.Components) == 0 {
		params.Components = defaultComponents
	}

	_, shouldUpdateApp := params.Components["app"]
	_, shouldUpdateSystem := params.Components["system"]

	if !shouldUpdateApp && !shouldUpdateSystem {
		return fmt.Errorf("no components to update")
	}

	appUpdate, systemUpdate, err := s.getUpdateStatus(ctx, params)
	if err != nil {
		return s.componentUpdateError("Error checking for updates", err, &scopedLogger)
	}

	s.metadataFetchedAt = time.Now()
	s.triggerStateUpdate()

	if shouldUpdateApp && appUpdate.available {
		appUpdate.pending = true
		s.updating = true
		s.triggerComponentUpdateState("app", appUpdate)
	}

	if shouldUpdateSystem && systemUpdate.available {
		systemUpdate.pending = true
		s.updating = true
		s.triggerComponentUpdateState("system", systemUpdate)
	}

	scopedLogger.Trace().Bool("pending", appUpdate.pending).Msg("Checking for app update")

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

	scopedLogger.Trace().Bool("pending", systemUpdate.pending).Msg("Checking for system update")

	if systemUpdate.pending {
		if err := s.updateSystem(ctx, systemUpdate); err != nil {
			return s.componentUpdateError("Error updating system", err, &scopedLogger)
		}
	} else {
		scopedLogger.Info().Msg("System is up to date")
	}

	if s.rebootNeeded {
		if appUpdate.customVersionUpdate || systemUpdate.customVersionUpdate {
			scopedLogger.Info().Msg("disabling auto-update due to custom version update")
			// If they are explicitly updating a custom version, we assume they want to disable auto-update
			if _, err := s.setAutoUpdate(false); err != nil {
				scopedLogger.Warn().Err(err).Msg("Failed to disable auto-update")
			}
			return nil
		}

		scopedLogger.Info().Msg("System Rebooting due to OTA update")

		redirectUrl := fmt.Sprintf("/settings/general/update?version=%s", systemUpdate.version)

		if params.ResetConfig {
			scopedLogger.Info().Msg("Resetting config")
			if err := s.resetConfig(); err != nil {
				return s.componentUpdateError("Error resetting config", err, &scopedLogger)
			}
			redirectUrl = "/device/setup"
		}

		postRebootAction := &PostRebootAction{
			HealthCheck: "/device/status",
			RedirectTo:  redirectUrl,
		}

		if err := s.reboot(true, postRebootAction, 10*time.Second); err != nil {
			return s.componentUpdateError("Error requesting reboot", err, &scopedLogger)
		}
	}

	// We don't need set the updating flag to false here. Either it will;
	// - set to false by the componentUpdateError function
	// - device will reboot
	return nil
}

// UpdateParams represents the parameters for the update
type UpdateParams struct {
	DeviceID          string            `json:"deviceID"`
	Components        map[string]string `json:"components"`
	IncludePreRelease bool              `json:"includePreRelease"`
	ResetConfig       bool              `json:"resetConfig"`
	// RequestID is a unique identifier for the update request
	// When it's set, detailed trace logs will be enabled (if the log level is Trace)
	RequestID string
}

// getUpdateStatus gets the update status for the given components
// and updates the componentUpdateStatuses map
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

	err = s.checkUpdateStatus(ctx, params, appUpdate, systemUpdate)
	if err != nil {
		return nil, nil, err
	}

	s.componentUpdateStatuses["app"] = *appUpdate
	s.componentUpdateStatuses["system"] = *systemUpdate

	return appUpdate, systemUpdate, nil
}

// checkUpdateStatus checks the update status for the given components
func (s *State) checkUpdateStatus(
	ctx context.Context,
	params UpdateParams,
	appUpdateStatus *componentUpdateStatus,
	systemUpdateStatus *componentUpdateStatus,
) error {
	// get the local versions
	systemVersionLocal, appVersionLocal, err := s.getLocalVersion()
	if err != nil {
		return fmt.Errorf("error getting local version: %w", err)
	}
	appUpdateStatus.localVersion = appVersionLocal.String()
	systemUpdateStatus.localVersion = systemVersionLocal.String()

	logger := s.l.With().Logger()
	if params.RequestID != "" {
		logger = logger.With().Str("requestID", params.RequestID).Logger()
	}
	t := time.Now()

	logger.Trace().
		Str("appVersionLocal", appVersionLocal.String()).
		Str("systemVersionLocal", systemVersionLocal.String()).
		Dur("duration", time.Since(t)).
		Msg("checkUpdateStatus: getLocalVersion")

	// fetch the remote metadata
	remoteMetadata, err := s.fetchUpdateMetadata(ctx, params)
	if err != nil {
		if err == ErrVersionNotFound || errors.Unwrap(err) == ErrVersionNotFound {
			err = ErrVersionNotFound
		} else {
			err = fmt.Errorf("error checking for updates: %w", err)
		}
		return err
	}

	logger.Trace().
		Interface("remoteMetadata", remoteMetadata).
		Dur("duration", time.Since(t)).
		Msg("checkUpdateStatus: fetchUpdateMetadata")

	// parse the remote metadata to the componentUpdateStatuses
	if err := remoteMetadataToComponentStatus(
		remoteMetadata,
		"app",
		appUpdateStatus,
		params,
	); err != nil {
		return fmt.Errorf("error parsing remote app version: %w", err)
	}

	if err := remoteMetadataToComponentStatus(
		remoteMetadata,
		"system",
		systemUpdateStatus,
		params,
	); err != nil {
		return fmt.Errorf("error parsing remote system version: %w", err)
	}

	if s.l.GetLevel() <= zerolog.TraceLevel {
		appUpdateStatus.getZerologLogger(&logger).Trace().Msg("checkUpdateStatus: remoteMetadataToComponentStatus [app]")
		systemUpdateStatus.getZerologLogger(&logger).Trace().Msg("checkUpdateStatus: remoteMetadataToComponentStatus [system]")
	}

	logger.Trace().
		Dur("duration", time.Since(t)).
		Msg("checkUpdateStatus: completed")

	return nil
}

// GetUpdateStatus returns the current update status (for backwards compatibility)
func (s *State) GetUpdateStatus(ctx context.Context, params UpdateParams) (*UpdateStatus, error) {
	// if no components are specified, use the default components
	// we should remove this once app router feature is released
	if len(params.Components) == 0 {
		params.Components = defaultComponents
	}

	appUpdateStatus := componentUpdateStatus{}
	systemUpdateStatus := componentUpdateStatus{}
	err := s.checkUpdateStatus(ctx, params, &appUpdateStatus, &systemUpdateStatus)
	if err != nil {
		return nil, fmt.Errorf("error getting update status: %w", err)
	}

	return toUpdateStatus(&appUpdateStatus, &systemUpdateStatus, ""), nil
}
