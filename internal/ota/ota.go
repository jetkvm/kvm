package ota

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

// newHTTPRequestWithTrace creates a new HTTP request with a trace logger
// TODO: use OTEL instead of doing this manually
func (s *State) newHTTPRequestWithTrace(ctx context.Context, method, url string, body io.Reader, logger func() *zerolog.Event) (*http.Request, error) {
	localCtx := ctx
	if s.l.GetLevel() <= zerolog.TraceLevel {
		if logger == nil {
			logger = func() *zerolog.Event { return s.l.Trace() }
		}

		l := func() *zerolog.Event { return logger().Str("url", url).Str("method", method) }
		localCtx = httptrace.WithClientTrace(localCtx, &httptrace.ClientTrace{
			GetConn:              func(hostPort string) { l().Str("hostPort", hostPort).Msg("[conn] starting to create conn") },
			GotConn:              func(info httptrace.GotConnInfo) { l().Interface("info", info).Msg("[conn] connection established") },
			PutIdleConn:          func(err error) { l().Err(err).Msg("[conn] connection returned to idle pool") },
			GotFirstResponseByte: func() { l().Msg("[resp] first response byte received") },
			Got100Continue:       func() { l().Msg("[resp] 100 continue received") },
			DNSStart:             func(info httptrace.DNSStartInfo) { l().Interface("info", info).Msg("[dns] starting to look up dns") },
			DNSDone:              func(info httptrace.DNSDoneInfo) { l().Interface("info", info).Msg("[dns] done looking up dns") },
			ConnectStart: func(network, addr string) {
				l().Str("network", network).Str("addr", addr).Msg("[tcp] starting tcp connection")
			},
			ConnectDone: func(network, addr string, err error) {
				l().Str("network", network).Str("addr", addr).Err(err).Msg("[tcp] tcp connection created")
			},
			TLSHandshakeStart: func() { l().Msg("[tls] handshake started") },
			TLSHandshakeDone: func(state tls.ConnectionState, err error) {
				l().
					Str("tlsVersion", tls.VersionName(state.Version)).
					Str("cipherSuite", tls.CipherSuiteName(state.CipherSuite)).
					Str("negotiatedProtocol", state.NegotiatedProtocol).
					Str("serverName", state.ServerName).
					Err(err).Msg("[tls] handshake done")
			},
		})
	}

	return http.NewRequestWithContext(localCtx, method, url, body)
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

	req, err := s.newHTTPRequestWithTrace(ctx, "GET", url, nil, traceLogger)
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

	s.updating = true
	s.triggerStateUpdate()

	if len(params.Components) == 0 {
		params.Components = defaultComponents
	}

	_, shouldUpdateApp := params.Components["app"]
	_, shouldUpdateSystem := params.Components["system"]

	if !shouldUpdateApp && !shouldUpdateSystem {
		return s.componentUpdateError(
			"Update aborted: no components were specified to update. Requested components: ",
			fmt.Errorf("%v", params.Components),
			&scopedLogger,
		)
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

	if !appUpdate.pending && !systemUpdate.pending {
		scopedLogger.Info().Msg("No updates available")
		s.updating = false
		s.triggerStateUpdate()
		return nil
	}

	scopedLogger.Trace().Bool("pending", appUpdate.pending).Msg("Checking for app update")

	if appUpdate.pending {
		scopedLogger.Info().
			Str("url", appUpdate.url).
			Str("hash", appUpdate.hash).
			Msg("App update available")

		bypassSignatureCheck := shouldBypassSignatureCheck(appUpdate.version, appUpdate.customVersionUpdate)
		if err := s.updateApp(ctx, appUpdate, bypassSignatureCheck); err != nil {
			return s.componentUpdateError("Error updating app", err, &scopedLogger)
		}
	} else {
		scopedLogger.Info().Msg("App is up to date")
	}

	scopedLogger.Trace().Bool("pending", systemUpdate.pending).Msg("Checking for system update")

	if systemUpdate.pending {
		bypassSignatureCheck := shouldBypassSignatureCheck(systemUpdate.version, systemUpdate.customVersionUpdate)
		if err := s.updateSystem(ctx, systemUpdate, bypassSignatureCheck); err != nil {
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
		}

		scopedLogger.Info().Msg("System Rebooting due to OTA update")

		redirectUrl := "/settings/general/update"

		if params.ResetConfig {
			scopedLogger.Info().Msg("Resetting config")
			if err := s.resetConfig(); err != nil {
				return s.componentUpdateError("Error resetting config", err, &scopedLogger)
			}
			redirectUrl = "/welcome"
		}

		postRebootAction := &PostRebootAction{
			HealthCheck: "/device/status",
			RedirectTo:  redirectUrl,
		}

		// REBOOT_REDIRECT_DELAY_MS is 7 seconds in the UI,
		// it means that healthCheckUrl will be called after 7 seconds that we send willReboot JSONRPC event
		// so we need to reboot it within 7 seconds to avoid it being called before the device is rebooted
		if err := s.reboot(true, postRebootAction, 5*time.Second); err != nil {
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
